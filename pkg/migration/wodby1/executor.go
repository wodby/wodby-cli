package wodby1

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

const (
	defaultMigrationPollInterval = 2 * time.Second
	defaultMigrationTimeout      = 30 * time.Minute
	defaultMigrationBackupAge    = time.Hour
)

// MigrationExecutorOptions controls resumable target mutations.
// RetryAmbiguousOperation is empty by default: a request whose result could
// not be observed is recovered by exact read-back, never blindly repeated.
// When set, it acknowledges exactly one resource-scoped operation ID.
type MigrationExecutorOptions struct {
	StatePath               string
	PollInterval            time.Duration
	OperationTimeout        time.Duration
	MaxBackupAge            time.Duration
	RetryAmbiguousOperation string
	AllowLiveSource         bool
	Progress                func(string)
	Now                     func() time.Time
	LookupHost              func(context.Context, string) ([]string, error)
	RefreshSource           func(context.Context) (Export, error)
}

// MigrationPhaseResult is safe to print or encode: MigrationState contains
// target IDs and operation status only, never source secrets or backup URLs.
type MigrationPhaseResult struct {
	Phase MigrationPhase  `json:"phase"`
	State *MigrationState `json:"state"`
}

// MigrationExecutor executes an approved migration plan with durable internal
// checkpoints. The CLI exposes these checkpoints as one resumable apply.
type MigrationExecutor struct {
	target                  *TargetClient
	statePath               string
	pollInterval            time.Duration
	operationTimeout        time.Duration
	maxBackupAge            time.Duration
	retryAmbiguousOperation string
	allowLiveSource         bool
	progress                func(string)
	now                     func() time.Time
	lookupHost              func(context.Context, string) ([]string, error)
	refreshSource           func(context.Context) (Export, error)
}

func NewMigrationExecutor(client *TargetClient, opts MigrationExecutorOptions) (*MigrationExecutor, error) {
	if client == nil {
		return nil, errors.New("target Wodby 2 client is required")
	}
	if strings.TrimSpace(opts.StatePath) == "" {
		return nil, errors.New("migration state path is required")
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = defaultMigrationPollInterval
	}
	if opts.OperationTimeout == 0 {
		opts.OperationTimeout = defaultMigrationTimeout
	}
	if opts.MaxBackupAge == 0 {
		opts.MaxBackupAge = defaultMigrationBackupAge
	}
	if opts.PollInterval < 0 || opts.OperationTimeout <= 0 || opts.MaxBackupAge <= 0 {
		return nil, errors.New("migration polling interval, timeout, and maximum backup age must be positive")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.LookupHost == nil {
		opts.LookupHost = net.DefaultResolver.LookupHost
	}
	return &MigrationExecutor{
		target:                  client,
		statePath:               opts.StatePath,
		pollInterval:            opts.PollInterval,
		operationTimeout:        opts.OperationTimeout,
		maxBackupAge:            opts.MaxBackupAge,
		retryAmbiguousOperation: opts.RetryAmbiguousOperation,
		allowLiveSource:         opts.AllowLiveSource,
		progress:                opts.Progress,
		now:                     opts.Now,
		lookupHost:              opts.LookupHost,
		refreshSource:           opts.RefreshSource,
	}, nil
}

func (e *MigrationExecutor) reportProgress(format string, args ...interface{}) {
	if e.progress != nil {
		e.progress(fmt.Sprintf(format, args...))
	}
}

// Apply creates and configures the target, deploys its routes, and imports the
// selected backup. It resumes from the last durable internal checkpoint.
func (e *MigrationExecutor) Apply(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
) (MigrationPhaseResult, error) {
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if state.Status == MigrationStatusComplete {
		e.reportProgress("Migration is already complete; no apply steps are required.")
		return MigrationPhaseResult{Phase: MigrationPhaseVerify, State: state}, nil
	}
	e.reportProgress("Starting resumable migration apply.")
	if preparedHasImports(prepared) {
		if e.allowLiveSource {
			e.reportProgress("Preflight: validate the existing source backup before target changes (--force allows post-backup writes to be excluded).")
		} else {
			e.reportProgress("Preflight: validate maintenance mode and the fresh source backup before target changes.")
		}
		digest, err := export.BackupDigest()
		if err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "compute selected backup digest")
		}
		requireFresh := state.Source.BackupDigest == ""
		if !requireFresh && state.Source.BackupDigest != digest {
			return MigrationPhaseResult{}, errors.New("migration state is already bound to a different backup snapshot")
		}
		if _, err := prepareDataSync(export, prepared, e.now(), e.maxBackupAge, dataSyncOptions{
			requireFresh:    requireFresh,
			allowLiveSource: e.allowLiveSource,
		}); err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "apply preflight failed before target changes")
		}
		e.reportProgress("Apply preflight passed; target changes may begin.")
	}
	switch state.Phase {
	case MigrationPhasePlan, MigrationPhasePrepare:
		if _, err := e.Prepare(ctx, export, plan, prepared); err != nil {
			return MigrationPhaseResult{}, err
		}
	case MigrationPhaseSyncData:
		// Resume the data import below.
	default:
		return MigrationPhaseResult{}, errors.Errorf(
			"migration state cannot be applied from internal phase %q",
			state.Phase,
		)
	}
	return e.SyncData(ctx, export, plan, prepared)
}

// Prepare creates the target app and instances, reconciles configuration,
// deploys generated technical routes, creates custom routes, and deploys them
// before DNS changes. Public traffic remains on Wodby 1 until DNS is changed.
func (e *MigrationExecutor) Prepare(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
) (MigrationPhaseResult, error) {
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := e.startPhase(state, MigrationPhasePrepare); err != nil {
		return MigrationPhaseResult{}, err
	}
	e.reportProgress("Step: create or resume the target app and app instances.")

	app, instances, err := e.ensureAppAndInstances(ctx, state, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	for _, item := range prepared.Instances {
		instance := instances[item.Source.UUID]
		e.reportProgress("Step: configure target instance %q (ID %d).", item.Source.Name, instance.ID)
		if err := e.prepareInstance(ctx, state, item, instance); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	for _, item := range prepared.Instances {
		instance := instances[item.Source.UUID]
		e.reportProgress("Step: build and deploy target instance %q (ID %d).", item.Source.Name, instance.ID)
		if err := e.ensureTechnicalDeployment(ctx, state, item, instance, "prepare_deploy"); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	for _, item := range prepared.Instances {
		instance := instances[item.Source.UUID]
		instancePlan := planInstance(plan, item.Source.UUID)
		if instancePlan == nil {
			return MigrationPhaseResult{}, errors.New("migration plan is missing route inventory for an instance")
		}
		if !planHasCustomRoutes(instancePlan) {
			e.reportProgress("No custom routes need to be created for target instance %q.", item.Source.Name)
			continue
		}
		e.reportProgress("Step: create custom routes for target instance %q (ID %d).", item.Source.Name, instance.ID)
		if err := e.ensureCustomRoutes(ctx, state, plan, item, instance); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	for _, item := range prepared.Instances {
		instance := instances[item.Source.UUID]
		instancePlan := planInstance(plan, item.Source.UUID)
		if !planHasCustomRoutes(instancePlan) {
			e.reportProgress("No custom routes are staged for target instance %q; skipping the second deployment.", item.Source.Name)
			continue
		}
		e.reportProgress("Step: deploy staged routes for target instance %q (ID %d).", item.Source.Name, instance.ID)
		if err := e.ensureTechnicalDeployment(ctx, state, item, instance, "apply_deploy"); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	for _, item := range prepared.Instances {
		instance := instances[item.Source.UUID]
		if err := e.waitAppInstanceOK(ctx, instance.ID, "finish target preparation"); err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "wait for target instance after deployment")
		}
	}

	if err := state.SetAppTarget(app.ID, MigrationResourceReady); err != nil {
		return MigrationPhaseResult{}, err
	}
	for sourceID, instance := range instances {
		if err := state.SetInstanceTarget(sourceID, instance.ID, MigrationResourceReady); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	if err := state.SetStatus(MigrationStatusRunning); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return MigrationPhaseResult{}, err
	}
	e.reportProgress("Target app %q (ID %d) is prepared.", app.Name, app.ID)
	return MigrationPhaseResult{Phase: MigrationPhasePrepare, State: state}, nil
}

func planHasCustomRoutes(plan *InstancePlan) bool {
	if plan == nil {
		return false
	}
	for _, route := range plan.Routes {
		if route.Action == "create_backend" || route.Action == "create_redirect" {
			return true
		}
	}
	return false
}

// SyncData imports the backup snapshot selected by the current export. The
// normal path requires a fresh, write-frozen snapshot; the explicit live-source
// override accepts an existing snapshot. Imports run sequentially to reduce
// load. Backup URLs remain absent from CLI plan/state artifacts; Wodby 2 retains
// an active URL only until the corresponding import reaches a terminal state.
func (e *MigrationExecutor) SyncData(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
) (MigrationPhaseResult, error) {
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := requirePreparedState(state); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := e.startPhase(state, MigrationPhaseSyncData); err != nil {
		return MigrationPhaseResult{}, err
	}

	if !preparedHasImports(prepared) {
		e.reportProgress("No data imports are planned; data synchronization is complete.")
		return e.finishRunningPhase(state, MigrationPhaseSyncData)
	}
	if e.allowLiveSource {
		e.reportProgress("Step: validate the existing Wodby 1 backup snapshot and import its data (--force).")
	} else {
		e.reportProgress("Step: validate the fresh Wodby 1 backup snapshot and import its data.")
	}
	digest, err := export.BackupDigest()
	if err != nil {
		return MigrationPhaseResult{}, errors.Wrap(err, "compute selected backup digest")
	}
	requireFresh := state.Source.BackupDigest == ""
	if !requireFresh && state.Source.BackupDigest != digest {
		return MigrationPhaseResult{}, errors.New("migration state is already bound to a different backup snapshot")
	}
	imports, err := prepareDataSync(export, prepared, e.now(), e.maxBackupAge, dataSyncOptions{
		requireFresh:    requireFresh,
		allowLiveSource: e.allowLiveSource,
	})
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if requireFresh {
		if err := state.SetBackupDigest(digest); err != nil {
			return MigrationPhaseResult{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return MigrationPhaseResult{}, err
		}
		e.reportProgress("Source backup snapshot accepted and pinned to the migration state.")
	}
	for _, item := range imports {
		instanceState := state.Instances[item.SourceInstanceUUID]
		if instanceState == nil || instanceState.TargetID <= 0 {
			return MigrationPhaseResult{}, errors.New("migration state is missing a prepared target instance")
		}
		if !importOperationSucceeded(instanceState, item.SourceInstanceUUID, item.Backup.Component) {
			e.reportProgress("Refreshing the protected download URL for backup component %q...", item.Backup.Component)
			item, err = e.refreshDataImport(ctx, item, prepared, plan, digest)
			if err != nil {
				return MigrationPhaseResult{}, err
			}
		}
		if err := e.waitAppInstanceOK(ctx, instanceState.TargetID, "start the next data import"); err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "wait for target instance before data import")
		}
		services, err := e.target.ListAppServices(ctx, instanceState.TargetID)
		if err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "read target services before data import")
		}
		service, err := exactAppService(services, item.Destination.ServiceName)
		if err != nil {
			return MigrationPhaseResult{}, err
		}
		if service.Disabled ||
			service.ServiceRevID != item.Destination.StackService.StackService.ServiceRevID {
			return MigrationPhaseResult{}, errors.New("target import service no longer matches the approved enabled stack service")
		}
		if err := e.ensureImport(ctx, state, item, instanceState.TargetID, service.ID); err != nil {
			return MigrationPhaseResult{}, err
		}
		if err := e.waitAppInstanceOK(
			ctx,
			instanceState.TargetID,
			fmt.Sprintf("continue after the %q data import", item.Backup.Component),
		); err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "wait for target instance after data import")
		}
	}
	e.reportProgress("All planned data imports completed.")
	return e.finishRunningPhase(state, MigrationPhaseSyncData)
}

func (e *MigrationExecutor) refreshDataImport(
	ctx context.Context,
	expected PreparedDataImport,
	prepared PreparedMigration,
	plan Plan,
	backupDigest string,
) (PreparedDataImport, error) {
	if e.refreshSource == nil {
		return expected, nil
	}
	refreshed, err := e.refreshSource(ctx)
	if err != nil {
		return PreparedDataImport{}, errors.Wrap(err, "refresh source backup download URL")
	}
	configDigest, err := refreshed.MigrationConfigDigest()
	if err != nil {
		return PreparedDataImport{}, errors.Wrap(err, "verify refreshed source configuration")
	}
	if configDigest != plan.Source.ConfigDigest {
		return PreparedDataImport{}, errors.New("source configuration changed while refreshing a backup download URL")
	}
	refreshedDigest, err := refreshed.BackupDigest()
	if err != nil {
		return PreparedDataImport{}, errors.Wrap(err, "verify refreshed backup snapshot")
	}
	if refreshedDigest != backupDigest {
		return PreparedDataImport{}, errors.New("source backup snapshot changed after data synchronization started")
	}
	items, err := prepareDataSync(refreshed, prepared, e.now(), e.maxBackupAge, dataSyncOptions{
		allowLiveSource: e.allowLiveSource,
	})
	if err != nil {
		return PreparedDataImport{}, err
	}
	component := strings.ToLower(strings.TrimSpace(expected.Backup.Component))
	matches := []PreparedDataImport{}
	for _, item := range items {
		if item.SourceInstanceUUID == expected.SourceInstanceUUID &&
			strings.ToLower(strings.TrimSpace(item.Backup.Component)) == component {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return PreparedDataImport{}, errors.Errorf(
			"refreshed source export matched %d backup files for instance %q component %q",
			len(matches),
			expected.SourceInstanceUUID,
			component,
		)
	}
	return matches[0], nil
}

// ValidateFinalize checks the preconditions for final traffic cutover without
// starting a deployment. The source must remain write-frozen when data was
// imported.
func (e *MigrationExecutor) ValidateFinalize(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
	cluster TargetCluster,
) error {
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return err
	}
	return e.validateFinalizeReadiness(ctx, export, plan, prepared, cluster, state)
}

// Finalize revalidates cutover readiness, then deploys the staged routes.
func (e *MigrationExecutor) Finalize(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
	cluster TargetCluster,
) (MigrationPhaseResult, error) {
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := e.validateFinalizeReadiness(ctx, export, plan, prepared, cluster, state); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := e.startPhase(state, MigrationPhaseFinalize); err != nil {
		return MigrationPhaseResult{}, err
	}

	for _, item := range prepared.Instances {
		instanceState := state.Instances[item.Source.UUID]
		if instanceState == nil || instanceState.TargetID <= 0 {
			return MigrationPhaseResult{}, errors.New("migration state is missing a prepared target instance")
		}
		instance, err := e.target.GetAppInstance(ctx, instanceState.TargetID)
		if err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "read target instance before final deployment")
		}
		if err := e.ensureTechnicalDeployment(ctx, state, item, instance, "finalize_deploy"); err != nil {
			return MigrationPhaseResult{}, err
		}
		if err := e.waitCustomRoutes(ctx, planInstance(plan, item.Source.UUID), instance.ID); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	return e.finishRunningPhase(state, MigrationPhaseFinalize)
}

func (e *MigrationExecutor) validateFinalizeReadiness(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
	cluster TargetCluster,
	state *MigrationState,
) error {
	if err := requirePreparedState(state); err != nil {
		return err
	}
	if preparedHasImports(prepared) {
		if state.Source.BackupDigest == "" {
			return errors.New("data synchronization must complete before finalization")
		}
		if err := e.verifyCompletedImports(ctx, state, prepared); err != nil {
			return err
		}
		if !e.allowLiveSource {
			if err := requireMaintenanceMode(export, prepared); err != nil {
				return err
			}
		}
	}
	if cluster.ID != plan.Target.ClusterID {
		return errors.New("finalization cluster does not match the approved target")
	}
	if cluster.OrgID != plan.Target.OrgID || !strings.EqualFold(strings.TrimSpace(cluster.Status), "OK") {
		return errors.New("finalization cluster is not an available member of the approved organization")
	}
	if err := e.checkCustomDNS(ctx, plan, cluster); err != nil {
		return err
	}

	for _, item := range prepared.Instances {
		instanceState := state.Instances[item.Source.UUID]
		if instanceState == nil || instanceState.TargetID <= 0 {
			return errors.New("migration state is missing a prepared target instance")
		}
		if !item.SkipCode {
			continue
		}
		services, err := e.target.ListAppServices(ctx, instanceState.TargetID)
		if err != nil {
			return errors.Wrap(err, "read manually deployed target code services")
		}
		if err := validateManuallyDeployedCodeServices(item, services); err != nil {
			return err
		}
	}
	return nil
}

