package wodby1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxMigrationPlanBytes = 4 << 20

var (
	ErrMigrationPlanInsecure = errors.New("migration plan file permissions are not 0600")
	ErrMigrationPlanInvalid  = errors.New("invalid migration plan")
)

// LoadReviewedPlan reads a previously reviewed plan from a regular 0600 file.
// Unknown fields and trailing JSON are rejected so the approved hash always
// describes the complete persisted authorization artifact.
func LoadReviewedPlan(path string) (Plan, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return Plan{}, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		return Plan{}, fmt.Errorf("%w: plan path must be a regular file", ErrMigrationPlanInvalid)
	}

	file, err := os.Open(path)
	if err != nil {
		return Plan{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Plan{}, fmt.Errorf("inspect migration plan file: %w", err)
	}
	if !os.SameFile(linkInfo, info) {
		return Plan{}, fmt.Errorf("%w: plan file changed while opening", ErrMigrationPlanInvalid)
	}
	if info.Mode().Perm() != migrationStateFileMode {
		return Plan{}, ErrMigrationPlanInsecure
	}
	if info.Size() > maxMigrationPlanBytes {
		return Plan{}, invalidPlanError("plan file exceeds maximum size")
	}

	data, err := io.ReadAll(io.LimitReader(file, maxMigrationPlanBytes+1))
	if err != nil {
		return Plan{}, fmt.Errorf("read migration plan file: %w", err)
	}
	if len(data) > maxMigrationPlanBytes {
		return Plan{}, invalidPlanError("plan file exceeds maximum size")
	}

	var plan Plan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("%w: decode plan: %v", ErrMigrationPlanInvalid, err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Plan{}, invalidPlanError("plan file contains trailing JSON")
		}
		return Plan{}, fmt.Errorf("%w: decode trailing plan data: %v", ErrMigrationPlanInvalid, err)
	}
	if err := plan.ValidateReviewed(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// ValidateReviewed verifies the schema and approval hash of a plan before any
// of its resolved target identifiers are trusted.
func (p Plan) ValidateReviewed() error {
	if p.Schema != MigrationPlanSchema {
		return invalidPlanError("unsupported schema")
	}
	if !stateDigestPattern.MatchString(p.PlanHash) {
		return invalidPlanError("plan hash must be a lowercase SHA-256 digest")
	}
	digest, err := p.contentDigest()
	if err != nil {
		return fmt.Errorf("%w: compute plan hash: %v", ErrMigrationPlanInvalid, err)
	}
	if digest != p.PlanHash {
		return invalidPlanError("plan hash does not match plan contents")
	}
	if (p.Source.Kind != "app" && p.Source.Kind != "instance" && p.Source.Kind != "server") || p.Source.ID == "" {
		return invalidPlanError("customer plan must identify a source app, instance, or server")
	}
	seenApps := map[string]bool{}
	for _, app := range p.Apps {
		if app.SourceUUID == "" || seenApps[app.SourceUUID] {
			return invalidPlanError("customer plan contains an invalid source app set")
		}
		seenApps[app.SourceUUID] = true
	}
	if p.Source.Kind == "app" && (len(p.Apps) != 1 || p.Apps[0].SourceUUID != p.Source.ID) {
		return invalidPlanError("customer app plan must contain exactly the approved source app")
	}
	if p.Source.Kind == "instance" && (len(p.Apps) != 1 || len(p.Apps[0].Instances) != 1 ||
		p.Apps[0].Instances[0].SourceUUID != p.Source.ID) {
		return invalidPlanError("customer instance plan must contain exactly the approved source instance")
	}
	if p.Source.Kind == "server" && len(p.Apps) == 0 {
		return invalidPlanError("customer server plan must contain at least one source app")
	}
	if p.Target.CIIntegrationID < 0 {
		return invalidPlanError("target CI integration ID must not be negative")
	}
	return nil
}

// PinReviewedTargets copies only preflight-resolved immutable target
// identifiers into a freshly rebuilt plan. Source data and customer options
// remain those of current, so a later hash comparison detects any drift.
func PinReviewedTargets(current *Plan, reviewed Plan) error {
	if current == nil {
		return invalidPlanError("current plan is required")
	}
	if err := reviewed.ValidateReviewed(); err != nil {
		return err
	}

	candidate, err := cloneMigrationPlan(*current)
	if err != nil {
		return err
	}
	if candidate.Source.Kind != reviewed.Source.Kind || candidate.Source.ID != reviewed.Source.ID {
		return currentPlanDriftError("source changed")
	}
	// Subscription capabilities are execution choices once apply starts: for
	// example, they decide whether migrated cron schedules are created disabled.
	// Preserve the reviewed choice on resume while the backend still performs
	// its live atomic entitlement and capacity checks for every mutation.
	candidate.Target.OrgCapabilities = reviewed.Target.OrgCapabilities
	candidate.Target.Subscription = reviewed.Target.Subscription
	reviewedApps := make(map[string]AppPlan, len(reviewed.Apps))
	for _, app := range reviewed.Apps {
		if app.SourceUUID == "" || reviewedApps[app.SourceUUID].SourceUUID != "" {
			return invalidPlanError("reviewed plan contains duplicate source apps")
		}
		reviewedApps[app.SourceUUID] = app
	}
	if len(candidate.Apps) != len(reviewedApps) {
		return currentPlanDriftError("source app set changed")
	}
	for index := range candidate.Apps {
		app := &candidate.Apps[index]
		reviewedApp, found := reviewedApps[app.SourceUUID]
		if !found {
			return currentPlanDriftError("source app set changed")
		}
		if err := pinReviewedApp(app, reviewedApp); err != nil {
			return err
		}
	}

	*current = candidate
	return nil
}

func pinReviewedApp(current *AppPlan, reviewed AppPlan) error {
	if current == nil {
		return invalidPlanError("current app plan is required")
	}
	if err := pinReviewedRepository(current.Repository, reviewed.Repository); err != nil {
		return err
	}
	reviewedInstances := make(map[string]InstancePlan, len(reviewed.Instances))
	for _, instance := range reviewed.Instances {
		if _, exists := reviewedInstances[instance.SourceUUID]; exists {
			return invalidPlanError("reviewed plan contains duplicate source instances")
		}
		reviewedInstances[instance.SourceUUID] = instance
	}
	if len(current.Instances) != len(reviewedInstances) {
		return currentPlanDriftError("source instance set changed")
	}
	for index := range current.Instances {
		instance := &current.Instances[index]
		reviewedInstance, found := reviewedInstances[instance.SourceUUID]
		if !found {
			return currentPlanDriftError("source instance set changed")
		}
		if err := pinReviewedInstance(instance, reviewedInstance); err != nil {
			return err
		}
	}
	return nil
}

func pinReviewedRepository(current *RepositoryPlan, reviewed *RepositoryPlan) error {
	if (current == nil) != (reviewed == nil) {
		return currentPlanDriftError("source repository or code migration options changed")
	}
	if current == nil {
		return nil
	}
	if current.GitIntegrationID != reviewed.GitIntegrationID || current.RepositoryName != reviewed.RepositoryName {
		return currentPlanDriftError("target Git integration or repository name changed")
	}
	if reviewed.Action != "skip" && reviewed.GitIntegrationID > 0 && reviewed.RepositoryName != "" {
		if reviewed.RemoteGitRepoID == "" {
			return invalidPlanError("reviewed target repository is missing its resolved remote ID")
		}
		current.RemoteGitRepoID = reviewed.RemoteGitRepoID
	}
	if current.TargetService != "" && current.TargetService != reviewed.TargetService {
		return currentPlanDriftError("target code service changed")
	}
	if reviewed.TargetService == "" {
		return nil
	}
	current.TargetService = reviewed.TargetService
	return nil
}

func pinReviewedInstance(current *InstancePlan, reviewed InstancePlan) error {
	if reviewed.Stack.Target == "" ||
		reviewed.Stack.TargetID <= 0 ||
		reviewed.Stack.TargetRevID <= 0 ||
		reviewed.Stack.TargetVersion == "" {
		return invalidPlanError("reviewed target stack is missing immutable identity")
	}
	if current.Stack.CreateTarget != reviewed.Stack.CreateTarget ||
		current.Stack.CatalogName != reviewed.Stack.CatalogName {
		return currentPlanDriftError("target stack creation strategy changed")
	}
	if reviewed.Stack.CreateTarget && reviewed.Stack.Target != reviewed.Stack.CatalogName {
		return invalidPlanError("reviewed generated stack does not match its catalog blueprint")
	}
	if current.Stack.ExplicitMapping && current.Stack.TargetID != reviewed.Stack.TargetID {
		return currentPlanDriftError("target stack ID changed")
	}
	current.Stack.Target = reviewed.Stack.Target
	current.Stack.TargetID = reviewed.Stack.TargetID
	current.Stack.TargetRevID = reviewed.Stack.TargetRevID
	current.Stack.TargetVersion = reviewed.Stack.TargetVersion
	if reviewed.BuildServiceID == 0 && reviewed.BuildServiceRevID == 0 {
		current.BuildServiceID = 0
		current.BuildServiceRevID = 0
	} else {
		if reviewed.BuildServiceID <= 0 || reviewed.BuildServiceRevID <= 0 {
			return invalidPlanError("reviewed build service pins are incomplete")
		}
		current.BuildServiceID = reviewed.BuildServiceID
		current.BuildServiceRevID = reviewed.BuildServiceRevID
	}

	reviewedServices := make(map[string]ServicePlan, len(reviewed.Services))
	for _, service := range reviewed.Services {
		if _, exists := reviewedServices[service.SourceName]; exists {
			return invalidPlanError("reviewed plan contains duplicate source services")
		}
		reviewedServices[service.SourceName] = service
	}
	if len(current.Services) != len(reviewedServices) {
		return currentPlanDriftError("source service set changed")
	}
	for index := range current.Services {
		service := &current.Services[index]
		reviewedService, found := reviewedServices[service.SourceName]
		if !found {
			return currentPlanDriftError("source service set changed")
		}
		if service.TargetName != reviewedService.TargetName {
			return currentPlanDriftError("target service mapping changed")
		}
		if service.SourceVersion != reviewedService.SourceVersion {
			return currentPlanDriftError("source service version changed")
		}
		if service.VersionExplicit != reviewedService.VersionExplicit {
			return currentPlanDriftError("target service version override changed")
		}
		if service.Enabled && service.VersionExplicit && service.TargetVersion != reviewedService.TargetVersion {
			return currentPlanDriftError("target service version override changed")
		}
		service.TargetVersion = reviewedService.TargetVersion
		service.VersionAction = reviewedService.VersionAction
		if reviewedService.AddToStack {
			if reviewedService.CatalogServiceID <= 0 || reviewedService.CatalogServiceRevID <= 0 ||
				reviewedService.TargetID != 0 || reviewedService.TargetServiceRevID != 0 {
				return invalidPlanError("reviewed additional target service pins are invalid")
			}
			service.AddToStack = true
			service.CatalogServiceID = reviewedService.CatalogServiceID
			service.CatalogServiceRevID = reviewedService.CatalogServiceRevID
			continue
		}
		if reviewedService.TargetID == 0 && reviewedService.TargetServiceRevID == 0 {
			continue
		}
		if reviewedService.TargetID <= 0 || reviewedService.TargetServiceRevID <= 0 {
			return invalidPlanError("reviewed target service pins are incomplete")
		}
		service.TargetID = reviewedService.TargetID
		service.TargetServiceRevID = reviewedService.TargetServiceRevID
	}

	reviewedImports := make(map[string]ImportPlan, len(reviewed.Imports))
	for _, item := range reviewed.Imports {
		key := normalizedImportComponent(item.Component)
		if key == "" {
			return invalidPlanError("reviewed import is missing a component")
		}
		if _, exists := reviewedImports[key]; exists {
			return invalidPlanError("reviewed plan contains duplicate import components")
		}
		reviewedImports[key] = item
	}
	if len(current.Imports) != len(reviewedImports) {
		return currentPlanDriftError("source backup component set changed")
	}
	for index := range current.Imports {
		item := &current.Imports[index]
		reviewedImport, found := reviewedImports[normalizedImportComponent(item.Component)]
		if !found {
			return currentPlanDriftError("source backup component set changed")
		}
		if item.TargetService != "" && item.TargetService != reviewedImport.TargetService {
			return currentPlanDriftError("target import mapping changed")
		}
		if item.TargetImport != "" && item.TargetImport != reviewedImport.TargetImport {
			return currentPlanDriftError("target import mapping changed")
		}
		if reviewedImport.Action == "skip" {
			continue
		}
		if reviewedImport.TargetService == "" ||
			reviewedImport.TargetImport == "" ||
			reviewedImport.TargetServiceID <= 0 ||
			reviewedImport.TargetServiceRevID <= 0 {
			return invalidPlanError("reviewed import is missing immutable target identity")
		}
		item.TargetService = reviewedImport.TargetService
		item.TargetImport = reviewedImport.TargetImport
		item.TargetServiceID = reviewedImport.TargetServiceID
		item.TargetServiceRevID = reviewedImport.TargetServiceRevID
	}
	return nil
}

func cloneMigrationPlan(plan Plan) (Plan, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: clone current plan: %v", ErrMigrationPlanInvalid, err)
	}
	var clone Plan
	if err := json.Unmarshal(data, &clone); err != nil {
		return Plan{}, fmt.Errorf("%w: clone current plan: %v", ErrMigrationPlanInvalid, err)
	}
	return clone, nil
}

func normalizedImportComponent(component string) string {
	return string(bytes.ToLower(bytes.TrimSpace([]byte(component))))
}

func invalidPlanError(message string) error {
	return fmt.Errorf("%w: %s", ErrMigrationPlanInvalid, message)
}

func currentPlanDriftError(message string) error {
	return fmt.Errorf("current source or migration options no longer match the reviewed plan: %s", message)
}
