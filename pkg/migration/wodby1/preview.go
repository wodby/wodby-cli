package wodby1

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// PreviewRequest selects what a migration preview reads. It holds the same
// choices as the command flags of a first preview, without local state.
type PreviewRequest struct {
	// SourceKind is "instance" or "app".
	SourceKind string
	SourceID   string

	TargetProject    string
	TargetCluster    string
	TargetApp        string
	TargetStackID    int
	TargetEnvMap     map[string]string
	TargetStackMap   map[string]string
	TargetServiceMap map[string]string
	TargetVersionMap map[string]string
	TargetImportMap  map[string]string

	TargetCIIntegrationID     int
	TargetWodbyCI             bool
	TargetLetsEncrypt         bool
	TargetBackupIntegrationID int
	TargetBackupBucket        string
	TargetBackupWindow        string
	TargetSMTPIntegrationID   int
	GitIntegrationID          int
	RepositoryName            string
	CodeService               string
	GitRef                    string
	GitRefType                string
	SkipCode                  bool
	SkipData                  bool
	AllowUnsupportedDrupal    bool
	AddMissingServices        bool
	SourceBackups             []string
	ExcludeInstances          []string

	// WorkspaceInstances holds the UUIDs of direct-Git source instances to
	// migrate to development workspaces.
	WorkspaceInstances      map[string]bool
	WorkspaceStorageClass   string
	WorkspaceStorageService string

	// SourceToken authenticates the source configuration fingerprint; it is the
	// token the source client uses.
	SourceToken string
}

// PreviewResult is a reviewed-free preview: the plan and, when the target
// could be inspected, what applying it would change.
type PreviewResult struct {
	Plan     Plan
	Prepared *PreparedMigration
}

// Preview builds a migration plan and inspects the target the way a first
// preview of `wodby migrate wodby1` does. It changes nothing on either
// platform and keeps no state.
func Preview(ctx context.Context, source *SourceClient, target *TargetClient, req PreviewRequest) (PreviewResult, error) {
	if source == nil || target == nil {
		return PreviewResult{}, errors.New("source and target clients are required")
	}
	var exportSource func(context.Context, string, SourceBackupSelection) (Export, error)
	switch req.SourceKind {
	case "app":
		exportSource = source.ExportAppWithBackups
	case "instance":
		exportSource = source.ExportInstanceWithBackups
	default:
		return PreviewResult{}, errors.Errorf("unsupported migration source kind %q", req.SourceKind)
	}
	if strings.TrimSpace(req.TargetApp) != "" && req.SourceKind != "instance" {
		return PreviewResult{}, errors.New("a target app can be selected only for an instance migration")
	}

	// Verify the caller's role in the destination organization before asking
	// Wodby 1 for the secret-bearing export.
	scope, err := target.DiscoverTargetScope(ctx, TargetScopeSelectors{Project: req.TargetProject, Cluster: req.TargetCluster})
	if err != nil {
		return PreviewResult{}, err
	}
	var targetApp *TargetApp
	stackID := req.TargetStackID
	if selector := strings.TrimSpace(req.TargetApp); selector != "" {
		app, resolvedStackID, err := target.ResolveInstanceTargetApp(ctx, &scope, selector, stackID)
		if err != nil {
			return PreviewResult{}, err
		}
		targetApp, stackID = &app, resolvedStackID
	}

	export, err := exportSource(ctx, req.SourceID, SourceBackupSelection{})
	if err != nil {
		return PreviewResult{}, err
	}
	if len(req.SourceBackups) != 0 {
		selection, err := ResolveSourceBackups(export, req.SourceKind, req.SourceBackups)
		if err != nil {
			return PreviewResult{}, err
		}
		if export, err = exportSource(ctx, req.SourceID, selection); err != nil {
			return PreviewResult{}, err
		}
	}
	export, selection, err := SelectExport(export, req.SourceKind, nil, req.ExcludeInstances, req.SourceToken)
	if err != nil {
		return PreviewResult{}, err
	}

	selectors, err := TargetEnvironmentSelectors(export, req.TargetEnvMap)
	if err != nil {
		return PreviewResult{}, err
	}
	resolved, err := target.ResolveTargetEnvs(ctx, scope.Org.ID, selectors)
	if err != nil {
		return PreviewResult{}, err
	}
	targetEnvs := make(map[string]TargetEnv, len(resolved))
	for _, item := range resolved {
		targetEnvs[item.Selector] = item.Env
	}

	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:                    req.SourceKind,
		SourceID:                      req.SourceID,
		TargetOrg:                     scope.Org.Name,
		TargetProject:                 req.TargetProject,
		TargetCluster:                 req.TargetCluster,
		TargetEnvMap:                  req.TargetEnvMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackID:                 stackID,
		TargetApp:                     targetApp,
		TargetStackMap:                req.TargetStackMap,
		TargetServiceMap:              req.TargetServiceMap,
		TargetVersionMap:              req.TargetVersionMap,
		TargetImportMap:               req.TargetImportMap,
		TargetCIIntegrationID:         req.TargetCIIntegrationID,
		TargetWodbyCI:                 req.TargetWodbyCI,
		TargetLetsEncrypt:             req.TargetLetsEncrypt,
		TargetBackupIntegrationID:     req.TargetBackupIntegrationID,
		TargetBackupBucket:            req.TargetBackupBucket,
		TargetBackupWindow:            req.TargetBackupWindow,
		TargetSMTPIntegrationID:       req.TargetSMTPIntegrationID,
		Repository: RepositoryTargetPlan{
			GitIntegrationID: req.GitIntegrationID,
			RepositoryName:   strings.TrimSpace(req.RepositoryName),
			Service:          strings.TrimSpace(req.CodeService),
		},
		SkipCode:                req.SkipCode,
		SkipData:                req.SkipData,
		RequireData:             !req.SkipData,
		AllowUnsupportedDrupal:  req.AllowUnsupportedDrupal,
		Selection:               &selection,
		WorkspaceInstances:      req.WorkspaceInstances,
		WorkspaceStorageClass:   req.WorkspaceStorageClass,
		WorkspaceStorageService: req.WorkspaceStorageService,
	})
	if err != nil {
		return PreviewResult{}, err
	}
	// The target cannot be inspected without a target stack; the plan already
	// blocks on it.
	if PlanNeedsTargetStack(plan) {
		return PreviewResult{Plan: plan}, nil
	}
	prepared, err := target.PreflightTarget(ctx, export, &plan, TargetPreflightOptions{
		SkipCode:           req.SkipCode,
		SkipData:           req.SkipData,
		GitRef:             req.GitRef,
		GitRefType:         req.GitRefType,
		CodeService:        req.CodeService,
		AddMissingServices: req.AddMissingServices,
	})
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Plan: plan, Prepared: &prepared}, nil
}