// Verify reads the migrated target resources and marks the migration complete
// only when the core relationships, service state, configuration, routes, and
// completed imports still match the approved source inventory.
func (e *MigrationExecutor) Verify(
	ctx context.Context,
	export Export,
	plan Plan,
	prepared PreparedMigration,
	cluster TargetCluster,
) (MigrationPhaseResult, error) {
	e.reportProgress("Starting migration verification.")
	state, _, err := e.loadState(export, plan, prepared)
	if err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := requirePreparedState(state); err != nil {
		return MigrationPhaseResult{}, err
	}
	if state.Status != MigrationStatusComplete && phaseRank(state.Phase) < phaseRank(MigrationPhaseSyncData) {
		return MigrationPhaseResult{}, errors.New("apply must complete before verification")
	}
	if preparedHasImports(prepared) {
		if state.Source.BackupDigest == "" {
			return MigrationPhaseResult{}, errors.New("apply must complete data imports before verification")
		}
		if err := e.verifyCompletedImports(ctx, state, prepared); err != nil {
			return MigrationPhaseResult{}, err
		}
		if err := requireMaintenanceMode(export, prepared); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	if cluster.ID != plan.Target.ClusterID || cluster.OrgID != plan.Target.OrgID ||
		!strings.EqualFold(strings.TrimSpace(cluster.Status), "OK") {
		return MigrationPhaseResult{}, errors.New("verification cluster does not match the available migration target")
	}
	if err := e.checkCustomDNS(ctx, plan, cluster); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := e.startPhase(state, MigrationPhaseVerify); err != nil {
		return MigrationPhaseResult{}, err
	}
	app, found, err := e.target.FindAppExact(ctx, plan.Target.OrgID, prepared.App.App.Name)
	if err != nil {
		return MigrationPhaseResult{}, errors.Wrap(err, "verify target app")
	}
	if !found || app.ID != state.App.TargetID {
		return MigrationPhaseResult{}, errors.New("target app no longer matches migration state")
	}
	e.reportProgress("Verified target app %q (ID %d).", app.Name, app.ID)
	for _, item := range prepared.Instances {
		instanceState := state.Instances[item.Source.UUID]
		if instanceState == nil || instanceState.TargetID <= 0 {
			return MigrationPhaseResult{}, errors.New("migration state is missing a target instance")
		}
		instance, err := e.target.GetAppInstance(ctx, instanceState.TargetID)
		if err != nil {
			return MigrationPhaseResult{}, errors.Wrap(err, "verify target instance")
		}
		instancePlan := planInstance(plan, item.Source.UUID)
		if instancePlan == nil || instance.AppID != app.ID ||
			instance.ClusterID != plan.Target.ClusterID ||
			instance.EnvID != instancePlan.TargetEnvID ||
			instance.StackRevID != item.Stack.RevID ||
			instance.Name != item.Source.Name {
			return MigrationPhaseResult{}, errors.New("target instance relationships no longer match the approved migration")
		}
		if err := e.verifyInstance(ctx, item, instance, instancePlan, instanceState); err != nil {
			return MigrationPhaseResult{}, err
		}
		e.reportProgress("Verified target instance %q (ID %d).", instance.Name, instance.ID)
		if err := state.SetInstanceTarget(item.Source.UUID, instance.ID, MigrationResourceReady); err != nil {
			return MigrationPhaseResult{}, err
		}
	}
	if err := state.SetAppTarget(app.ID, MigrationResourceReady); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := state.SetStatus(MigrationStatusComplete); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return MigrationPhaseResult{}, err
	}
	e.reportProgress("Migration verification completed successfully.")
	return MigrationPhaseResult{Phase: MigrationPhaseVerify, State: state}, nil
}

func (e *MigrationExecutor) loadState(
	export Export,
	plan Plan,
	prepared PreparedMigration,
) (*MigrationState, bool, error) {
	if err := export.ValidateSource(plan.Source.Kind, plan.Source.ID); err != nil {
		return nil, false, err
	}
	configDigest, err := export.MigrationConfigDigest()
	if err != nil {
		return nil, false, errors.Wrap(err, "compute source configuration digest")
	}
	if configDigest != plan.Source.ConfigDigest {
		return nil, false, errors.New("source configuration no longer matches the approved migration plan")
	}
	expectedPlanHash, err := plan.contentDigest()
	if err != nil {
		return nil, false, errors.Wrap(err, "verify migration plan hash")
	}
	if plan.PlanHash == "" || expectedPlanHash != plan.PlanHash {
		return nil, false, errors.New("migration plan hash is missing or invalid")
	}
	if plan.Status == "blocked" || plan.Summary.Blocking != 0 {
		return nil, false, errors.New("blocked migration plan cannot be executed")
	}
	if !plan.Target.DiscoveryVerified || !plan.Target.OrgOwnerOrAdminVerified ||
		plan.Target.OrgID <= 0 || plan.Target.ProjectID < 0 || plan.Target.ClusterID <= 0 {
		return nil, false, errors.New("approved target organization owner/admin discovery is required")
	}
	if len(prepared.Instances) == 0 {
		return nil, false, errors.New("prepared target mapping does not contain a source instance")
	}
	switch plan.Source.Kind {
	case "app":
		if prepared.App.App.UUID != plan.Source.ID {
			return nil, false, errors.New("prepared target mapping does not match the approved source app")
		}
	case "instance":
		if len(prepared.Instances) != 1 || prepared.Instances[0].Source.UUID != plan.Source.ID {
			return nil, false, errors.New("prepared target mapping does not match the approved source instance")
		}
	default:
		return nil, false, errors.Errorf("unsupported executable migration source kind %q", plan.Source.Kind)
	}
	sourceIDs := make([]string, 0, len(prepared.Instances))
	seen := map[string]bool{}
	for _, item := range prepared.Instances {
		if item.Source.UUID == "" || seen[item.Source.UUID] || planInstance(plan, item.Source.UUID) == nil {
			return nil, false, errors.New("prepared target mapping contains an invalid source instance set")
		}
		for _, service := range item.Source.Services {
			for _, variable := range service.EnvVars {
				if variable.Origin == "custom" && !variable.Enabled {
					return nil, false, errors.Errorf(
						"source custom environment variable %q is disabled and cannot be represented safely by the target API",
						variable.Name,
					)
				}
			}
		}
		seen[item.Source.UUID] = true
		sourceIDs = append(sourceIDs, item.Source.UUID)
	}
	sort.Strings(sourceIDs)
	identity := MigrationStateIdentity{
		Source: MigrationStateSourceIdentity{
			Kind:         plan.Source.Kind,
			ID:           plan.Source.ID,
			ConfigDigest: configDigest,
		},
		PlanHash: plan.PlanHash,
		Target: MigrationStateTarget{
			OrgID:     plan.Target.OrgID,
			ProjectID: plan.Target.ProjectID,
			ClusterID: plan.Target.ClusterID,
		},
	}
	return LoadOrInitializeMigrationState(e.statePath, identity, sourceIDs)
}

func (e *MigrationExecutor) startPhase(state *MigrationState, phase MigrationPhase) error {
	if state.Status == MigrationStatusComplete {
		if phase == MigrationPhaseVerify {
			return nil
		}
		return errors.New("completed migration cannot execute another phase")
	}
	if phaseRank(phase) < phaseRank(state.Phase) {
		return errors.Errorf("migration state is already past phase %q", phase)
	}
	if err := state.SetPhase(phase); err != nil {
		return err
	}
	if err := state.SetStatus(MigrationStatusRunning); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func (e *MigrationExecutor) finishRunningPhase(
	state *MigrationState,
	phase MigrationPhase,
) (MigrationPhaseResult, error) {
	if err := state.SetStatus(MigrationStatusRunning); err != nil {
		return MigrationPhaseResult{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return MigrationPhaseResult{}, err
	}
	return MigrationPhaseResult{Phase: phase, State: state}, nil
}

func phaseRank(phase MigrationPhase) int {
	switch phase {
	case MigrationPhasePlan:
		return 0
	case MigrationPhasePrepare:
		return 1
	case MigrationPhaseSyncData:
		return 2
	case MigrationPhaseFinalize:
		return 3
	case MigrationPhaseVerify:
		return 4
	default:
		return -1
	}
}

func requirePreparedState(state *MigrationState) error {
	if state.App.TargetID <= 0 || state.App.Status != MigrationResourceReady {
		return errors.New("target preparation must complete before data import or verification")
	}
	for _, item := range state.Instances {
		if item == nil || item.TargetID <= 0 || item.Status != MigrationResourceReady {
			return errors.New("target preparation must complete for every instance")
		}
	}
	return nil
}

func preparedHasImports(prepared PreparedMigration) bool {
	for _, item := range prepared.Instances {
		if len(item.ImportByComponent) != 0 {
			return true
		}
	}
	return false
}

func (e *MigrationExecutor) verifyCompletedImports(
	ctx context.Context,
	state *MigrationState,
	prepared PreparedMigration,
) error {
	for _, instance := range prepared.Instances {
		resource := state.Instances[instance.Source.UUID]
		if resource == nil || resource.TargetID <= 0 {
			return errors.New("migration state is missing an imported target instance")
		}
		services, err := e.target.ListAppServices(ctx, resource.TargetID)
		if err != nil {
			return errors.Wrap(err, "verify import target services before finalization")
		}
		for component, destination := range instance.ImportByComponent {
			item, ok := successfulImportOperation(resource, instance.Source.UUID, component)
			if !ok || item.TargetID <= 0 {
				return errors.Errorf("data import for component %q has not completed", component)
			}
			service, err := exactAppService(services, destination.ServiceName)
			if err != nil {
				return err
			}
			imported, err := e.target.GetImport(ctx, item.TargetID)
			if err != nil {
				return errors.Wrap(err, "verify data import before finalization")
			}
			if err := validateRecoveredImport(
				imported,
				resource.TargetID,
				service.ID,
				destination.ImportName,
			); err != nil {
				return err
			}
			if !strings.EqualFold(strings.TrimSpace(imported.Status), "COMPLETED") {
				return errors.Errorf("data import for component %q has target status %q", component, imported.Status)
			}
		}
	}
	return nil
}

func requireMaintenanceMode(export Export, prepared PreparedMigration) error {
	instances := map[string]Instance{}
	for _, app := range export.AppExports() {
		for _, item := range app.Instances {
			instances[item.UUID] = item
		}
	}
	for _, item := range prepared.Instances {
		if len(item.ImportByComponent) == 0 {
			continue
		}
		current, ok := instances[item.Source.UUID]
		if !ok || !sourceMaintenanceMode(current.Properties) {
			return errors.Errorf("source instance %q must remain in maintenance mode through finalization", item.Source.Name)
		}
	}
	return nil
}

func (e *MigrationExecutor) ensureAppAndInstances(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedMigration,
) (TargetApp, map[string]TargetAppInstance, error) {
	initial := prepared.Instances[0]
	initialPlan := planInstance(plan, initial.Source.UUID)
	if initialPlan == nil {
		return TargetApp{}, nil, errors.New("migration plan is missing the initial instance")
	}
	app, first, err := e.ensureApp(ctx, state, plan, prepared.App.App, initial, *initialPlan)
	if err != nil {
		return TargetApp{}, nil, err
	}
	result := map[string]TargetAppInstance{initial.Source.UUID: first}
	for _, item := range prepared.Instances[1:] {
		itemPlan := planInstance(plan, item.Source.UUID)
		if itemPlan == nil {
			return TargetApp{}, nil, errors.New("migration plan is missing a source instance")
		}
		instance, err := e.ensureInstance(ctx, state, plan, app, item, *itemPlan)
		if err != nil {
			return TargetApp{}, nil, err
		}
		result[item.Source.UUID] = instance
	}
	return app, result, nil
}

func (e *MigrationExecutor) ensureApp(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	sourceApp App,
	initial PreparedInstance,
	initialPlan InstancePlan,
) (TargetApp, TargetAppInstance, error) {
	const operation = "create"
	retryOperation := appCreateAmbiguousRetryOperation(state.Source.ID)
	appOp, hasAppOp := state.App.Operations[operation]
	instanceState := state.Instances[initial.Source.UUID]
	if instanceState == nil {
		return TargetApp{}, TargetAppInstance{}, errors.New("migration state is missing the initial source instance")
	}
	instanceOp, hasInstanceOp := instanceState.Operations[operation]

	app, appFound, err := e.target.FindAppExact(ctx, plan.Target.OrgID, sourceApp.Name)
	if err != nil {
		return TargetApp{}, TargetAppInstance{}, errors.Wrap(err, "look up target app")
	}
	if state.App.TargetID > 0 {
		if !appFound || app.ID != state.App.TargetID {
			return TargetApp{}, TargetAppInstance{}, errors.New("target app no longer matches migration state")
		}
		first, err := e.findExpectedInstance(
			ctx,
			plan.Target.OrgID,
			app,
			initial,
			initialPlan,
			plan.Target.ClusterID,
		)
		if err != nil {
			return TargetApp{}, TargetAppInstance{}, err
		}
		if instanceState.TargetID > 0 && first.ID != instanceState.TargetID {
			return TargetApp{}, TargetAppInstance{}, errors.New("target initial instance no longer matches migration state")
		}
		if instanceState.TargetID == 0 {
			if !hasInstanceOp ||
				(instanceOp.Status != MigrationOperationIntent && instanceOp.Status != MigrationOperationAmbiguous) ||
				!createdWithinOperation(first.CreatedAt, instanceOp) {
				return TargetApp{}, TargetAppInstance{}, errors.New("target initial instance cannot be safely recovered from migration state")
			}
			if err := promoteInstanceOperationForRecovery(state, initial.Source.UUID, operation); err != nil {
				return TargetApp{}, TargetAppInstance{}, err
			}
			if err := state.MarkInstanceOperationSuccessWithIDs(initial.Source.UUID, operation, first.ID, 0); err != nil {
				return TargetApp{}, TargetAppInstance{}, err
			}
			if err := state.SetInstanceTarget(initial.Source.UUID, first.ID, MigrationResourceCreating); err != nil {
				return TargetApp{}, TargetAppInstance{}, err
			}
			if err := SaveMigrationState(e.statePath, state); err != nil {
				return TargetApp{}, TargetAppInstance{}, err
			}
		}
		e.reportProgress(
			"Resuming target app %q (ID %d), instance %q (ID %d) from the saved migration state.",
			app.Name,
			app.ID,
			first.Name,
			first.ID,
		)
		return app, first, nil
	}

	recoverable := (hasAppOp && (appOp.Status == MigrationOperationIntent || appOp.Status == MigrationOperationAmbiguous)) ||
		(hasInstanceOp && (instanceOp.Status == MigrationOperationIntent || instanceOp.Status == MigrationOperationAmbiguous))
	if appFound {
		if !recoverable {
			return TargetApp{}, TargetAppInstance{}, errors.New("target organization already contains an app with the migration name")
		}
		appWindow := appOp
		if appWindow.IntentAt.IsZero() {
			appWindow = instanceOp
		}
		if !createdWithinOperation(app.CreatedAt, appWindow) {
			return TargetApp{}, TargetAppInstance{}, errors.New("existing target app predates the recorded create operation and cannot be adopted")
		}
		first, err := e.findExpectedInstance(
			ctx,
			plan.Target.OrgID,
			app,
			initial,
			initialPlan,
			plan.Target.ClusterID,
		)
		if err != nil {
			return TargetApp{}, TargetAppInstance{}, err
		}
		instanceWindow := instanceOp
		if instanceWindow.IntentAt.IsZero() {
			instanceWindow = appWindow
		}
		if !createdWithinOperation(first.CreatedAt, instanceWindow) {
			return TargetApp{}, TargetAppInstance{}, errors.New("existing target initial instance predates the recorded create operation and cannot be adopted")
		}
		if err := e.recordRecoveredApp(state, operation, app.ID, initial.Source.UUID, first.ID); err != nil {
			return TargetApp{}, TargetAppInstance{}, err
		}
		e.reportProgress(
			"Recovered target app %q (ID %d), instance %q (ID %d) from the saved create operation.",
			app.Name,
			app.ID,
			first.Name,
			first.ID,
		)
		return app, first, nil
	}
	if recoverable && !e.ambiguousRetryAuthorized(retryOperation) {
		return TargetApp{}, TargetAppInstance{}, ambiguousRetryRequiredError(
			"target app create result is ambiguous and no timestamp-bounded match was found",
			retryOperation,
		)
	}

	var projectID *int
	if plan.Target.ProjectID > 0 {
		projectID = &plan.Target.ProjectID
	}
	ciIntegrationID := plan.Target.CIIntegrationID
	createInput := TargetCreateAppInput{
		OrgID:                  plan.Target.OrgID,
		Name:                   sourceApp.Name,
		Title:                  sourceApp.Title,
		InstanceName:           initial.Source.Name,
		InstanceTitle:          initial.Source.Title,
		ProjectID:              projectID,
		StackRevID:             initial.Stack.RevID,
		ClusterID:              plan.Target.ClusterID,
		EnvID:                  initialPlan.TargetEnvID,
		CIIntegrationID:        &ciIntegrationID,
		DeferInitialDeployment: true,
	}
	if err := validateTargetCreateAppInput(createInput); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.MarkAppOperationIntent(operation); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.MarkInstanceOperationIntent(initial.Source.UUID, operation); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.SetAppTarget(0, MigrationResourceCreating); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.SetInstanceTarget(initial.Source.UUID, 0, MigrationResourceCreating); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}

	e.reportProgress(
		"Creating target app %q with initial instance %q in cluster ID %d (automatic initial deployment deferred)...",
		createInput.Name,
		createInput.InstanceName,
		createInput.ClusterID,
	)
	created, err := e.target.CreateApp(ctx, createInput)
	if err != nil {
		return TargetApp{}, TargetAppInstance{}, e.recordPairMutationError(state, initial.Source.UUID, operation, "app creation", err)
	}
	if err := state.MarkAppOperationSuccessWithIDs(operation, created.ID, 0); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.SetAppTarget(created.ID, MigrationResourceCreating); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	e.reportProgress("Target app %q created (ID %d).", created.Name, created.ID)

	first, err := e.findExpectedInstance(
		ctx,
		plan.Target.OrgID,
		created,
		initial,
		initialPlan,
		plan.Target.ClusterID,
	)
	if err != nil {
		_ = state.MarkInstanceOperationAmbiguous(initial.Source.UUID, operation)
		_ = SaveMigrationState(e.statePath, state)
		return TargetApp{}, TargetAppInstance{}, errors.New("target app was created but its initial instance could not be identified; resume after inspecting the target")
	}
	if err := state.MarkInstanceOperationSuccessWithIDs(initial.Source.UUID, operation, first.ID, 0); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := state.SetInstanceTarget(initial.Source.UUID, first.ID, MigrationResourceCreating); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetApp{}, TargetAppInstance{}, err
	}
	e.reportProgress("Initial target instance %q created (ID %d).", first.Name, first.ID)
	return created, first, nil
}

func (e *MigrationExecutor) ensureInstance(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	app TargetApp,
	prepared PreparedInstance,
	instancePlan InstancePlan,
) (TargetAppInstance, error) {
	const operation = "create"
	resource := state.Instances[prepared.Source.UUID]
	if resource == nil {
		return TargetAppInstance{}, errors.New("migration state is missing a source instance")
	}
	foundInstance, found, err := e.target.FindAppInstanceExact(
		ctx,
		plan.Target.OrgID,
		app.Name,
		prepared.Source.Name,
	)
	if err != nil {
		return TargetAppInstance{}, errors.Wrap(err, "look up target app instance")
	}
	if resource.TargetID > 0 {
		if !found || foundInstance.ID != resource.TargetID {
			return TargetAppInstance{}, errors.New("target app instance no longer matches migration state")
		}
		if err := validatePreparedInstance(foundInstance, app.ID, prepared, instancePlan, plan.Target.ClusterID); err != nil {
			return TargetAppInstance{}, err
		}
		e.reportProgress("Resuming target instance %q (ID %d) from the saved migration state.", foundInstance.Name, foundInstance.ID)
		return foundInstance, nil
	}

	op, hasOp := resource.Operations[operation]
	recoverable := hasOp && (op.Status == MigrationOperationIntent || op.Status == MigrationOperationAmbiguous)
	if found {
		if !recoverable {
			return TargetAppInstance{}, errors.New("target app already contains an instance with the migration name")
		}
		if !createdWithinOperation(foundInstance.CreatedAt, op) {
			return TargetAppInstance{}, errors.New("existing target app instance predates the recorded create operation and cannot be adopted")
		}
		if err := validatePreparedInstance(foundInstance, app.ID, prepared, instancePlan, plan.Target.ClusterID); err != nil {
			return TargetAppInstance{}, err
		}
		if err := promoteInstanceOperationForRecovery(state, prepared.Source.UUID, operation); err != nil {
			return TargetAppInstance{}, err
		}
		if err := state.MarkInstanceOperationSuccessWithIDs(prepared.Source.UUID, operation, foundInstance.ID, 0); err != nil {
			return TargetAppInstance{}, err
		}
		if err := state.SetInstanceTarget(prepared.Source.UUID, foundInstance.ID, MigrationResourceCreating); err != nil {
			return TargetAppInstance{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return TargetAppInstance{}, err
		}
		e.reportProgress("Recovered target instance %q (ID %d) from the saved create operation.", foundInstance.Name, foundInstance.ID)
		return foundInstance, nil
	}
	retryOperation := instanceAmbiguousRetryOperation(prepared.Source.UUID, operation)
	if recoverable && !e.ambiguousRetryAuthorized(retryOperation) {
		return TargetAppInstance{}, ambiguousRetryRequiredError(
			"target app instance create result is ambiguous and no timestamp-bounded match was found",
			retryOperation,
		)
	}

	ciIntegrationID := plan.Target.CIIntegrationID
	input := TargetCreateAppInstanceInput{
		AppID:                  app.ID,
		InstanceName:           prepared.Source.Name,
		InstanceTitle:          prepared.Source.Title,
		StackRevID:             prepared.Stack.RevID,
		ClusterID:              plan.Target.ClusterID,
		EnvID:                  instancePlan.TargetEnvID,
		CIIntegrationID:        &ciIntegrationID,
		DeferInitialDeployment: true,
	}
	if err := validateTargetCreateAppInstanceInput(input); err != nil {
		return TargetAppInstance{}, err
	}
	if err := state.MarkInstanceOperationIntent(prepared.Source.UUID, operation); err != nil {
		return TargetAppInstance{}, err
	}
	if err := state.SetInstanceTarget(prepared.Source.UUID, 0, MigrationResourceCreating); err != nil {
		return TargetAppInstance{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetAppInstance{}, err
	}
	e.reportProgress("Creating target instance %q in app %q (ID %d; automatic initial deployment deferred)...", input.InstanceName, app.Name, app.ID)
	created, err := e.target.CreateAppInstance(ctx, input)
	if err != nil {
		return TargetAppInstance{}, e.recordInstanceCreationMutationError(
			state,
			prepared.Source.UUID,
			operation,
			"app instance creation",
			err,
		)
	}
	if err := state.MarkInstanceOperationSuccessWithIDs(prepared.Source.UUID, operation, created.ID, 0); err != nil {
		return TargetAppInstance{}, err
	}
	if err := state.SetInstanceTarget(prepared.Source.UUID, created.ID, MigrationResourceCreating); err != nil {
		return TargetAppInstance{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetAppInstance{}, err
	}
	e.reportProgress("Target instance %q created (ID %d).", created.Name, created.ID)
	return created, nil
}

func (e *MigrationExecutor) findExpectedInstance(
	ctx context.Context,
	orgID int,
	app TargetApp,
	prepared PreparedInstance,
	instancePlan InstancePlan,
	clusterID int,
) (TargetAppInstance, error) {
	item, found, err := e.target.FindAppInstanceExact(ctx, orgID, app.Name, prepared.Source.Name)
	if err != nil {
		return TargetAppInstance{}, err
	}
	if !found {
		return TargetAppInstance{}, errors.New("target app initial instance was not found")
	}
	if err := validatePreparedInstance(item, app.ID, prepared, instancePlan, clusterID); err != nil {
		return TargetAppInstance{}, err
	}
	return item, nil
}

func validatePreparedInstance(
	item TargetAppInstance,
	appID int,
	prepared PreparedInstance,
	plan InstancePlan,
	clusterID int,
) error {
	if item.AppID != appID || item.Name != prepared.Source.Name ||
		item.EnvID != plan.TargetEnvID || item.StackRevID != prepared.Stack.RevID {
		return errors.New("target app instance relationships do not match the approved migration")
	}
	if clusterID > 0 && item.ClusterID != clusterID {
		return errors.New("target app instance cluster does not match the approved migration")
	}
	return nil
}

const migrationRecoveryWindow = 5 * time.Minute

func createdWithinOperation(createdAt time.Time, operation MigrationOperationState) bool {
	if !createdAfterOperationIntent(createdAt, operation) {
		return false
	}
	// API timestamps may be serialized only to whole seconds. Truncation keeps
	// same-second responses recoverable. The fixed upper bound prevents a later
	// unrelated resource with the same natural key from being adopted.
	upper := operation.IntentAt.UTC().Add(migrationRecoveryWindow)
	return !createdAt.UTC().After(upper)
}

func createdAfterOperationIntent(createdAt time.Time, operation MigrationOperationState) bool {
	if createdAt.IsZero() || operation.IntentAt.IsZero() {
		return false
	}
	lower := operation.IntentAt.UTC().Truncate(time.Second)
	return !createdAt.UTC().Before(lower)
}

func (e *MigrationExecutor) recordRecoveredApp(
	state *MigrationState,
	operation string,
	appID int,
	sourceInstanceID string,
	instanceID int,
) error {
	if !operationSucceeded(&state.App, operation) {
		if err := promoteAppOperationForRecovery(state, operation); err != nil {
			return err
		}
		if err := state.MarkAppOperationSuccessWithIDs(operation, appID, 0); err != nil {
			return err
		}
	}
	if !operationSucceeded(state.Instances[sourceInstanceID], operation) {
		if err := promoteInstanceOperationForRecovery(state, sourceInstanceID, operation); err != nil {
			return err
		}
		if err := state.MarkInstanceOperationSuccessWithIDs(sourceInstanceID, operation, instanceID, 0); err != nil {
			return err
		}
	}
	if err := state.SetAppTarget(appID, MigrationResourceCreating); err != nil {
		return err
	}
	if err := state.SetInstanceTarget(sourceInstanceID, instanceID, MigrationResourceCreating); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func promoteAppOperationForRecovery(state *MigrationState, operation string) error {
	item, ok := state.App.Operations[operation]
	if !ok {
		return errors.New("migration app operation intent is missing")
	}
	if item.Status == MigrationOperationSucceeded {
		return nil
	}
	if item.Status != MigrationOperationIntent && item.Status != MigrationOperationAmbiguous {
		return errors.New("migration app operation is not recoverable")
	}
	item.Status = MigrationOperationIntent
	state.App.Operations[operation] = item
	if state.Status == MigrationStatusFailed {
		state.Status = MigrationStatusRunning
	}
	return state.Validate()
}

func promoteInstanceOperationForRecovery(state *MigrationState, sourceID, operation string) error {
	resource := state.Instances[sourceID]
	if resource == nil {
		return errors.New("migration instance operation state is missing")
	}
	item, ok := resource.Operations[operation]
	if !ok {
		return errors.New("migration instance operation intent is missing")
	}
	if item.Status == MigrationOperationSucceeded {
		return nil
	}
	if item.Status != MigrationOperationIntent &&
		item.Status != MigrationOperationAccepted &&
		item.Status != MigrationOperationAmbiguous {
		return errors.New("migration instance operation is not recoverable")
	}
	item.Status = MigrationOperationIntent
	resource.Operations[operation] = item
	if state.Status == MigrationStatusFailed {
		state.Status = MigrationStatusRunning
	}
	return state.Validate()
}

func (e *MigrationExecutor) prepareInstance(
	ctx context.Context,
	state *MigrationState,
	prepared PreparedInstance,
	instance TargetAppInstance,
) error {
	services, err := e.target.ListAppServices(ctx, instance.ID)
	if err != nil {
		return errors.Wrap(err, "list target app services for preparation")
	}
	byName, err := indexAppServices(services)
	if err != nil {
		return err
	}
	for _, inspection := range prepared.StackServices {
		target, ok := byName[inspection.StackService.Name]
		if !ok {
			return errors.Errorf("target instance is missing stack service %q", inspection.StackService.Name)
		}
		if target.ServiceRevID != inspection.StackService.ServiceRevID {
			return errors.Errorf("target service %q revision no longer matches preflight", inspection.StackService.Name)
		}
		enabled := prepared.EffectiveState[inspection.StackService.Name]
		if err := e.ensureServiceEnabled(
			ctx,
			state,
			prepared.Source.UUID,
			target,
			enabled,
		); err != nil {
			return err
		}
	}
	if prepared.BuildSource != nil {
		service, ok := byName[prepared.BuildSource.ServiceName]
		if !ok {
			return errors.New("target code service disappeared after preflight")
		}
		if err := e.ensureBuildSource(
			ctx,
			state,
			prepared.Source.UUID,
			service,
			*prepared.BuildSource,
		); err != nil {
			return err
		}
	}
	for _, sourceService := range prepared.Source.Services {
		if !sourceService.Enabled {
			continue
		}
		mapping, ok := prepared.Services[sourceService.Name]
		if !ok {
			continue
		}
		target, ok := byName[mapping.Target.StackService.Name]
		if !ok {
			return errors.New("mapped target service disappeared after preflight")
		}
		if err := e.ensureServiceVersion(
			ctx,
			state,
			prepared.Source.UUID,
			target,
			mapping.TargetVersion,
		); err != nil {
			return err
		}
		if err := e.ensureServiceEnvironment(
			ctx,
			state,
			prepared.Source.UUID,
			target,
			sourceService,
			prepared.Source.Properties,
		); err != nil {
			return err
		}
		if err := e.ensureServiceSettings(ctx, state, prepared.Source.UUID, target, sourceService); err != nil {
			return err
		}
		if err := e.ensureServiceCrons(
			ctx,
			state,
			prepared.Source.UUID,
			target,
			sourceService,
			mapping.Target,
			prepared.DisableCronSchedules,
		); err != nil {
			return err
		}
	}
	return nil
}

func (e *MigrationExecutor) ensureServiceVersion(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	version string,
) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	operation := operationKey("service_version", strconv.Itoa(service.ID))
	if service.Version == version {
		e.reportProgress("Service %q (ID %d) already uses version %s.", service.Name, service.ID, version)
		return e.recordObservedInstanceOperation(state, sourceID, operation, service.ID, 0)
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Changing service %q (ID %d) version from %s to %s...", service.Name, service.ID, emptyVersionLabel(service.Version), version)
	updated, err := e.target.UpdateAppService(ctx, service.ID, TargetAppServiceUpdateInput{Version: &version})
	if err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "service version update", err)
	}
	if updated.Version != version {
		return e.recordInstanceMutationError(
			state,
			sourceID,
			operation,
			"service version update",
			errors.Errorf("target returned version %q, expected %q", updated.Version, version),
		)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, service.ID, 0); err != nil {
		return err
	}
	e.reportProgress("Service %q now uses version %s.", service.Name, version)
	return nil
}

func emptyVersionLabel(version string) string {
	if strings.TrimSpace(version) == "" {
		return "<unspecified>"
	}
	return version
}

func indexAppServices(items []TargetAppService) (map[string]TargetAppService, error) {
	result := make(map[string]TargetAppService, len(items))
	for _, item := range items {
		if _, exists := result[item.Name]; exists {
			return nil, &TargetAmbiguousMatchError{
				Resource: "app service",
				Name:     item.Name,
				Count:    2,
			}
		}
		result[item.Name] = item
	}
	return result, nil
}

func exactAppService(items []TargetAppService, name string) (TargetAppService, error) {
	indexed, err := indexAppServices(items)
	if err != nil {
		return TargetAppService{}, err
	}
	item, ok := indexed[name]
	if !ok {
		return TargetAppService{}, errors.Errorf("target app service %q was not found", name)
	}
	return item, nil
}

func (e *MigrationExecutor) ensureServiceEnabled(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	enabled bool,
) error {
	operation := operationKey("service_state", strconv.Itoa(service.ID))
	desiredDisabled := !enabled
	if service.Disabled == desiredDisabled {
		e.reportProgress("Service %q (ID %d) is already %s.", service.Name, service.ID, enabledStateLabel(enabled))
		return e.recordObservedInstanceOperation(state, sourceID, operation, service.ID, 0)
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Setting service %q (ID %d) to %s...", service.Name, service.ID, enabledStateLabel(enabled))
	if _, err := e.target.UpdateAppService(ctx, service.ID, TargetAppServiceUpdateInput{
		Disabled: &desiredDisabled,
	}); err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "service state update", err)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, service.ID, 0); err != nil {
		return err
	}
	e.reportProgress("Service %q is now %s.", service.Name, enabledStateLabel(enabled))
	return nil
}

func enabledStateLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (e *MigrationExecutor) ensureBuildSource(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	build PreparedBuildSource,
) error {
	operation := operationKey("build_source", strconv.Itoa(service.ID))
	if operationSucceeded(state.Instances[sourceID], operation) {
		e.reportProgress("Build source for service %q (ID %d) is already configured by this migration.", service.Name, service.ID)
		return nil
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Configuring build source for service %q (ID %d)...", service.Name, service.ID)
	if _, err := e.target.UpdateAppServiceBuildSource(ctx, service.ID, build.Input); err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "build source update", err)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, service.ID, 0); err != nil {
		return err
	}
	e.reportProgress("Build source for service %q configured.", service.Name)
	return nil
}