// PlanNeedsTargetStack reports a plan that already blocks because a custom
// source stack has no target stack; the target cannot be inspected without
// one.
func PlanNeedsTargetStack(plan Plan) bool {
	for _, item := range plan.Review {
		if item.Severity == SeverityBlocking && item.Subject == "target stack" {
			return true
		}
	}
	return false
}

// ResolveInstanceTargetApp resolves an existing Wodby 2 app that will receive
// a migrated instance and the stack its new app environment uses. It updates
// scope.Project when the app belongs to a project the cluster selection did
// not name. stackID is an explicit stack choice, or 0 to infer it.
func (c *TargetClient) ResolveInstanceTargetApp(ctx context.Context, scope *TargetScopeDiscovery, selector string, stackID int) (TargetApp, int, error) {
	app, err := c.ResolveTargetApp(ctx, scope.Org.ID, selector)
	if err != nil {
		return TargetApp{}, 0, err
	}
	if app.ClusterApp {
		return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) is a cluster infrastructure app and cannot receive a migrated customer app environment", app.Name, app.ID)
	}
	if status := strings.ToUpper(strings.TrimSpace(app.Status)); status != "OK" {
		return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) has status %q and cannot receive a migrated app environment", app.Name, app.ID, app.Status)
	}
	switch strings.ToLower(strings.TrimSpace(app.OwnershipScope)) {
	case TargetOwnershipScopeOrg:
		if scope.Project.ID > 0 {
			return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) is organization-owned, but the selected cluster is owned by project %q (ID %d); select an organization-owned cluster", app.Name, app.ID, scope.Project.Name, scope.Project.ID)
		}
	case TargetOwnershipScopeProject:
		if app.OwnerProjectID == nil || *app.OwnerProjectID <= 0 {
			return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) did not return a valid owner project", app.Name, app.ID)
		}
		if scope.Project.ID == 0 {
			project, err := c.GetProject(ctx, *app.OwnerProjectID)
			if err != nil {
				return TargetApp{}, 0, errors.Wrap(err, "resolve selected target app owner project")
			}
			if project.OrgID != scope.Org.ID {
				return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) owner project belongs to organization ID %d, expected %d", app.Name, app.ID, project.OrgID, scope.Org.ID)
			}
			scope.Project = project
		} else if scope.Project.ID != *app.OwnerProjectID {
			return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) belongs to project ID %d, but the selected cluster belongs to project ID %d; select a cluster owned by the app's project", app.Name, app.ID, *app.OwnerProjectID, scope.Project.ID)
		}
	default:
		return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) did not return an ownership scope; update the Wodby 2 backend before using --target-app", app.Name, app.ID)
	}
	instances, err := c.ListAppInstances(ctx, scope.Org.ID, app.ID)
	if err != nil {
		return TargetApp{}, 0, err
	}
	stackIDs := targetAppStackIDs(instances)
	if stackID == 0 {
		switch len(stackIDs) {
		case 0:
			return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) has no app environments from which to infer a stack; provide --target-stack-id", app.Name, app.ID)
		case 1:
			stackID = stackIDs[0]
		default:
			return TargetApp{}, 0, errors.Errorf("target app %q (ID %d) uses multiple stacks (%s); provide --target-stack-id with one of these stack IDs", app.Name, app.ID, joinIntValues(stackIDs))
		}
	} else if len(stackIDs) != 0 && !containsInt(stackIDs, stackID) {
		return TargetApp{}, 0, errors.Errorf("--target-stack-id %d is not used by target app %q (ID %d); choose one of %s", stackID, app.Name, app.ID, joinIntValues(stackIDs))
	}
	return app, stackID, nil
}

func targetAppStackIDs(instances []TargetAppInstance) []int {
	seen := map[int]bool{}
	for _, instance := range instances {
		if instance.StackID > 0 {
			seen[instance.StackID] = true
		}
	}
	result := make([]int, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Ints(result)
	return result
}

func containsInt(values []int, expected int) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func joinIntValues(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}