func (e *MigrationExecutor) ensureServiceEnvironment(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	source Service,
	properties map[string]interface{},
) error {
	current, err := e.target.ListAppServiceEnvVars(ctx, service.ID)
	if err != nil {
		return errors.Wrap(err, "list target service environment variables")
	}
	byName := map[string][]TargetAppServiceEnvVar{}
	for _, item := range current {
		if !targetMutableGlobalEnvVar(item) {
			// The target read endpoint also returns compiled stack/service
			// defaults and scoped entries. They cannot represent the global
			// override being migrated here.
			continue
		}
		byName[item.Name] = append(byName[item.Name], item)
	}
	for _, variable := range source.EnvVars {
		if !sourceEnvVarRequiresMigration(properties, variable) {
			continue
		}
		if variable.IsRedacted() {
			return errors.Errorf("source environment variable %q is redacted and cannot be migrated", variable.Name)
		}
		if strings.TrimSpace(variable.Name) == "" {
			return errors.New("source custom environment variable name is empty")
		}
		matches := byName[variable.Name]
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{
				Resource: "app service environment variable",
				Name:     variable.Name,
				Count:    len(matches),
			}
		}
		operation := operationKey("env", strconv.Itoa(service.ID), variable.Name)
		runtime, build := true, false
		if len(matches) == 1 {
			item := matches[0]
			matchesDesired := item.Runtime == runtime && item.Build == build
			if variable.Secret || variable.Protected {
				matchesDesired = matchesDesired && item.ValueSecretID != nil
			} else {
				matchesDesired = matchesDesired && item.ValueSecretID == nil && item.Value == variable.Value
			}
			succeeded := operationSucceeded(state.Instances[sourceID], operation)
			if succeeded {
				if !matchesDesired {
					return errors.Errorf(
						"target environment variable %q changed after the migration operation completed",
						variable.Name,
					)
				}
				e.reportProgress("Environment variable %q on service %q is already migrated.", variable.Name, service.Name)
				continue
			}
			if matchesDesired && !(variable.Secret || variable.Protected) {
				if err := e.recordObservedInstanceOperation(state, sourceID, operation, item.ID, 0); err != nil {
					return err
				}
				e.reportProgress("Environment variable %q on service %q already matches the source.", variable.Name, service.Name)
				continue
			}
			run, err := e.beginInstanceMutation(state, sourceID, operation, false)
			if err != nil || !run {
				return err
			}
			e.reportProgress("Updating environment variable %q on service %q...", variable.Name, service.Name)
			value := variable.Value
			if _, err := e.target.UpdateAppServiceEnvVar(ctx, item.ID, TargetUpdateAppServiceEnvVarInput{
				Value:   &value,
				Secret:  variable.Secret || variable.Protected,
				Runtime: &runtime,
				Build:   &build,
			}); err != nil {
				return e.recordInstanceMutationError(state, sourceID, operation, "environment variable update", err)
			}
			if err := e.completeInstanceMutation(state, sourceID, operation, item.ID, 0); err != nil {
				return err
			}
			e.reportProgress("Environment variable %q on service %q updated.", variable.Name, service.Name)
			continue
		}
		if operationSucceeded(state.Instances[sourceID], operation) {
			return errors.Errorf(
				"target environment variable %q is missing after the migration operation completed",
				variable.Name,
			)
		}

		run, err := e.beginInstanceMutation(state, sourceID, operation, false)
		if err != nil || !run {
			return err
		}
		e.reportProgress("Creating environment variable %q on service %q...", variable.Name, service.Name)
		created, err := e.target.CreateAppServiceEnvVar(ctx, service.ID, TargetCreateAppServiceEnvVarInput{
			Name:    variable.Name,
			Value:   variable.Value,
			Secret:  variable.Secret || variable.Protected,
			Runtime: &runtime,
			Build:   &build,
		})
		if err != nil {
			return e.recordInstanceMutationError(state, sourceID, operation, "environment variable creation", err)
		}
		if err := e.completeInstanceMutation(state, sourceID, operation, created.ID, 0); err != nil {
			return err
		}
		e.reportProgress("Environment variable %q on service %q created (ID %d).", variable.Name, service.Name, created.ID)
	}
	return nil
}

func targetMutableGlobalEnvVar(item TargetAppServiceEnvVar) bool {
	return item.ID > 0 &&
		item.Source == nil &&
		item.Workload == "" &&
		item.Container == ""
}

func (e *MigrationExecutor) ensureServiceSettings(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	source Service,
) error {
	if len(source.Configuration) == 0 {
		return nil
	}
	current, err := e.target.ListAppServiceSettings(ctx, service.ID)
	if err != nil {
		return errors.Wrap(err, "list target service settings")
	}
	byName := map[string][]TargetAppServiceSetting{}
	for _, item := range current {
		byName[item.Name] = append(byName[item.Name], item)
	}
	names := make([]string, 0, len(source.Configuration))
	for name := range source.Configuration {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value, err := scalarConfigurationValue(source.Configuration[name])
		if err != nil {
			return errors.Wrapf(err, "source service setting %q", name)
		}
		matches := byName[name]
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{
				Resource: "app service setting",
				Name:     name,
				Count:    len(matches),
			}
		}
		operation := operationKey("setting", strconv.Itoa(service.ID), name)
		if len(matches) == 1 && matches[0].Value == value {
			if err := e.recordObservedInstanceOperation(state, sourceID, operation, matches[0].ID, 0); err != nil {
				return err
			}
			e.reportProgress("Setting %q on service %q already matches the source.", name, service.Name)
			continue
		}
		run, err := e.beginInstanceMutation(state, sourceID, operation, false)
		if err != nil || !run {
			return err
		}
		e.reportProgress("Applying setting %q on service %q...", name, service.Name)
		updated, err := e.target.SetAppServiceSetting(ctx, service.ID, name, value)
		if err != nil {
			return e.recordInstanceMutationError(state, sourceID, operation, "service setting update", err)
		}
		if err := e.completeInstanceMutation(state, sourceID, operation, updated.ID, 0); err != nil {
			return err
		}
		e.reportProgress("Setting %q on service %q applied.", name, service.Name)
	}
	return nil
}

func scalarConfigurationValue(value interface{}) (string, error) {
	switch item := value.(type) {
	case string:
		return item, nil
	case bool:
		return strconv.FormatBool(item), nil
	case json.Number:
		return item.String(), nil
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(item), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(item), nil
	case int64:
		return strconv.FormatInt(item, 10), nil
	case nil:
		return "", nil
	default:
		return "", errors.New("configuration value is not a supported scalar")
	}
}

func (e *MigrationExecutor) ensureServiceCrons(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	source Service,
	inspection TargetStackServiceInspection,
	disableMigrated bool,
) error {
	current, err := e.target.ListAppServiceCronSchedules(ctx, service.ID)
	if err != nil {
		return errors.Wrap(err, "list target service cron schedules")
	}
	if err := e.disableDefaultPHPCronSchedules(
		ctx,
		state,
		sourceID,
		service,
		inspection,
		current,
	); err != nil {
		return err
	}
	byName := map[string][]TargetAppServiceCronSchedule{}
	for _, item := range current {
		byName[item.Name] = append(byName[item.Name], item)
	}
	for index, cron := range source.CronJobs {
		if !cron.Enabled || cron.Classification == "source_only_infrastructure" {
			continue
		}
		if strings.TrimSpace(cron.Crontab) == "" || strings.TrimSpace(cron.Command) == "" {
			return errors.New("source application cron requires both schedule and command")
		}
		name := "w1-" + shortDigest(sourceID, source.Name, strconv.Itoa(index), cron.Crontab, cron.Command)
		matches := byName[name]
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{Resource: "app service cron schedule", Name: name, Count: len(matches)}
		}
		operation := operationKey("cron", strconv.Itoa(service.ID), name)
		title := strings.TrimSpace(cron.Title)
		if title == "" {
			title = "Migrated Wodby 1 cron"
		}
		desiredDisabled := disableMigrated
		if len(matches) == 1 {
			item := matches[0]
			if item.Title == title && item.Crontab == cron.Crontab && item.Command == cron.Command && item.Disabled == desiredDisabled {
				if err := e.recordObservedInstanceOperation(state, sourceID, operation, item.ID, 0); err != nil {
					return err
				}
				e.reportProgress("Cron schedule %q on service %q already matches the source.", title, service.Name)
				continue
			}
			run, err := e.beginInstanceMutation(state, sourceID, operation, false)
			if err != nil || !run {
				return err
			}
			e.reportProgress("Updating cron schedule %q on service %q...", title, service.Name)
			if _, err := e.target.UpdateAppServiceCronSchedule(ctx, item.ID, TargetUpdateAppServiceCronScheduleInput{
				Disabled: &desiredDisabled,
				Title:    &title,
				Crontab:  &cron.Crontab,
				Command:  &cron.Command,
			}); err != nil {
				return e.recordInstanceMutationError(state, sourceID, operation, "cron schedule update", err)
			}
			if err := e.completeInstanceMutation(state, sourceID, operation, item.ID, 0); err != nil {
				return err
			}
			e.reportProgress("Cron schedule %q on service %q updated.", title, service.Name)
			continue
		}
		run, err := e.beginInstanceMutation(state, sourceID, operation, false)
		if err != nil || !run {
			return err
		}
		e.reportProgress("Creating cron schedule %q on service %q...", title, service.Name)
		created, err := e.target.CreateAppServiceCronSchedule(ctx, service.ID, TargetCreateAppServiceCronScheduleInput{
			Name:     &name,
			Title:    title,
			Crontab:  cron.Crontab,
			Command:  cron.Command,
			Disabled: &desiredDisabled,
		})
		if err != nil {
			return e.recordInstanceMutationError(state, sourceID, operation, "cron schedule creation", err)
		}
		if err := e.completeInstanceMutation(state, sourceID, operation, created.ID, 0); err != nil {
			return err
		}
		e.reportProgress("Cron schedule %q on service %q created (ID %d).", title, service.Name, created.ID)
	}
	return nil
}

func (e *MigrationExecutor) disableDefaultPHPCronSchedules(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	inspection TargetStackServiceInspection,
	current []TargetAppServiceCronSchedule,
) error {
	defaultNames := defaultPHPCronScheduleNames(inspection)
	if len(defaultNames) == 0 {
		return nil
	}
	byName := map[string][]TargetAppServiceCronSchedule{}
	for _, item := range current {
		if defaultNames[item.Name] {
			byName[item.Name] = append(byName[item.Name], item)
		}
	}
	names := make([]string, 0, len(defaultNames))
	for name := range defaultNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		matches := byName[name]
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{
				Resource: "default app service cron schedule",
				Name:     name,
				Count:    len(matches),
			}
		}
		if len(matches) == 0 {
			continue
		}
		item := matches[0]
		operation := operationKey("cron_default_disable", strconv.Itoa(service.ID), item.Name)
		if item.Disabled {
			if err := e.recordObservedInstanceOperation(state, sourceID, operation, item.ID, 0); err != nil {
				return err
			}
			e.reportProgress("Default cron schedule %q on service %q is already disabled.", item.Title, service.Name)
			continue
		}
		run, err := e.beginInstanceMutation(state, sourceID, operation, false)
		if err != nil || !run {
			return err
		}
		e.reportProgress("Disabling default cron schedule %q on service %q...", item.Title, service.Name)
		disabled := true
		updated, err := e.target.UpdateAppServiceCronSchedule(
			ctx,
			item.ID,
			TargetUpdateAppServiceCronScheduleInput{Disabled: &disabled},
		)
		if err != nil {
			return e.recordInstanceMutationError(state, sourceID, operation, "default cron schedule update", err)
		}
		if !updated.Disabled || updated.AppServiceID != service.ID || updated.Name != item.Name {
			return errors.Errorf("updated default cron schedule %q does not match the requested disabled state", item.Name)
		}
		if err := e.completeInstanceMutation(state, sourceID, operation, updated.ID, 0); err != nil {
			return err
		}
		e.reportProgress("Default cron schedule %q on service %q disabled.", item.Title, service.Name)
	}
	return nil
}

func defaultPHPCronScheduleNames(inspection TargetStackServiceInspection) map[string]bool {
	manifest := inspection.ServiceRevision.Manifest
	if manifest == nil {
		return nil
	}
	serviceName := strings.ToLower(strings.TrimSpace(manifest.Name))
	if serviceName == "" {
		serviceName = strings.ToLower(strings.TrimSpace(inspection.ServiceRevision.Name))
	}
	if !strings.HasSuffix(serviceName, "-php") ||
		(!strings.HasPrefix(serviceName, "drupal") && !strings.HasPrefix(serviceName, "wordpress")) {
		return nil
	}
	result := map[string]bool{}
	for _, schedule := range manifest.CronSchedules {
		name := strings.TrimSpace(schedule.Name)
		if name != "" {
			result[name] = true
		}
	}
	return result
}

func operationSucceeded(resource *MigrationResourceState, operation string) bool {
	if resource == nil {
		return false
	}
	item, ok := resource.Operations[operation]
	return ok && item.Status == MigrationOperationSucceeded
}

func (e *MigrationExecutor) beginInstanceMutation(
	state *MigrationState,
	sourceID string,
	operation string,
	verifiedRecovered bool,
) (bool, error) {
	resource := state.Instances[sourceID]
	if resource == nil {
		return false, errors.New("migration instance state is missing")
	}
	if current, ok := resource.Operations[operation]; ok {
		switch current.Status {
		case MigrationOperationSucceeded:
			return false, nil
		case MigrationOperationIntent, MigrationOperationAmbiguous:
			if verifiedRecovered {
				if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
					return false, err
				}
				if err := state.MarkInstanceOperationSuccessWithIDs(
					sourceID,
					operation,
					current.TargetID,
					current.TaskID,
				); err != nil {
					return false, err
				}
				return false, SaveMigrationState(e.statePath, state)
			}
			retryOperation := instanceAmbiguousRetryOperation(sourceID, operation)
			if !e.ambiguousRetryAuthorized(retryOperation) {
				return false, ambiguousRetryRequiredError(
					fmt.Sprintf("migration operation %q has an ambiguous result", operation),
					retryOperation,
				)
			}
		case MigrationOperationAccepted:
			return false, errors.Errorf(
				"migration operation %q was accepted by the target and must be recovered by its target or task ID",
				operation,
			)
		case MigrationOperationFailed:
			// A structured API rejection proves the previous request did not
			// mutate the target, so a corrected retry is safe.
		default:
			return false, errors.New("migration operation has an unsupported state")
		}
	}
	if err := state.MarkInstanceOperationIntent(sourceID, operation); err != nil {
		return false, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return false, err
	}
	return true, nil
}

func (e *MigrationExecutor) recordObservedInstanceOperation(
	state *MigrationState,
	sourceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	resource := state.Instances[sourceID]
	if resource == nil {
		return errors.New("migration instance state is missing")
	}
	if operationSucceeded(resource, operation) {
		return nil
	}
	if current, ok := resource.Operations[operation]; ok &&
		(current.Status == MigrationOperationIntent || current.Status == MigrationOperationAmbiguous) {
		if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
			return err
		}
	} else {
		if err := state.MarkInstanceOperationIntent(sourceID, operation); err != nil {
			return err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return err
		}
	}
	if err := state.MarkInstanceOperationSuccessWithIDs(sourceID, operation, targetID, taskID); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func (e *MigrationExecutor) completeInstanceMutation(
	state *MigrationState,
	sourceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := state.MarkInstanceOperationSuccessWithIDs(sourceID, operation, targetID, taskID); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func (e *MigrationExecutor) acceptInstanceMutation(
	state *MigrationState,
	sourceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := state.MarkInstanceOperationAcceptedWithIDs(
		sourceID,
		operation,
		targetID,
		taskID,
	); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func (e *MigrationExecutor) acceptRecoveredInstanceMutation(
	state *MigrationState,
	sourceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	resource := state.Instances[sourceID]
	if resource == nil {
		return errors.New("migration instance state is missing")
	}
	current, ok := resource.Operations[operation]
	if !ok {
		return errors.New("migration operation intent is missing")
	}
	if current.Status != MigrationOperationIntent &&
		current.Status != MigrationOperationAccepted {
		if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
			return err
		}
	}
	return e.acceptInstanceMutation(state, sourceID, operation, targetID, taskID)
}

func (e *MigrationExecutor) completeAcceptedInstanceMutation(
	state *MigrationState,
	sourceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
		return err
	}
	return e.completeInstanceMutation(state, sourceID, operation, targetID, taskID)
}

func (e *MigrationExecutor) recordAcceptedOperationWaitError(
	state *MigrationState,
	sourceID string,
	operation string,
	waitErr error,
) error {
	var terminal *TargetTerminalOperationError
	if !errors.As(waitErr, &terminal) {
		return waitErr
	}
	if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
		return err
	}
	if err := state.MarkInstanceOperationFailure(sourceID, operation, "target_terminal"); err != nil {
		return err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return err
	}
	return waitErr
}

func (e *MigrationExecutor) recordInstanceMutationError(
	state *MigrationState,
	sourceID string,
	operation string,
	label string,
	mutationErr error,
) error {
	return e.recordInstanceMutationErrorWithResourceStatus(
		state,
		sourceID,
		operation,
		label,
		mutationErr,
		false,
	)
}

func (e *MigrationExecutor) recordInstanceCreationMutationError(
	state *MigrationState,
	sourceID string,
	operation string,
	label string,
	mutationErr error,
) error {
	return e.recordInstanceMutationErrorWithResourceStatus(
		state,
		sourceID,
		operation,
		label,
		mutationErr,
		true,
	)
}

func (e *MigrationExecutor) recordInstanceMutationErrorWithResourceStatus(
	state *MigrationState,
	sourceID string,
	operation string,
	label string,
	mutationErr error,
	affectResource bool,
) error {
	var apiErr *rest.APIError
	if errors.As(mutationErr, &apiErr) &&
		targetRejectionIsDefinitive(apiErr.StatusCode) {
		if err := state.MarkInstanceOperationFailure(sourceID, operation, "api_rejected"); err != nil {
			return err
		}
		if affectResource {
			_ = state.SetInstanceTarget(
				sourceID,
				state.Instances[sourceID].TargetID,
				MigrationResourceFailed,
			)
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return err
		}
		return targetRejectionError(label, apiErr)
	}
	if err := state.MarkInstanceOperationAmbiguous(sourceID, operation); err != nil {
		return err
	}
	if affectResource {
		_ = state.SetInstanceTarget(
			sourceID,
			state.Instances[sourceID].TargetID,
			MigrationResourceAmbiguous,
		)
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return err
	}
	return ambiguousRetryRequiredError(
		fmt.Sprintf("result of %s is ambiguous", label),
		instanceAmbiguousRetryOperation(sourceID, operation),
	)
}

func (e *MigrationExecutor) recordPairMutationError(
	state *MigrationState,
	sourceID string,
	operation string,
	label string,
	mutationErr error,
) error {
	var apiErr *rest.APIError
	if errors.As(mutationErr, &apiErr) &&
		targetRejectionIsDefinitive(apiErr.StatusCode) {
		if err := state.MarkAppOperationFailure(operation, "api_rejected"); err != nil {
			return err
		}
		if err := state.MarkInstanceOperationFailure(sourceID, operation, "api_rejected"); err != nil {
			return err
		}
		_ = state.SetAppTarget(0, MigrationResourceFailed)
		_ = state.SetInstanceTarget(sourceID, 0, MigrationResourceFailed)
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return err
		}
		return targetRejectionError(label, apiErr)
	}
	if err := state.MarkAppOperationAmbiguous(operation); err != nil {
		return err
	}
	if err := state.MarkInstanceOperationAmbiguous(sourceID, operation); err != nil {
		return err
	}
	_ = state.SetAppTarget(0, MigrationResourceAmbiguous)
	_ = state.SetInstanceTarget(sourceID, 0, MigrationResourceAmbiguous)
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return err
	}
	return ambiguousRetryRequiredError(
		fmt.Sprintf("result of %s is ambiguous", label),
		appCreateAmbiguousRetryOperation(state.Source.ID),
	)
}

func targetRejectionError(label string, apiErr *rest.APIError) error {
	detail := safeTargetRejectionDetail(apiErr)
	if detail == "" {
		return errors.Errorf(
			"target rejected %s (HTTP %d); correct the configuration and resume",
			label,
			apiErr.StatusCode,
		)
	}
	return errors.Errorf(
		"target rejected %s (HTTP %d): %s; correct the configuration and resume",
		label,
		apiErr.StatusCode,
		detail,
	)
}

// safeTargetRejectionDetail exposes structured target validation messages but
// deliberately ignores raw response bodies. Raw bodies can contain upstream
// responses or signed URLs. URLs in structured messages are redacted for the
// same reason, and whitespace and length are bounded for terminal output.
func safeTargetRejectionDetail(apiErr *rest.APIError) string {
	if apiErr == nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(apiErr.Message))
	for index, field := range fields {
		candidate := strings.Trim(field, `"'()[]{}<>,;`)
		parsed, err := url.Parse(candidate)
		if err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			fields[index] = "[redacted URL]"
		}
	}
	detail := strings.Join(fields, " ")
	const maxDetailRunes = 500
	runes := []rune(detail)
	if len(runes) > maxDetailRunes {
		detail = string(runes[:maxDetailRunes]) + "…"
	}
	return detail
}

func targetRejectionIsDefinitive(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusNotAcceptable,
		http.StatusGone,
		http.StatusLengthRequired,
		http.StatusPreconditionFailed,
		http.StatusRequestEntityTooLarge,
		http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType,
		http.StatusRequestedRangeNotSatisfiable,
		http.StatusExpectationFailed,
		http.StatusMisdirectedRequest,
		http.StatusUnprocessableEntity,
		http.StatusLocked,
		http.StatusFailedDependency,
		http.StatusUpgradeRequired,
		http.StatusPreconditionRequired,
		http.StatusTooManyRequests,
		http.StatusRequestHeaderFieldsTooLarge,
		http.StatusUnavailableForLegalReasons:
		return true
	default:
		// Request Timeout, Conflict, Too Early, non-standard proxy 4xx
		// responses, and all 5xx/transport failures may follow a mutation.
		return false
	}
}

func operationKey(prefix string, parts ...string) string {
	return prefix + "." + shortDigest(parts...)
}

func appCreateAmbiguousRetryOperation(sourceID string) string {
	return "app:" + sourceID + ":create"
}

func instanceAmbiguousRetryOperation(sourceID, operation string) string {
	return "instance:" + sourceID + ":" + operation
}

func (e *MigrationExecutor) ambiguousRetryAuthorized(operationID string) bool {
	return e.retryAmbiguousOperation != "" &&
		e.retryAmbiguousOperation == operationID
}

func ambiguousRetryRequiredError(message, operationID string) error {
	return errors.Errorf(
		"%s; inspect the target, then retry only this operation with --retry-ambiguous %q",
		message,
		operationID,
	)
}

func shortDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum[:8])
}

func (e *MigrationExecutor) ensureTechnicalDeployment(
	ctx context.Context,
	state *MigrationState,
	prepared PreparedInstance,
	instance TargetAppInstance,
	operationPrefix string,
) error {
	services, err := e.target.ListAppServices(ctx, instance.ID)
	if err != nil {
		return errors.Wrap(err, "list target services before deployment")
	}
	var build *TargetAppBuild
	if prepared.BuildSource != nil {
		service, err := exactAppService(services, prepared.BuildSource.ServiceName)
		if err != nil {
			return err
		}
		build, err = e.ensureBuild(
			ctx,
			state,
			prepared.Source.UUID,
			instance.ID,
			service.ID,
			prepared.BuildSource.Input,
		)
		if err != nil {
			return err
		}
	}

	input, err := technicalDeploymentInput(prepared, services, build)
	if err != nil {
		return err
	}
	if len(input.Services) == 0 {
		return nil
	}
	return e.ensureDeployment(
		ctx,
		state,
		prepared.Source.UUID,
		instance.ID,
		operationKey(operationPrefix, prepared.Source.UUID),
		input,
	)
}

func technicalDeploymentInput(
	prepared PreparedInstance,
	services []TargetAppService,
	build *TargetAppBuild,
) (TargetCreateAppDeploymentInput, error) {
	input := TargetCreateAppDeploymentInput{Services: []TargetAppServiceDeploymentInput{}}
	skipPostDeployment := !sourceBoolProperty(prepared.Source.Properties, "post_deploy", true)
	skippedCodeServices := skippedCodeServiceNames(prepared)
	serviceBuildID := 0
	if build != nil {
		var err error
		serviceBuildID, err = completedServiceBuildID(*build, build.AppServiceID)
		if err != nil {
			return TargetCreateAppDeploymentInput{}, err
		}
	}
	for _, service := range services {
		if service.Disabled {
			continue
		}
		if _, skipped := skippedCodeServices[service.Name]; skipped {
			continue
		}
		item := TargetAppServiceDeploymentInput{
			AppServiceID: service.ID,
			Force:        true,
		}
		usesBuild := build != nil && (build.AppServiceID == service.ID ||
			(service.ParentAppServiceID != nil && *service.ParentAppServiceID == build.AppServiceID))
		if usesBuild {
			item.AppServiceBuildID = &serviceBuildID
			if build.AppServiceID == service.ID && skipPostDeployment {
				item.SkipPostDeployment = &skipPostDeployment
			}
		}
		input.Services = append(input.Services, item)
	}
	if len(input.Services) == 0 {
		if prepared.SkipCode {
			return input, nil
		}
		return TargetCreateAppDeploymentInput{}, errors.New("target instance has no enabled services to deploy")
	}
	sort.Slice(input.Services, func(i, j int) bool {
		return input.Services[i].AppServiceID < input.Services[j].AppServiceID
	})
	return input, nil
}

func skippedCodeServiceNames(prepared PreparedInstance) map[string]struct{} {
	result := map[string]struct{}{}
	if !prepared.SkipCode {
		return result
	}
	for _, inspection := range prepared.StackServices {
		manifest := inspection.ServiceRevision.Manifest
		if manifest == nil || manifest.Build == nil {
			continue
		}
		result[inspection.StackService.Name] = struct{}{}
	}
	return result
}

func validateManuallyDeployedCodeServices(
	prepared PreparedInstance,
	services []TargetAppService,
) error {
	skipped := skippedCodeServiceNames(prepared)
	if len(skipped) == 0 {
		return nil
	}
	byName, err := indexAppServices(services)
	if err != nil {
		return err
	}
	for name := range skipped {
		service, ok := byName[name]
		if !ok {
			return errors.Errorf("manually deployed target code service %q is missing", name)
		}
		expectedEnabled, ok := prepared.EffectiveState[name]
		if !ok {
			return errors.Errorf("approved enabled state for target code service %q is missing", name)
		}
		if service.Disabled == expectedEnabled {
			return errors.Errorf(
				"target code service %q enabled state no longer matches the approved source state",
				name,
			)
		}
		if !expectedEnabled {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(service.Status), "OK") ||
			service.NeedsRebuild || service.NeedsRedeploy {
			return errors.Errorf(
				"target code service %q was skipped by --skip-code and is not manually deployed and healthy",
				name,
			)
		}
	}
	return nil
}

func (e *MigrationExecutor) ensureBuild(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	instanceID int,
	serviceID int,
	source TargetBuildSourceInput,
) (*TargetAppBuild, error) {
	operation := operationKey("build", strconv.Itoa(serviceID))
	resource := state.Instances[sourceID]
	if resource == nil {
		return nil, errors.New("migration instance state is missing")
	}
	if current, ok := resource.Operations[operation]; ok {
		switch current.Status {
		case MigrationOperationSucceeded:
			if current.TargetID <= 0 {
				return nil, errors.New("completed build operation is missing its target ID")
			}
			e.reportProgress("Reusing completed target build ID %d from the saved migration state.", current.TargetID)
			build, err := e.waitBuild(ctx, current.TargetID)
			if err != nil {
				return nil, err
			}
			if !buildMatchesApprovedSource(build, instanceID, serviceID, source) {
				return nil, errors.New("recorded target build does not match the approved service and Git ref")
			}
			return &build, nil
		case MigrationOperationIntent, MigrationOperationAccepted, MigrationOperationAmbiguous:
			build, found, err := e.recoverBuild(ctx, instanceID, serviceID, source, current)
			if err != nil {
				return nil, err
			}
			if found {
				taskID := 0
				if build.TaskID != nil {
					taskID = *build.TaskID
				}
				if current.TaskID > 0 {
					taskID = current.TaskID
				}
				if err := e.acceptRecoveredInstanceMutation(
					state,
					sourceID,
					operation,
					build.ID,
					taskID,
				); err != nil {
					return nil, err
				}
				e.reportProgress("Recovered target build ID %d%s. Waiting for completion...", build.ID, taskSuffix(taskID))
				completed, err := e.waitBuild(ctx, build.ID)
				if err != nil {
					return nil, e.recordAcceptedOperationWaitError(
						state,
						sourceID,
						operation,
						err,
					)
				}
				if !buildMatchesApprovedSource(completed, instanceID, serviceID, source) {
					return nil, errors.New("recovered target build does not match the approved service and Git ref")
				}
				if err := e.completeAcceptedInstanceMutation(
					state,
					sourceID,
					operation,
					completed.ID,
					taskID,
				); err != nil {
					return nil, err
				}
				e.reportProgress("Target build ID %d completed.", completed.ID)
				return &completed, nil
			}
			if current.Status == MigrationOperationAccepted {
				return nil, errors.New("accepted target build no longer matches its recorded migration operation")
			}
			retryOperation := instanceAmbiguousRetryOperation(sourceID, operation)
			if !e.ambiguousRetryAuthorized(retryOperation) {
				return nil, ambiguousRetryRequiredError(
					"target build result is ambiguous and no timestamp-bounded match was found",
					retryOperation,
				)
			}
		}
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return nil, err
	}
	e.reportProgress("Launching target build for app service ID %d...", serviceID)
	response, err := e.target.CreateAppBuild(ctx, []int{serviceID})
	if err != nil {
		return nil, e.recordInstanceMutationError(state, sourceID, operation, "application build creation", err)
	}
	if len(response.Items) != 1 {
		_ = state.MarkInstanceOperationAmbiguous(sourceID, operation)
		_ = SaveMigrationState(e.statePath, state)
		return nil, ambiguousRetryRequiredError(
			"target build response was ambiguous",
			instanceAmbiguousRetryOperation(sourceID, operation),
		)
	}
	build := response.Items[0]
	taskID := 0
	if response.TaskID != nil {
		taskID = *response.TaskID
	} else if build.TaskID != nil {
		taskID = *build.TaskID
	}
	if err := e.acceptInstanceMutation(state, sourceID, operation, build.ID, taskID); err != nil {
		return nil, err
	}
	e.reportProgress("Target build launched (build ID %d%s). Waiting for completion...", build.ID, taskSuffix(taskID))
	completed, err := e.waitBuild(ctx, build.ID)
	if err != nil {
		return nil, e.recordAcceptedOperationWaitError(state, sourceID, operation, err)
	}
	if !buildMatchesApprovedSource(completed, instanceID, serviceID, source) {
		return nil, errors.New("created target build does not match the approved service and Git ref")
	}
	if err := e.completeAcceptedInstanceMutation(
		state,
		sourceID,
		operation,
		completed.ID,
		taskID,
	); err != nil {
		return nil, err
	}
	e.reportProgress("Target build ID %d completed.", completed.ID)
	return &completed, nil
}

func taskSuffix(taskID int) string {
	if taskID <= 0 {
		return ""
	}
	return fmt.Sprintf(", task ID %d", taskID)
}

func (e *MigrationExecutor) recoverBuild(
	ctx context.Context,
	instanceID int,
	serviceID int,
	source TargetBuildSourceInput,
	operation MigrationOperationState,
) (TargetAppBuild, bool, error) {
	if operation.TargetID > 0 {
		item, err := e.target.GetAppBuild(ctx, operation.TargetID)
		if err != nil {
			return TargetAppBuild{}, false, err
		}
		if buildMatchesApprovedSource(item, instanceID, serviceID, source) &&
			(operation.Status == MigrationOperationAccepted ||
				createdWithinOperation(item.CreatedAt, operation)) {
			return item, true, nil
		}
		return TargetAppBuild{}, false, nil
	}
	response, err := e.target.ListAppBuilds(ctx, instanceID, TargetPageOptions{PageSize: 100})
	if err != nil {
		return TargetAppBuild{}, false, err
	}
	matches := []TargetAppBuild{}
	for _, item := range response.Items {
		if buildMatchesApprovedSource(item, instanceID, serviceID, source) &&
			createdWithinOperation(item.CreatedAt, operation) {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return TargetAppBuild{}, false, errors.New("multiple timestamp-bounded target builds match the ambiguous operation")
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return TargetAppBuild{}, false, nil
}

func buildMatchesApprovedSource(
	build TargetAppBuild,
	instanceID int,
	serviceID int,
	source TargetBuildSourceInput,
) bool {
	if build.AppInstanceID != instanceID || build.AppServiceID != serviceID {
		return false
	}
	if source.GitRef != nil && build.GitRef != *source.GitRef {
		return false
	}
	if source.GitRefType != nil &&
		!strings.EqualFold(strings.TrimSpace(build.GitRefType), strings.TrimSpace(*source.GitRefType)) {
		return false
	}
	return true
}

func completedServiceBuildID(build TargetAppBuild, serviceID int) (int, error) {
	matches := []TargetAppServiceBuild{}
	for _, item := range build.AppServiceBuilds {
		if item.AppServiceID == serviceID && strings.EqualFold(item.Status, "COMPLETED") {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return 0, errors.Errorf("completed target build contains %d service builds for service ID %d", len(matches), serviceID)
	}
	return matches[0].ID, nil
}

func (e *MigrationExecutor) ensureDeployment(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	instanceID int,
	operation string,
	input TargetCreateAppDeploymentInput,
) error {
	resource := state.Instances[sourceID]
	if resource == nil {
		return errors.New("migration instance state is missing")
	}
	if current, ok := resource.Operations[operation]; ok {
		switch current.Status {
		case MigrationOperationSucceeded:
			if current.TargetID <= 0 {
				return errors.New("completed deployment operation is missing its target ID")
			}
			e.reportProgress("Reusing completed target deployment ID %d from the saved migration state.", current.TargetID)
			completed, err := e.waitDeployment(ctx, current.TargetID)
			if err != nil {
				return err
			}
			if !deploymentMatchesInput(completed, input) {
				return errors.New("recorded target deployment does not match the approved service inputs")
			}
			return nil
		case MigrationOperationIntent, MigrationOperationAccepted, MigrationOperationAmbiguous:
			deployment, found, err := e.recoverDeployment(ctx, instanceID, input, current)
			if err != nil {
				return err
			}
			if found {
				taskID := 0
				if deployment.TaskID != nil {
					taskID = *deployment.TaskID
				}
				if current.TaskID > 0 {
					taskID = current.TaskID
				}
				if err := e.acceptRecoveredInstanceMutation(
					state,
					sourceID,
					operation,
					deployment.ID,
					taskID,
				); err != nil {
					return err
				}
				e.reportProgress("Recovered target deployment ID %d%s. Waiting for completion...", deployment.ID, taskSuffix(taskID))
				completed, err := e.waitDeployment(ctx, deployment.ID)
				if err != nil {
					return e.recordAcceptedOperationWaitError(
						state,
						sourceID,
						operation,
						err,
					)
				}
				if !deploymentMatchesInput(completed, input) {
					return errors.New("recovered target deployment does not match the approved service inputs")
				}
				if err := e.completeAcceptedInstanceMutation(
					state,
					sourceID,
					operation,
					completed.ID,
					taskID,
				); err != nil {
					return err
				}
				e.reportProgress("Target deployment ID %d completed.", completed.ID)
				return nil
			}
			if current.Status == MigrationOperationAccepted {
				return errors.New("accepted target deployment no longer matches its recorded migration operation")
			}
			retryOperation := instanceAmbiguousRetryOperation(sourceID, operation)
			if !e.ambiguousRetryAuthorized(retryOperation) {
				return ambiguousRetryRequiredError(
					"target deployment result is ambiguous and no timestamp-bounded match was found",
					retryOperation,
				)
			}
		}
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Launching target deployment for app instance ID %d...", instanceID)
	deployment, err := e.target.CreateAppDeployment(ctx, input)
	if err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "application deployment creation", err)
	}
	taskID := 0
	if deployment.TaskID != nil {
		taskID = *deployment.TaskID
	}
	if err := e.acceptInstanceMutation(state, sourceID, operation, deployment.ID, taskID); err != nil {
		return err
	}
	e.reportProgress("Target deployment launched (deployment ID %d%s). Waiting for completion...", deployment.ID, taskSuffix(taskID))
	completed, err := e.waitDeployment(ctx, deployment.ID)
	if err != nil {
		return e.recordAcceptedOperationWaitError(state, sourceID, operation, err)
	}
	if !deploymentMatchesInput(completed, input) {
		return errors.New("created target deployment does not match the approved service inputs")
	}
	if err := e.completeAcceptedInstanceMutation(
		state,
		sourceID,
		operation,
		completed.ID,
		taskID,
	); err != nil {
		return err
	}
	e.reportProgress("Target deployment ID %d completed.", completed.ID)
	return nil
}

func (e *MigrationExecutor) recoverDeployment(
	ctx context.Context,
	instanceID int,
	input TargetCreateAppDeploymentInput,
	operation MigrationOperationState,
) (TargetAppDeployment, bool, error) {
	if operation.TargetID > 0 {
		item, err := e.target.GetAppDeployment(ctx, operation.TargetID)
		if err != nil {
			return TargetAppDeployment{}, false, err
		}
		if item.AppInstanceID == instanceID &&
			deploymentMatchesInput(item, input) &&
			(operation.Status == MigrationOperationAccepted ||
				createdWithinOperation(item.CreatedAt, operation)) {
			return item, true, nil
		}
		return TargetAppDeployment{}, false, nil
	}
	response, err := e.target.ListAppDeployments(ctx, instanceID, TargetPageOptions{PageSize: 100})
	if err != nil {
		return TargetAppDeployment{}, false, err
	}
	matches := []TargetAppDeployment{}
	for _, item := range response.Items {
		if deploymentMatchesInput(item, input) && createdWithinOperation(item.CreatedAt, operation) {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return TargetAppDeployment{}, false, errors.New("multiple timestamp-bounded target deployments match the ambiguous operation")
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return TargetAppDeployment{}, false, nil
}

func deploymentMatchesInput(item TargetAppDeployment, input TargetCreateAppDeploymentInput) bool {
	if len(item.AppServiceDeployments) != len(input.Services) {
		return false
	}
	expected := map[int]*int{}
	expectedSkip := map[int]bool{}
	for _, service := range input.Services {
		expected[service.AppServiceID] = service.AppServiceBuildID
		expectedSkip[service.AppServiceID] =
			service.SkipPostDeployment != nil && *service.SkipPostDeployment
	}
	for _, service := range item.AppServiceDeployments {
		buildID, ok := expected[service.AppServiceID]
		if !ok ||
			!targetEqualOptionalID(buildID, service.AppServiceBuildID) ||
			expectedSkip[service.AppServiceID] != service.SkipPostDeployment {
			return false
		}
		for _, wanted := range input.Services {
			if wanted.AppServiceID != service.AppServiceID {
				continue
			}
			skipPost := wanted.SkipPostDeployment != nil && *wanted.SkipPostDeployment
			if service.SkipPostDeployment != skipPost || service.Force != wanted.Force {
				return false
			}
			break
		}
	}
	return true
}

func sourceBoolProperty(properties map[string]interface{}, name string, defaultValue bool) bool {
	value, found := properties[name]
	if !found || value == nil {
		return defaultValue
	}
	enabled, ok := value.(bool)
	if !ok {
		return defaultValue
	}
	return enabled
}

func (e *MigrationExecutor) ensureCustomRoutes(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedInstance,
	instance TargetAppInstance,
) error {
	instancePlan := planInstance(plan, prepared.Source.UUID)
	if instancePlan == nil {
		return errors.New("migration plan is missing route inventory for an instance")
	}
	services, err := e.target.ListAppServices(ctx, instance.ID)
	if err != nil {
		return errors.Wrap(err, "list target services before staging custom routes")
	}
	serviceByName, err := indexAppServices(services)
	if err != nil {
		return err
	}
	ports, err := e.target.ListAppPorts(ctx, instance.ID)
	if err != nil {
		return errors.Wrap(err, "list target ports before staging custom routes")
	}
	for _, routePlan := range instancePlan.Routes {
		if routePlan.Action != "create_backend" && routePlan.Action != "create_redirect" {
			continue
		}
		mapping, ok := prepared.Services[routePlan.Service]
		if !ok {
			return errors.Errorf("route service %q has no approved target mapping", routePlan.Service)
		}
		service, ok := serviceByName[mapping.Target.StackService.Name]
		if !ok {
			return errors.New("mapped target route service disappeared after preflight")
		}
		if routePlan.PortNumber == nil {
			return errors.New("approved route is missing its target port number")
		}
		port, err := exactAppPort(ports, service.ID, *routePlan.PortNumber)
		if err != nil {
			return err
		}
		route, err := e.ensureRoute(
			ctx,
			state,
			prepared.Source.UUID,
			instance.ID,
			service,
			port,
			routePlan,
			prepared.DisableCustomRoutes,
		)
		if err != nil {
			return err
		}
		for _, setting := range routePlan.Settings {
			if err := e.ensureRouteSetting(
				ctx,
				state,
				prepared.Source.UUID,
				route.ID,
				setting,
			); err != nil {
				return err
			}
		}
		if routePlan.BasicAuth {
			if prepared.Source.BasicAuth == nil || !prepared.Source.BasicAuth.Enabled ||
				prepared.Source.BasicAuth.IsPasswordRedacted() ||
				prepared.Source.BasicAuth.Password == "" {
				return errors.New("source basic-auth secret is unavailable at apply time")
			}
			if err := e.ensureRouteAuth(
				ctx,
				state,
				prepared.Source.UUID,
				instance.ID,
				service.ID,
				route.ID,
				*prepared.Source.BasicAuth,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func exactAppPort(items []TargetAppPort, serviceID, number int) (TargetAppPort, error) {
	matches := []TargetAppPort{}
	for _, item := range items {
		if item.AppServiceID == serviceID && item.Number == number {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetAppPort{}, errors.Errorf("target service ID %d has no app port %d", serviceID, number)
	case 1:
		return matches[0], nil
	default:
		return TargetAppPort{}, &TargetAmbiguousMatchError{
			Resource: "app port",
			Name:     strconv.Itoa(number),
			Count:    len(matches),
		}
	}
}

func (e *MigrationExecutor) ensureRoute(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	instanceID int,
	service TargetAppService,
	port TargetAppPort,
	plan RoutePlan,
	disabled bool,
) (TargetAppRoute, error) {
	operation := operationKey(
		"route",
		strconv.Itoa(service.ID),
		strconv.Itoa(port.ID),
		plan.Host,
		plan.Action,
	)
	routes, err := e.target.ListAppRoutes(ctx, instanceID)
	if err != nil {
		return TargetAppRoute{}, errors.Wrap(err, "list target routes")
	}
	matches := matchingRoutes(routes, service.ID, port.ID, plan, disabled)
	if len(matches) > 1 {
		return TargetAppRoute{}, &TargetAmbiguousMatchError{Resource: "app route", Name: plan.Host, Count: len(matches)}
	}
	resource := state.Instances[sourceID]
	current, hasOperation := resource.Operations[operation]
	if len(matches) == 1 {
		route := matches[0]
		if !hasOperation {
			return TargetAppRoute{}, errors.New("target app already contains the planned custom route without a migration operation record")
		}
		switch current.Status {
		case MigrationOperationSucceeded:
			if current.TargetID != 0 && current.TargetID != route.ID {
				return TargetAppRoute{}, errors.New("recorded target route ID no longer matches")
			}
			e.reportProgress("Custom route %q already exists (ID %d).", plan.Host, route.ID)
			return route, nil
		case MigrationOperationIntent, MigrationOperationAmbiguous:
			if !createdWithinOperation(route.CreatedAt, current) {
				return TargetAppRoute{}, errors.New("existing target route predates the recorded create operation and cannot be adopted")
			}
			if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
				return TargetAppRoute{}, err
			}
			if err := e.completeInstanceMutation(state, sourceID, operation, route.ID, 0); err != nil {
				return TargetAppRoute{}, err
			}
			e.reportProgress("Recovered custom route %q (ID %d) from the saved migration operation.", plan.Host, route.ID)
			return route, nil
		case MigrationOperationFailed:
			return TargetAppRoute{}, errors.New("a target route exists after a definitively failed create operation; inspect the target before resuming")
		}
	}
	if hasOperation && current.Status == MigrationOperationSucceeded {
		return TargetAppRoute{}, errors.New("recorded target custom route no longer exists")
	}
	retryOperation := instanceAmbiguousRetryOperation(sourceID, operation)
	if hasOperation &&
		(current.Status == MigrationOperationIntent || current.Status == MigrationOperationAmbiguous) &&
		!e.ambiguousRetryAuthorized(retryOperation) {
		return TargetAppRoute{}, ambiguousRetryRequiredError(
			"target route create result is ambiguous and no timestamp-bounded match was found",
			retryOperation,
		)
	}

	path, pathType := "/", TargetRoutePathPrefix
	action := TargetRouteActionBackend
	input := TargetCreateAppRouteInput{
		AppServiceID: service.ID,
		Main:         plan.Primary && !disabled,
		Primary:      plan.Primary && !disabled,
		Port:         port.ID,
		Host:         plan.Host,
		Path:         &path,
		PathType:     &pathType,
		Action:       &action,
	}
	if disabled {
		input.Disabled = &disabled
	}
	if plan.SSL {
		enabled := true
		input.LetsEncrypt = &enabled
	}
	if plan.Action == "create_redirect" {
		action = TargetRouteActionRedirect
		input.Action = &action
		scheme, host, redirectPath, err := routeRedirectTarget(plan)
		if err != nil {
			return TargetAppRoute{}, err
		}
		status := 301
		input.RedirectScheme = &scheme
		input.RedirectHost = &host
		input.RedirectPath = &redirectPath
		input.RedirectStatusCode = &status
	}
	if err := validateTargetCreateRouteInput(input); err != nil {
		return TargetAppRoute{}, err
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return TargetAppRoute{}, err
	}
	if disabled {
		e.reportProgress("Creating custom route %q disabled because the target subscription does not include custom domains...", plan.Host)
	} else {
		e.reportProgress("Creating custom route %q for service %q...", plan.Host, service.Name)
	}
	created, err := e.target.CreateAppRoute(ctx, input)
	if err != nil {
		return TargetAppRoute{}, e.recordInstanceMutationError(state, sourceID, operation, "custom route creation", err)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, created.ID, 0); err != nil {
		return TargetAppRoute{}, err
	}
	if disabled {
		e.reportProgress("Custom route %q created disabled (ID %d); upgrade the target plan and enable it before DNS cutover.", plan.Host, created.ID)
	} else {
		e.reportProgress("Custom route %q created (ID %d).", plan.Host, created.ID)
	}
	return created, nil
}

func matchingRoutes(
	items []TargetAppRoute,
	serviceID, portID int,
	plan RoutePlan,
	disabled bool,
) []TargetAppRoute {
	expectedAction := TargetRouteActionBackend
	if plan.Action == "create_redirect" {
		expectedAction = TargetRouteActionRedirect
	}
	result := []TargetAppRoute{}
	for _, item := range items {
		path := item.Path
		if path == "" {
			path = "/"
		}
		expectedPrimary := plan.Primary && !disabled
		if item.Host == plan.Host && item.AppServiceID == serviceID &&
			item.PortID == portID && path == "/" && item.Action == expectedAction &&
			item.Disabled == disabled && item.Main == expectedPrimary && item.Primary == expectedPrimary {
			if plan.Action == "create_redirect" {
				scheme, host, redirectPath, err := routeRedirectTarget(plan)
				if err != nil ||
					item.RedirectScheme == nil || *item.RedirectScheme != scheme ||
					item.RedirectHost == nil || *item.RedirectHost != host ||
					item.RedirectPath == nil || *item.RedirectPath != redirectPath ||
					item.RedirectStatusCode == nil || *item.RedirectStatusCode != 301 {
					continue
				}
			}
			result = append(result, item)
		}
	}
	return result
}

func routeRedirectTarget(plan RoutePlan) (string, string, string, error) {
	scheme := "https"
	if !plan.SSL {
		scheme = "http"
	}
	host := strings.TrimSpace(plan.RedirectTarget)
	path := "/"
	if host != "" && strings.Contains(host, "://") {
		parsed, err := url.Parse(host)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil {
			return "", "", "", errors.New("source redirect target is invalid")
		}
		scheme = parsed.Scheme
		host = parsed.Host
		if parsed.EscapedPath() != "" {
			path = parsed.EscapedPath()
		}
	} else if host == "" && plan.RedirectToWWW {
		host = "www." + strings.TrimPrefix(plan.Host, "www.")
	} else if host == "" && plan.RedirectNonWWW {
		host = strings.TrimPrefix(plan.Host, "www.")
	}
	if host == "" {
		return "", "", "", errors.New("source redirect route has no target hostname")
	}
	return scheme, host, path, nil
}

func (e *MigrationExecutor) ensureRouteSetting(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	routeID int,
	setting RouteSettingPlan,
) error {
	current, err := e.target.ListAppRouteSettings(ctx, routeID)
	if err != nil {
		return errors.Wrap(err, "list target route settings")
	}
	matches := []TargetAppRouteSetting{}
	for _, item := range current {
		if item.Name == setting.Name {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return &TargetAmbiguousMatchError{Resource: "app route setting", Name: setting.Name, Count: len(matches)}
	}
	operation := operationKey("route_setting", strconv.Itoa(routeID), setting.Name)
	if len(matches) == 1 && matches[0].Value == setting.Value {
		if err := e.recordObservedInstanceOperation(state, sourceID, operation, matches[0].ID, 0); err != nil {
			return err
		}
		e.reportProgress("Route setting %q on route ID %d already matches the source.", setting.Name, routeID)
		return nil
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Applying route setting %q on route ID %d...", setting.Name, routeID)
	updated, err := e.target.SetAppRouteSetting(ctx, routeID, setting.Name, setting.Value)
	if err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "route setting update", err)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, updated.ID, 0); err != nil {
		return err
	}
	e.reportProgress("Route setting %q on route ID %d applied.", setting.Name, routeID)
	return nil
}

func (e *MigrationExecutor) ensureRouteAuth(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	instanceID, serviceID, routeID int,
	auth BasicAuth,
) error {
	current, err := e.target.ListAppAuths(ctx, instanceID)
	if err != nil {
		return errors.Wrap(err, "list target route authentication entries")
	}
	realm := "Restricted"
	matches := []TargetAppAuth{}
	for _, item := range current {
		if item.AppServiceID != nil && *item.AppServiceID == serviceID &&
			item.AppRouteID != nil && *item.AppRouteID == routeID &&
			item.Login == auth.Login && item.Realm == realm {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return &TargetAmbiguousMatchError{Resource: "app authentication", Name: auth.Login, Count: len(matches)}
	}
	operation := operationKey("route_auth", strconv.Itoa(routeID), auth.Login)
	resource := state.Instances[sourceID]
	op, hasOperation := resource.Operations[operation]
	if len(matches) == 1 {
		item := matches[0]
		if !hasOperation {
			return errors.New("target route already contains the planned authentication entry without a migration operation record")
		}
		switch op.Status {
		case MigrationOperationSucceeded:
			if op.TargetID != 0 && op.TargetID != item.ID {
				return errors.New("recorded target authentication ID no longer matches")
			}
			e.reportProgress("Basic authentication on route ID %d is already migrated.", routeID)
			return nil
		case MigrationOperationIntent, MigrationOperationAmbiguous:
			if !createdWithinOperation(item.CreatedAt, op) {
				return errors.New("existing target authentication predates the recorded create operation and cannot be adopted")
			}
			if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
				return err
			}
			return e.completeInstanceMutation(state, sourceID, operation, item.ID, 0)
		}
	}
	if hasOperation && op.Status == MigrationOperationSucceeded {
		return errors.New("recorded target route authentication no longer exists")
	}
	retryOperation := instanceAmbiguousRetryOperation(sourceID, operation)
	if hasOperation &&
		(op.Status == MigrationOperationIntent || op.Status == MigrationOperationAmbiguous) &&
		!e.ambiguousRetryAuthorized(retryOperation) {
		return ambiguousRetryRequiredError(
			"target authentication create result is ambiguous and no timestamp-bounded match was found",
			retryOperation,
		)
	}
	run, err := e.beginInstanceMutation(state, sourceID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress("Creating basic authentication on route ID %d...", routeID)
	created, err := e.target.CreateAppAuth(ctx, TargetCreateAppAuthInput{
		AppInstanceID: instanceID,
		AppServiceID:  &serviceID,
		AppRouteID:    &routeID,
		Login:         auth.Login,
		Password:      auth.Password,
		Realm:         realm,
	})
	if err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "route authentication creation", err)
	}
	if err := e.completeInstanceMutation(state, sourceID, operation, created.ID, 0); err != nil {
		return err
	}
	e.reportProgress("Basic authentication on route ID %d created (ID %d).", routeID, created.ID)
	return nil
}

func importOperationKey(sourceID, component string, mirrored bool) string {
	prefix := "import"
	if mirrored {
		prefix = "import_mirror"
	}
	return operationKey(prefix, sourceID, strings.ToLower(strings.TrimSpace(component)))
}

func successfulImportOperation(
	resource *MigrationResourceState,
	sourceID, component string,
) (MigrationOperationState, bool) {
	if resource == nil {
		return MigrationOperationState{}, false
	}
	for _, mirrored := range []bool{false, true} {
		operation, ok := resource.Operations[importOperationKey(sourceID, component, mirrored)]
		if ok && operation.Status == MigrationOperationSucceeded {
			return operation, true
		}
	}
	return MigrationOperationState{}, false
}

func importOperationSucceeded(
	resource *MigrationResourceState,
	sourceID, component string,
) bool {
	_, ok := successfulImportOperation(resource, sourceID, component)
	return ok
}

func (e *MigrationExecutor) ensureImport(
	ctx context.Context,
	state *MigrationState,
	item PreparedDataImport,
	instanceID, serviceID int,
) error {
	resource := state.Instances[item.SourceInstanceUUID]
	if resource == nil {
		return errors.New("migration instance state is missing")
	}
	serverOperation := importOperationKey(item.SourceInstanceUUID, item.Backup.Component, false)
	if current, ok := resource.Operations[serverOperation]; ok && current.Status != MigrationOperationFailed {
		item.Backup.URL = strings.TrimSpace(item.Backup.URL)
		return e.ensureImportAttempt(
			ctx,
			state,
			item,
			instanceID,
			serviceID,
			serverOperation,
			"the Wodby 1 server",
		)
	}

	mirroredURL := strings.TrimSpace(item.Backup.MirroredURL)
	serverURL := strings.TrimSpace(item.Backup.URL)
	if mirroredURL != serverURL && validBackupTransferURL(mirroredURL) {
		mirrorOperation := importOperationKey(item.SourceInstanceUUID, item.Backup.Component, true)
		current, attempted := resource.Operations[mirrorOperation]
		if !attempted || current.Status != MigrationOperationFailed {
			mirrorItem := item
			mirrorItem.Backup.URL = mirroredURL
			err := e.ensureImportAttempt(
				ctx,
				state,
				mirrorItem,
				instanceID,
				serviceID,
				mirrorOperation,
				"the Wodby 1 backup mirror",
			)
			if err == nil {
				return nil
			}
			current = resource.Operations[mirrorOperation]
			if current.Status != MigrationOperationFailed {
				return err
			}
		}
		e.reportProgress(
			"The backup mirror import for component %q failed definitively; retrying from the Wodby 1 server...",
			item.Backup.Component,
		)
		if err := e.waitAppInstanceOK(ctx, instanceID, "retry the data import from the Wodby 1 server"); err != nil {
			return errors.Wrap(err, "wait for target instance before backup URL fallback")
		}
	}

	item.Backup.URL = serverURL
	return e.ensureImportAttempt(
		ctx,
		state,
		item,
		instanceID,
		serviceID,
		serverOperation,
		"the Wodby 1 server",
	)
}

func (e *MigrationExecutor) ensureImportAttempt(
	ctx context.Context,
	state *MigrationState,
	item PreparedDataImport,
	instanceID, serviceID int,
	operation, sourceDescription string,
) error {
	resource := state.Instances[item.SourceInstanceUUID]
	if resource == nil {
		return errors.New("migration instance state is missing")
	}
	if current, ok := resource.Operations[operation]; ok {
		switch current.Status {
		case MigrationOperationSucceeded:
			if current.TargetID <= 0 {
				return errors.New("completed data import operation is missing its target ID")
			}
			e.reportProgress("Reusing completed %q data import ID %d from the saved migration state.", item.Backup.Component, current.TargetID)
			imported, err := e.waitImport(ctx, current.TargetID)
			if err != nil {
				return err
			}
			return validateRecoveredImport(imported, instanceID, serviceID, item.Destination.ImportName)
		case MigrationOperationIntent, MigrationOperationAccepted, MigrationOperationAmbiguous:
			imported, found, err := e.recoverImport(
				ctx,
				instanceID,
				serviceID,
				item.Destination.ImportName,
				current,
			)
			if err != nil {
				return err
			}
			if found {
				e.reportProgress("Recovered %q data import ID %d%s.", item.Backup.Component, imported.ID, taskSuffix(current.TaskID))
				return e.finishAcceptedImport(
					ctx,
					state,
					item.SourceInstanceUUID,
					operation,
					imported,
					instanceID,
					serviceID,
					item.Destination.ImportName,
					current.TaskID,
				)
			}
			if current.Status == MigrationOperationAccepted {
				if current.TaskID <= 0 {
					return errors.New("accepted target data import no longer matches its recorded migration operation")
				}
				e.reportProgress("Waiting for target task ID %d for the %q data import...", current.TaskID, item.Backup.Component)
				if err := e.waitTask(ctx, current.TaskID); err != nil {
					return e.recordAcceptedOperationWaitError(
						state,
						item.SourceInstanceUUID,
						operation,
						err,
					)
				}
				imported, err = e.waitForImportRecord(
					ctx,
					instanceID,
					serviceID,
					item.Destination.ImportName,
					current.TaskID,
					current,
				)
				if err != nil {
					return errors.New("target accepted the data import but its result could not be identified; inspect the target before resuming")
				}
				return e.finishAcceptedImport(
					ctx,
					state,
					item.SourceInstanceUUID,
					operation,
					imported,
					instanceID,
					serviceID,
					item.Destination.ImportName,
					current.TaskID,
				)
			}
			retryOperation := instanceAmbiguousRetryOperation(item.SourceInstanceUUID, operation)
			if !e.ambiguousRetryAuthorized(retryOperation) {
				return ambiguousRetryRequiredError(
					"target data import result is ambiguous and no timestamp-bounded match was found",
					retryOperation,
				)
			}
		}
	}

	run, err := e.beginInstanceMutation(state, item.SourceInstanceUUID, operation, false)
	if err != nil || !run {
		return err
	}
	e.reportProgress(
		"Launching %q data import from %s into service ID %d (%s)...",
		item.Backup.Component,
		sourceDescription,
		serviceID,
		item.Destination.ImportName,
	)
	result, err := e.target.StartURLImport(ctx, TargetStartURLImportInput{
		AppServiceID: serviceID,
		ImportName:   item.Destination.ImportName,
		URL:          item.Backup.URL,
	})
	if err != nil {
		// Deliberately do not wrap the API error: a server or proxy may echo the
		// backup URL. recordInstanceMutationError returns metadata only.
		return e.recordInstanceMutationError(
			state,
			item.SourceInstanceUUID,
			operation,
			"data import start",
			err,
		)
	}
	taskID := 0
	if result.TaskID != nil {
		taskID = *result.TaskID
		if err := e.acceptInstanceMutation(
			state,
			item.SourceInstanceUUID,
			operation,
			0,
			taskID,
		); err != nil {
			return err
		}
		e.reportProgress("Data import launched. Waiting for target task ID %d...", taskID)
		if err := e.waitTask(ctx, taskID); err != nil {
			return e.recordAcceptedOperationWaitError(
				state,
				item.SourceInstanceUUID,
				operation,
				err,
			)
		}
	}
	intent := state.Instances[item.SourceInstanceUUID].Operations[operation]
	imported, err := e.waitForImportRecord(
		ctx,
		instanceID,
		serviceID,
		item.Destination.ImportName,
		taskID,
		intent,
	)
	if err != nil {
		if taskID == 0 {
			_ = state.MarkInstanceOperationAmbiguous(item.SourceInstanceUUID, operation)
			_ = SaveMigrationState(e.statePath, state)
			return ambiguousRetryRequiredError(
				"target accepted the data import but its result could not be identified",
				instanceAmbiguousRetryOperation(item.SourceInstanceUUID, operation),
			)
		}
		return errors.New("target accepted the data import but its result could not be identified; inspect the target before resuming")
	}
	return e.finishAcceptedImport(
		ctx,
		state,
		item.SourceInstanceUUID,
		operation,
		imported,
		instanceID,
		serviceID,
		item.Destination.ImportName,
		taskID,
	)
}

func (e *MigrationExecutor) finishAcceptedImport(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	operation string,
	imported TargetImport,
	instanceID int,
	serviceID int,
	importName string,
	taskID int,
) error {
	if err := validateRecoveredImport(imported, instanceID, serviceID, importName); err != nil {
		return err
	}
	if taskID == 0 && imported.TaskID != nil {
		taskID = *imported.TaskID
	}
	if err := e.acceptRecoveredInstanceMutation(
		state,
		sourceID,
		operation,
		imported.ID,
		taskID,
	); err != nil {
		return err
	}
	completed, err := e.waitImport(ctx, imported.ID)
	if err != nil {
		return e.recordAcceptedOperationWaitError(state, sourceID, operation, err)
	}
	if err := validateRecoveredImport(completed, instanceID, serviceID, importName); err != nil {
		return err
	}
	if err := e.completeAcceptedInstanceMutation(
		state,
		sourceID,
		operation,
		completed.ID,
		taskID,
	); err != nil {
		return err
	}
	e.reportProgress("Target data import ID %d completed.", completed.ID)
	return nil
}

func (e *MigrationExecutor) recoverImport(
	ctx context.Context,
	instanceID, serviceID int,
	importName string,
	operation MigrationOperationState,
) (TargetImport, bool, error) {
	if operation.TargetID > 0 {
		item, err := e.target.GetImport(ctx, operation.TargetID)
		if err != nil {
			return TargetImport{}, false, err
		}
		if validateRecoveredImport(item, instanceID, serviceID, importName) == nil &&
			(operation.Status == MigrationOperationAccepted ||
				createdWithinOperation(item.CreatedAt, operation)) {
			return item, true, nil
		}
		return TargetImport{}, false, nil
	}
	// Query by service alone. When both filters are supplied, Wodby 2 can
	// return every import for the instance instead of narrowing by service.
	items, err := e.target.ListImports(ctx, TargetImportFilters{
		AppServiceID: &serviceID,
	})
	if err != nil {
		return TargetImport{}, false, err
	}
	matches := matchingImports(items, importName, operation.TaskID, operation)
	if len(matches) > 1 {
		return TargetImport{}, false, errors.New("multiple timestamp-bounded target imports match the ambiguous operation")
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return TargetImport{}, false, nil
}

func (e *MigrationExecutor) waitForImportRecord(
	ctx context.Context,
	instanceID, serviceID int,
	importName string,
	taskID int,
	operation MigrationOperationState,
) (TargetImport, error) {
	var found TargetImport
	err := e.poll(ctx, "target import record", func(ctx context.Context) (bool, error) {
		// Keep this service-scoped for the same reason as recoverImport.
		items, err := e.target.ListImports(ctx, TargetImportFilters{
			AppServiceID: &serviceID,
		})
		if err != nil {
			return false, err
		}
		matches := matchingImports(items, importName, taskID, operation)
		if len(matches) > 1 {
			return false, errors.New("multiple target import records match one migration operation")
		}
		if len(matches) == 1 {
			found = matches[0]
			return true, nil
		}
		return false, nil
	})
	return found, err
}

func matchingImports(
	items []TargetImport,
	importName string,
	taskID int,
	operation MigrationOperationState,
) []TargetImport {
	result := []TargetImport{}
	for _, item := range items {
		createdInWindow := createdWithinOperation(item.CreatedAt, operation)
		if taskID > 0 {
			// An exact task relationship is a stable idempotency marker, so a
			// slow queue may create the import after the natural-key window.
			createdInWindow = createdAfterOperationIntent(item.CreatedAt, operation)
		}
		if item.Name != importName || !createdInWindow {
			continue
		}
		if taskID > 0 && (item.TaskID == nil || *item.TaskID != taskID) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func validateRecoveredImport(item TargetImport, instanceID, serviceID int, importName string) error {
	if item.AppInstanceID == nil || *item.AppInstanceID != instanceID ||
		item.AppServiceID == nil || *item.AppServiceID != serviceID ||
		item.Name != importName {
		return errors.New("recorded target import does not match the approved instance and service")
	}
	return nil
}

func (e *MigrationExecutor) waitTask(ctx context.Context, taskID int) error {
	return e.poll(ctx, "target task", func(ctx context.Context) (bool, error) {
		item, err := e.target.GetTask(ctx, taskID)
		if err != nil {
			return false, err
		}
		return taskStatusOutcome(taskID, item.Status)
	})
}

// TargetTerminalOperationError distinguishes a known failed target operation
// from a polling/network error whose final result is still unknown.
type TargetTerminalOperationError struct {
	Resource string
	ID       int
	Status   string
}

func (e *TargetTerminalOperationError) Error() string {
	return fmt.Sprintf(
		"target %s ID %d ended with status %q",
		e.Resource,
		e.ID,
		e.Status,
	)
}

func taskStatusOutcome(taskID int, status string) (bool, error) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "DONE", "DONE_WITH_WARNINGS":
		return true, nil
	case "FAILED", "TIMED_OUT", "CANCELED":
		return false, &TargetTerminalOperationError{
			Resource: "task",
			ID:       taskID,
			Status:   status,
		}
	default:
		// PENDING, QUEUED, IN_PROGRESS, CANCELING, and BACKED_OFF are
		// all active states in Wodby 2.
		return false, nil
	}
}

func (e *MigrationExecutor) waitBuild(ctx context.Context, buildID int) (TargetAppBuild, error) {
	var completed TargetAppBuild
	err := e.poll(ctx, "target application build", func(ctx context.Context) (bool, error) {
		item, err := e.target.GetAppBuild(ctx, buildID)
		if err != nil {
			return false, err
		}
		switch strings.ToUpper(strings.TrimSpace(item.Status)) {
		case "COMPLETED":
			completed = item
			return true, nil
		case "CANCELED", "ERRORED":
			return false, &TargetTerminalOperationError{
				Resource: "build",
				ID:       buildID,
				Status:   item.Status,
			}
		default:
			return false, nil
		}
	})
	return completed, err
}

func (e *MigrationExecutor) waitDeployment(
	ctx context.Context,
	deploymentID int,
) (TargetAppDeployment, error) {
	var completed TargetAppDeployment
	err := e.poll(ctx, "target application deployment", func(ctx context.Context) (bool, error) {
		item, err := e.target.GetAppDeployment(ctx, deploymentID)
		if err != nil {
			return false, err
		}
		switch strings.ToUpper(strings.TrimSpace(item.Status)) {
		case "COMPLETED":
			completed = item
			return true, nil
		case "CANCELED", "ERRORED":
			return false, &TargetTerminalOperationError{
				Resource: "deployment",
				ID:       deploymentID,
				Status:   item.Status,
			}
		default:
			return false, nil
		}
	})
	return completed, err
}

func (e *MigrationExecutor) waitAppInstanceOK(
	ctx context.Context,
	appInstanceID int,
	nextAction string,
) error {
	reportedStatus := ""
	err := e.poll(ctx, "target app instance readiness", func(ctx context.Context) (bool, error) {
		instance, err := e.target.GetAppInstance(ctx, appInstanceID)
		if err != nil {
			return false, err
		}
		status := strings.ToUpper(strings.TrimSpace(instance.Status))
		if status == "OK" {
			if reportedStatus != "" {
				e.reportProgress("Target app instance ID %d is OK; continuing.", appInstanceID)
			}
			return true, nil
		}
		switch status {
		case "ERRORED", "DISABLED", "NA", "DELETING", "PAUSED":
			return false, errors.Errorf(
				"target app instance ID %d entered status %q and cannot %s",
				appInstanceID,
				instance.Status,
				nextAction,
			)
		}
		if status != reportedStatus {
			e.reportProgress(
				"Target app instance ID %d is %q; waiting until it is OK to %s...",
				appInstanceID,
				instance.Status,
				nextAction,
			)
			reportedStatus = status
		}
		return false, nil
	})
	return err
}

func (e *MigrationExecutor) waitImport(ctx context.Context, importID int) (TargetImport, error) {
	var completed TargetImport
	err := e.poll(ctx, "target data import", func(ctx context.Context) (bool, error) {
		item, err := e.target.GetImport(ctx, importID)
		if err != nil {
			return false, err
		}
		switch strings.ToUpper(strings.TrimSpace(item.Status)) {
		case "COMPLETED":
			completed = item
			return true, nil
		case "CANCELED", "ERRORED":
			return false, &TargetTerminalOperationError{
				Resource: "import",
				ID:       importID,
				Status:   item.Status,
			}
		default:
			return false, nil
		}
	})
	return completed, err
}

func (e *MigrationExecutor) poll(
	ctx context.Context,
	label string,
	check func(context.Context) (bool, error),
) error {
	ctx, cancel := context.WithTimeout(ctx, e.operationTimeout)
	defer cancel()
	for {
		done, err := check(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		timer := time.NewTimer(e.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.Errorf("%s did not complete before timeout", label)
		case <-timer.C:
		}
	}
}

func (e *MigrationExecutor) checkCustomDNS(
	ctx context.Context,
	plan Plan,
	cluster TargetCluster,
) error {
	hosts := map[string]bool{}
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, route := range instance.Routes {
				if route.Action == "create_backend" || route.Action == "create_redirect" {
					hosts[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(route.Host)), ".")] = true
				}
			}
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	expected := map[string]bool{}
	for _, candidate := range cluster.IPs {
		if normalized := normalizeIP(candidate); normalized != "" {
			expected[normalized] = true
		}
	}
	if cluster.Hostname != nil && strings.TrimSpace(*cluster.Hostname) != "" {
		items, err := e.lookupHost(ctx, strings.TrimSpace(*cluster.Hostname))
		if err != nil {
			return errors.Wrap(err, "resolve selected target cluster hostname")
		}
		for _, candidate := range items {
			if normalized := normalizeIP(candidate); normalized != "" {
				expected[normalized] = true
			}
		}
	}
	if len(expected) == 0 {
		return errors.New("selected target cluster exposes no verifiable IP address or hostname")
	}
	sorted := make([]string, 0, len(hosts))
	for host := range hosts {
		sorted = append(sorted, host)
	}
	sort.Strings(sorted)
	for _, host := range sorted {
		if host == "" || strings.HasPrefix(host, "*.") {
			return errors.Errorf("custom hostname %q cannot be verified for DNS cutover", host)
		}
		addresses, err := e.lookupHost(ctx, host)
		if err != nil {
			return errors.Wrapf(err, "resolve custom hostname %q", host)
		}
		matched := 0
		for _, address := range addresses {
			normalized := normalizeIP(address)
			if normalized == "" {
				return errors.Errorf("custom hostname %q returned an invalid IP address", host)
			}
			if !expected[normalized] {
				return errors.Errorf("custom hostname %q still resolves outside the selected target cluster", host)
			}
			matched++
		}
		if matched == 0 {
			return errors.Errorf("custom hostname %q does not resolve to the selected target cluster", host)
		}
	}
	return nil
}

func normalizeIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return ip.String()
}

func (e *MigrationExecutor) waitCustomRoutes(
	ctx context.Context,
	plan *InstancePlan,
	instanceID int,
) error {
	if plan == nil {
		return errors.New("migration plan is missing target routes")
	}
	type expectedRoute struct {
		action string
		ssl    bool
	}
	expected := map[string]expectedRoute{}
	for _, item := range plan.Routes {
		switch item.Action {
		case "create_backend":
			expected[item.Host] = expectedRoute{action: TargetRouteActionBackend, ssl: item.SSL}
		case "create_redirect":
			expected[item.Host] = expectedRoute{action: TargetRouteActionRedirect, ssl: item.SSL}
		}
	}
	if len(expected) == 0 {
		return nil
	}
	return e.poll(ctx, "target custom routes", func(ctx context.Context) (bool, error) {
		routes, err := e.target.ListAppRoutes(ctx, instanceID)
		if err != nil {
			return false, err
		}
		seen := map[string]bool{}
		for _, route := range routes {
			wanted, exists := expected[route.Host]
			if !exists || wanted.action != route.Action {
				continue
			}
			if seen[route.Host] {
				return false, errors.Errorf("target returned duplicate custom route hostname %q", route.Host)
			}
			seen[route.Host] = true
			if route.Disabled {
				return false, errors.Errorf("target custom route %q is disabled", route.Host)
			}
			switch strings.ToUpper(strings.TrimSpace(route.Status)) {
			case "OK":
			case "NA":
				return false, errors.Errorf("target custom route %q cannot be activated", route.Host)
			default:
				return false, nil
			}
			certificateReady, err := targetRouteCertificateReady(route, wanted.ssl)
			if err != nil {
				return false, err
			}
			if !certificateReady {
				return false, nil
			}
		}
		return len(seen) == len(expected), nil
	})
}

func targetRouteCertificateReady(route TargetAppRoute, required bool) (bool, error) {
	if !required {
		return true, nil
	}
	if route.Cert == nil {
		return false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(route.Cert.Issuer), "letsencrypt") {
		return false, errors.Errorf(
			"target custom route %q certificate issuer is %q, expected letsencrypt",
			route.Host,
			route.Cert.Issuer,
		)
	}
	switch strings.ToUpper(strings.TrimSpace(route.Cert.Status)) {
	case "OK":
		return true, nil
	case "CREATING", "RENEWING":
		return false, nil
	case "ERRORED", "EXPIRED", "REVOKED", "DELETING":
		return false, errors.Errorf(
			"target custom route %q certificate status is %q",
			route.Host,
			route.Cert.Status,
		)
	default:
		return false, errors.Errorf(
			"target custom route %q returned unsupported certificate status %q",
			route.Host,
			route.Cert.Status,
		)
	}
}

func (e *MigrationExecutor) verifyInstance(
	ctx context.Context,
	prepared PreparedInstance,
	instance TargetAppInstance,
	plan *InstancePlan,
	resource *MigrationResourceState,
) error {
	if plan == nil || resource == nil {
		return errors.New("migration verification state is incomplete")
	}
	if !strings.EqualFold(instance.Status, "OK") {
		return errors.Errorf("target instance %q status is %q, expected OK", instance.Name, instance.Status)
	}
	services, err := e.target.ListAppServices(ctx, instance.ID)
	if err != nil {
		return errors.Wrap(err, "verify target services")
	}
	byName, err := indexAppServices(services)
	if err != nil {
		return err
	}
	deploymentInput, err := technicalDeploymentInput(prepared, services, nil)
	if err != nil {
		return err
	}
	deploymentRecorded := operationSucceeded(resource, operationKey("apply_deploy", prepared.Source.UUID)) ||
		operationSucceeded(resource, operationKey("finalize_deploy", prepared.Source.UUID))
	if !planHasCustomRoutes(plan) {
		deploymentRecorded = deploymentRecorded ||
			operationSucceeded(resource, operationKey("prepare_deploy", prepared.Source.UUID))
	}
	if len(deploymentInput.Services) > 0 && !deploymentRecorded {
		return errors.New("final target deployment is not recorded as complete")
	}
	if err := validateManuallyDeployedCodeServices(prepared, services); err != nil {
		return err
	}
	for _, inspection := range prepared.StackServices {
		target, ok := byName[inspection.StackService.Name]
		if !ok || target.ServiceRevID != inspection.StackService.ServiceRevID {
			return errors.Errorf("target service %q no longer matches the approved stack", inspection.StackService.Name)
		}
		if target.Disabled == prepared.EffectiveState[inspection.StackService.Name] {
			return errors.Errorf("target service %q enabled state no longer matches the source", inspection.StackService.Name)
		}
	}
	if prepared.BuildSource != nil {
		service, ok := byName[prepared.BuildSource.ServiceName]
		if !ok || !operationSucceeded(resource, operationKey("build_source", strconv.Itoa(service.ID))) {
			return errors.New("target build source is not recorded as successfully reconciled")
		}
	}
	for _, source := range prepared.Source.Services {
		if !source.Enabled {
			continue
		}
		mapping, ok := prepared.Services[source.Name]
		if !ok {
			continue
		}
		service, ok := byName[mapping.Target.StackService.Name]
		if !ok {
			return errors.New("mapped target service is missing during verification")
		}
		if mapping.TargetVersion != "" && service.Version != mapping.TargetVersion {
			return errors.Errorf(
				"target service %q version is %q, expected %q",
				service.Name,
				service.Version,
				mapping.TargetVersion,
			)
		}
		if err := e.verifyServiceEnvironment(ctx, service.ID, source, prepared.Source.Properties); err != nil {
			return err
		}
		if err := e.verifyServiceSettings(ctx, service.ID, source); err != nil {
			return err
		}
		if err := e.verifyServiceCrons(
			ctx,
			prepared.Source.UUID,
			service.ID,
			source,
			mapping.Target,
			prepared.DisableCronSchedules,
		); err != nil {
			return err
		}
	}
	if err := e.verifyRoutes(ctx, prepared, instance.ID, plan); err != nil {
		return err
	}
	for component, destination := range prepared.ImportByComponent {
		item, ok := successfulImportOperation(resource, prepared.Source.UUID, component)
		if !ok || item.TargetID <= 0 {
			return errors.Errorf("target data import for component %q is not recorded as complete", component)
		}
		imported, err := e.target.GetImport(ctx, item.TargetID)
		if err != nil {
			return errors.Wrap(err, "verify target data import")
		}
		service, ok := byName[destination.ServiceName]
		if !ok {
			return errors.Errorf("target data import service %q is missing", destination.ServiceName)
		}
		if err := validateRecoveredImport(
			imported,
			instance.ID,
			service.ID,
			destination.ImportName,
		); err != nil {
			return err
		}
		if !strings.EqualFold(imported.Status, "COMPLETED") {
			return errors.Errorf("target data import ID %d status is %q", imported.ID, imported.Status)
		}
	}
	return nil
}

func (e *MigrationExecutor) verifyServiceEnvironment(
	ctx context.Context,
	serviceID int,
	source Service,
	properties map[string]interface{},
) error {
	items, err := e.target.ListAppServiceEnvVars(ctx, serviceID)
	if err != nil {
		return errors.Wrap(err, "verify target service environment")
	}
	byName := map[string][]TargetAppServiceEnvVar{}
	for _, item := range items {
		if !targetMutableGlobalEnvVar(item) {
			continue
		}
		byName[item.Name] = append(byName[item.Name], item)
	}
	for _, variable := range source.EnvVars {
		if !sourceEnvVarRequiresMigration(properties, variable) {
			continue
		}
		matches := byName[variable.Name]
		if len(matches) != 1 {
			return errors.Errorf("target environment variable %q matched %d entries", variable.Name, len(matches))
		}
		item := matches[0]
		if variable.Secret || variable.Protected {
			if item.ValueSecretID == nil {
				return errors.Errorf("target environment variable %q is not stored as a secret", variable.Name)
			}
		} else if item.ValueSecretID != nil || item.Value != variable.Value {
			return errors.Errorf("target environment variable %q no longer matches the source", variable.Name)
		}
	}
	return nil
}

func (e *MigrationExecutor) verifyServiceSettings(
	ctx context.Context,
	serviceID int,
	source Service,
) error {
	if len(source.Configuration) == 0 {
		return nil
	}
	items, err := e.target.ListAppServiceSettings(ctx, serviceID)
	if err != nil {
		return errors.Wrap(err, "verify target service settings")
	}
	byName := map[string][]TargetAppServiceSetting{}
	for _, item := range items {
		byName[item.Name] = append(byName[item.Name], item)
	}
	for name, raw := range source.Configuration {
		value, err := scalarConfigurationValue(raw)
		if err != nil {
			return err
		}
		matches := byName[name]
		if len(matches) != 1 || matches[0].Value != value {
			return errors.Errorf("target service setting %q no longer matches the source", name)
		}
	}
	return nil
}

func (e *MigrationExecutor) verifyServiceCrons(
	ctx context.Context,
	sourceID string,
	serviceID int,
	source Service,
	inspection TargetStackServiceInspection,
	disableMigrated bool,
) error {
	items, err := e.target.ListAppServiceCronSchedules(ctx, serviceID)
	if err != nil {
		return errors.Wrap(err, "verify target cron schedules")
	}
	byName := map[string][]TargetAppServiceCronSchedule{}
	for _, item := range items {
		byName[item.Name] = append(byName[item.Name], item)
	}
	for name := range defaultPHPCronScheduleNames(inspection) {
		matches := byName[name]
		if len(matches) > 1 {
			return errors.Errorf("default target cron schedule %q matched %d entries", name, len(matches))
		}
		if len(matches) == 1 && !matches[0].Disabled {
			return errors.Errorf("default target cron schedule %q is not disabled", name)
		}
	}
	for index, cron := range source.CronJobs {
		if !cron.Enabled || cron.Classification == "source_only_infrastructure" {
			continue
		}
		name := "w1-" + shortDigest(sourceID, source.Name, strconv.Itoa(index), cron.Crontab, cron.Command)
		matches := byName[name]
		if len(matches) != 1 || matches[0].Disabled != disableMigrated ||
			matches[0].Crontab != cron.Crontab || matches[0].Command != cron.Command {
			return errors.Errorf("target cron schedule %q no longer matches the source", name)
		}
	}
	return nil
}

func (e *MigrationExecutor) verifyRoutes(
	ctx context.Context,
	prepared PreparedInstance,
	instanceID int,
	plan *InstancePlan,
) error {
	services, err := e.target.ListAppServices(ctx, instanceID)
	if err != nil {
		return errors.Wrap(err, "verify target route services")
	}
	serviceByName, err := indexAppServices(services)
	if err != nil {
		return err
	}
	ports, err := e.target.ListAppPorts(ctx, instanceID)
	if err != nil {
		return errors.Wrap(err, "verify target route ports")
	}
	routes, err := e.target.ListAppRoutes(ctx, instanceID)
	if err != nil {
		return errors.Wrap(err, "verify target custom routes")
	}
	auths, err := e.target.ListAppAuths(ctx, instanceID)
	if err != nil {
		return errors.Wrap(err, "verify target route authentication")
	}
	for _, expected := range plan.Routes {
		if expected.Action != "create_backend" && expected.Action != "create_redirect" {
			continue
		}
		mapping, ok := prepared.Services[expected.Service]
		if !ok {
			return errors.Errorf("target route %q service mapping is missing", expected.Host)
		}
		service, ok := serviceByName[mapping.Target.StackService.Name]
		if !ok || expected.PortNumber == nil {
			return errors.Errorf("target route %q service or port mapping is missing", expected.Host)
		}
		port, err := exactAppPort(ports, service.ID, *expected.PortNumber)
		if err != nil {
			return err
		}
		// Verification checks the final cutover state. A route that was staged
		// disabled on a free plan must have been enabled (and its source
		// main/primary flags restored) after the organization upgraded.
		matches := matchingRoutes(routes, service.ID, port.ID, expected, false)
		if len(matches) != 1 || matches[0].Disabled || !strings.EqualFold(matches[0].Status, "OK") {
			stagedMatches := matchingRoutes(routes, service.ID, port.ID, expected, true)
			if len(stagedMatches) == 1 && prepared.DisableCustomRoutes {
				return errors.Errorf(
					"target custom route %q is staged disabled because the target subscription did not include custom domains; upgrade the target plan and enable the route before verification",
					expected.Host,
				)
			}
			return errors.Errorf("target custom route %q is not uniquely active", expected.Host)
		}
		route := matches[0]
		certificateReady, err := targetRouteCertificateReady(route, expected.SSL)
		if err != nil {
			return err
		}
		if !certificateReady {
			return errors.Errorf("target custom route %q certificate is not ready", expected.Host)
		}
		settings, err := e.target.ListAppRouteSettings(ctx, route.ID)
		if err != nil {
			return errors.Wrap(err, "verify target route settings")
		}
		for _, wanted := range expected.Settings {
			count := 0
			for _, actual := range settings {
				if actual.Name == wanted.Name && actual.Value == wanted.Value {
					count++
				}
			}
			if count != 1 {
				return errors.Errorf("target route %q setting %q no longer matches", expected.Host, wanted.Name)
			}
		}
		if expected.BasicAuth {
			if prepared.Source.BasicAuth == nil {
				return errors.New("source basic-auth inventory disappeared")
			}
			count := 0
			for _, actual := range auths {
				if actual.AppRouteID != nil && *actual.AppRouteID == route.ID &&
					actual.Login == prepared.Source.BasicAuth.Login {
					count++
				}
			}
			if count != 1 {
				return errors.Errorf("target route %q authentication is missing or ambiguous", expected.Host)
			}
		}
	}
	return nil
}

func planInstance(plan Plan, sourceID string) *InstancePlan {
	for appIndex := range plan.Apps {
		for instanceIndex := range plan.Apps[appIndex].Instances {
			if plan.Apps[appIndex].Instances[instanceIndex].SourceUUID == sourceID {
				return &plan.Apps[appIndex].Instances[instanceIndex]
			}
		}
	}
	return nil
}
