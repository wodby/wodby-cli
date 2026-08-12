package wodby1

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

const stackPublishOperation = "stack_config_publish"

func (e *MigrationExecutor) ensureTargetStackServices(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedMigration,
) (PreparedMigration, error) {
	if len(prepared.StackAdditions) == 0 || len(prepared.Instances) == 0 {
		return prepared, nil
	}
	current, err := e.target.GetStack(ctx, prepared.Instances[0].Stack.ID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "read target stack before adding services")
	}
	if current.OrgID != plan.Target.OrgID || current.Public {
		return PreparedMigration{}, errors.New("target stack service additions no longer match migration ownership")
	}
	if !migrationCreatesTargetStack(plan, prepared) && current.DraftRevID != nil && !stackConfigurationStarted(state) {
		return PreparedMigration{}, errors.New("explicit target stack has an unpublished draft; publish or discard it before applying the migration")
	}
	revisionID := current.RevID
	if current.DraftRevID != nil {
		revisionID = *current.DraftRevID
	}
	inspections, err := e.target.InspectStackRevision(ctx, revisionID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "inspect target stack before adding services")
	}
	byName, err := indexStackInspections(inspections)
	if err != nil {
		return PreparedMigration{}, err
	}
	e.reportProgress("Step: add mapped services missing from target stack %q (ID %d).", current.Name, current.ID)
	for _, addition := range prepared.StackAdditions {
		matching, exists := byName[addition.Name]
		matches := exists && matching.StackService.ServiceRevID == addition.ServiceRevisionID
		if exists && !matches {
			return PreparedMigration{}, errors.Errorf("target stack service %q uses revision ID %d, expected reviewed revision ID %d", addition.Name, matching.StackService.ServiceRevID, addition.ServiceRevisionID)
		}
		operation := operationKey("stack_service_add", addition.Name)
		run, err := e.beginStackConfigurationMutation(state, operation, matches, matching.StackService.ID)
		if err != nil {
			return PreparedMigration{}, err
		}
		if !run {
			continue
		}
		pinned := true
		e.reportProgress("  Adding service %q from Wodby 2 service ID %d (revision ID %d)...", addition.Name, addition.ServiceID, addition.ServiceRevisionID)
		created, createErr := e.target.CreateStackService(ctx, TargetCreateStackServiceInput{
			StackID: current.ID, ServiceID: addition.ServiceID, Name: addition.Name,
			Title: addition.Title, Required: false, Replicas: 1, ServiceRevPinned: &pinned,
		})
		if createErr != nil {
			return PreparedMigration{}, e.recordStackConfigurationMutationError(state, operation, "stack service creation", createErr)
		}
		if created.ServiceRevID != addition.ServiceRevisionID {
			if markErr := state.MarkAppOperationAmbiguousWithIDs(operation, created.ID, 0); markErr != nil {
				return PreparedMigration{}, markErr
			}
			if saveErr := SaveMigrationState(e.statePath, state); saveErr != nil {
				return PreparedMigration{}, saveErr
			}
			return PreparedMigration{}, errors.Errorf("added stack service %q resolved to revision ID %d instead of reviewed revision ID %d; inspect the target before resuming", addition.Name, created.ServiceRevID, addition.ServiceRevisionID)
		}
		if err := e.completeStackConfigurationMutation(state, operation, created.ID); err != nil {
			return PreparedMigration{}, err
		}
		e.reportProgress("  Stack service %q added (ID %d).", addition.Name, created.ID)
	}
	current, err = e.target.GetStack(ctx, current.ID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "read target stack after adding services")
	}
	revisionID = current.RevID
	if current.DraftRevID != nil {
		revisionID = *current.DraftRevID
	}
	return e.bindTargetStackRevision(ctx, prepared, current, revisionID, true)
}

func (e *MigrationExecutor) bindAppliedTargetStack(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedMigration,
) (PreparedMigration, error) {
	if len(prepared.Instances) == 0 {
		return PreparedMigration{}, errors.New("prepared migration has no target stack")
	}
	stackID := prepared.Instances[0].Stack.ID
	if migrationCreatesTargetStack(plan, prepared) {
		operation, ok := state.App.Operations[generatedStackOperation]
		if !ok || operation.Status != MigrationOperationSucceeded || operation.TargetID <= 0 {
			return PreparedMigration{}, errors.New("migration state does not contain a completed generated target stack")
		}
		stackID = operation.TargetID
	}
	stack, err := e.target.GetStack(ctx, stackID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "read applied target stack")
	}
	if stackConfigurationHasChanges(prepared.StackConfiguration) || len(prepared.StackAdditions) != 0 {
		operation, ok := state.App.Operations[stackPublishOperation]
		if !ok || operation.Status != MigrationOperationSucceeded || operation.TargetID != stack.ID || operation.TaskID != stack.RevID {
			return PreparedMigration{}, errors.New("migration state does not contain the published target stack configuration revision")
		}
	}
	return e.bindTargetStackRevision(ctx, prepared, stack, stack.RevID, len(prepared.StackAdditions) != 0)
}

func (e *MigrationExecutor) ensureTargetStackConfiguration(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedMigration,
) (PreparedMigration, error) {
	if len(prepared.Instances) == 0 || (!stackConfigurationHasChanges(prepared.StackConfiguration) && len(prepared.StackAdditions) == 0) {
		return prepared, nil
	}
	stack := prepared.Instances[0].Stack
	current, err := e.target.GetStack(ctx, stack.ID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "read target stack before configuration")
	}
	if current.OrgID != plan.Target.OrgID || current.Public {
		return PreparedMigration{}, errors.New("target stack configuration ownership no longer matches the migration target")
	}
	createTarget := migrationCreatesTargetStack(plan, prepared)
	if !createTarget && current.DraftRevID != nil && !stackConfigurationStarted(state) {
		return PreparedMigration{}, errors.New("explicit target stack has an unpublished draft; publish or discard it before applying the migration")
	}

	revisionID := current.RevID
	if current.DraftRevID != nil {
		revisionID = *current.DraftRevID
	}
	inspections, err := e.target.InspectStackRevision(ctx, revisionID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "inspect target stack configuration revision")
	}
	byName, err := indexStackInspections(inspections)
	if err != nil {
		return PreparedMigration{}, err
	}
	e.reportProgress("Step: configure target stack %q (ID %d) before creating app instances.", current.Name, current.ID)
	names := sortedPreparedStackServiceNames(prepared.StackConfiguration)
	for _, name := range names {
		inspection, ok := byName[name]
		if !ok {
			return PreparedMigration{}, errors.Errorf("target stack configuration service %q disappeared", name)
		}
		configuration := prepared.StackConfiguration.Services[name]
		if err := e.ensureStackServiceVersionOptions(ctx, state, inspection.StackService, configuration.VersionOptions); err != nil {
			return PreparedMigration{}, err
		}
		if err := e.ensureStackServiceSettings(ctx, state, inspection.StackService, configuration.Settings); err != nil {
			return PreparedMigration{}, err
		}
		if err := e.ensureStackServiceEnvVars(ctx, state, inspection.StackService, configuration.EnvVars); err != nil {
			return PreparedMigration{}, err
		}
		if err := e.ensureStackServiceCronSchedules(ctx, state, inspection.StackService, configuration.CronSchedules); err != nil {
			return PreparedMigration{}, err
		}
	}

	current, err = e.target.GetStack(ctx, current.ID)
	if err != nil {
		return PreparedMigration{}, errors.Wrap(err, "read configured target stack")
	}
	if current.DraftRevID == nil {
		if operationSucceeded(&state.App, stackPublishOperation) {
			operation := state.App.Operations[stackPublishOperation]
			if operation.TargetID != current.ID || operation.TaskID != current.RevID {
				return PreparedMigration{}, errors.New("published target stack revision no longer matches migration state")
			}
			return e.bindTargetStackRevision(ctx, prepared, current, current.RevID, true)
		}
		// A publish response may have been lost. If every configuration item is
		// present in the active revision, recover the idempotent operation.
		matches, matchErr := e.targetStackConfigurationMatches(ctx, current.RevID, prepared.StackConfiguration)
		if matchErr != nil {
			return PreparedMigration{}, matchErr
		}
		if !matches {
			return PreparedMigration{}, errors.New("target stack configuration did not create a draft and does not match the migration plan")
		}
		if _, exists := state.App.Operations[stackPublishOperation]; exists {
			if err := promoteAppOperationForRecovery(state, stackPublishOperation); err != nil {
				return PreparedMigration{}, err
			}
		} else if err := state.MarkAppOperationIntent(stackPublishOperation); err != nil {
			return PreparedMigration{}, err
		}
		if err := state.MarkAppOperationSuccessWithIDs(stackPublishOperation, current.ID, current.RevID); err != nil {
			return PreparedMigration{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return PreparedMigration{}, err
		}
		e.reportProgress("Recovered published target stack revision ID %d from migration state.", current.RevID)
		return e.bindTargetStackRevision(ctx, prepared, current, current.RevID, true)
	}

	if operationSucceeded(&state.App, stackPublishOperation) {
		return PreparedMigration{}, errors.New("target stack unexpectedly has a draft after its migration revision was published")
	}
	retryID := "app:" + state.Source.ID + ":" + stackPublishOperation
	if operation, exists := state.App.Operations[stackPublishOperation]; exists && operation.Status == MigrationOperationAmbiguous && !e.ambiguousRetryAuthorized(retryID) {
		return PreparedMigration{}, ambiguousRetryRequiredError("target stack publish result is ambiguous", retryID)
	}
	if err := state.MarkAppOperationIntent(stackPublishOperation); err != nil {
		return PreparedMigration{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return PreparedMigration{}, err
	}
	e.reportProgress("Publishing the configured target stack draft (revision ID %d)...", *current.DraftRevID)
	published, err := e.target.PublishStackDraft(ctx, current.ID)
	if err != nil {
		return PreparedMigration{}, e.recordAppMutationError(state, stackPublishOperation, "target stack draft publish", retryID, err)
	}
	if err := state.MarkAppOperationSuccessWithIDs(stackPublishOperation, published.ID, published.RevID); err != nil {
		return PreparedMigration{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return PreparedMigration{}, err
	}
	e.reportProgress("Target stack %q configuration published as revision ID %d.", published.Name, published.RevID)
	return e.bindTargetStackRevision(ctx, prepared, published, published.RevID, true)
}

func (e *MigrationExecutor) ensureStackServiceVersionOptions(ctx context.Context, state *MigrationState, service TargetStackService, desired []TargetStackServiceOptionInput) error {
	if len(desired) == 0 {
		return nil
	}
	operation := operationKey("stack_version", service.Name)
	matches := stackServiceOptionsMatch(service.Options, desired)
	run, err := e.beginStackConfigurationMutation(state, operation, matches, service.ID)
	if err != nil || !run {
		return err
	}
	e.reportProgress("  Setting default version %q for stack service %q...", selectedStackServiceVersion(desired), service.Name)
	if err := e.target.SetStackServiceOptions(ctx, service.ID, desired); err != nil {
		return e.recordStackConfigurationMutationError(state, operation, "stack service version options", err)
	}
	return e.completeStackConfigurationMutation(state, operation, service.ID)
}

func (e *MigrationExecutor) ensureStackServiceSettings(ctx context.Context, state *MigrationState, service TargetStackService, desired map[string]string) error {
	current := map[string]string{}
	for _, setting := range service.Settings {
		if _, exists := current[setting.Name]; exists {
			return errors.Errorf("target stack service %q returned duplicate setting %q", service.Name, setting.Name)
		}
		current[setting.Name] = setting.Value
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		operation := operationKey("stack_setting", service.Name, name)
		run, err := e.beginStackConfigurationMutation(state, operation, current[name] == desired[name], service.ID)
		if err != nil || !run {
			return err
		}
		e.reportProgress("  Applying setting %q to stack service %q...", name, service.Name)
		if err := e.target.SetStackServiceSetting(ctx, service.ID, name, desired[name]); err != nil {
			return e.recordStackConfigurationMutationError(state, operation, "stack service setting", err)
		}
		if err := e.completeStackConfigurationMutation(state, operation, service.ID); err != nil {
			return err
		}
		current[name] = desired[name]
	}
	return nil
}

func (e *MigrationExecutor) ensureStackServiceEnvVars(ctx context.Context, state *MigrationState, service TargetStackService, desired []PreparedStackEnvVar) error {
	current, err := e.target.ListStackServiceEnvVars(ctx, service.ID)
	if err != nil {
		return err
	}
	for _, variable := range desired {
		matches := matchingStackEnvVars(current, variable.Name, variable.EnvType)
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{Resource: "stack service environment variable", Name: variable.Name, Count: len(matches)}
		}
		operation := operationKey("stack_env", service.Name, variable.Name, optionalStringValue(variable.EnvType))
		matchesValue := len(matches) == 1 && stackEnvVarMatches(matches[0], variable)
		if variable.Secret && !operationSucceeded(&state.App, operation) {
			// A redacted existing value cannot be assumed equal until this
			// migration has written it.
			matchesValue = false
		}
		run, err := e.beginStackConfigurationMutation(state, operation, matchesValue, service.ID)
		if err != nil || !run {
			return err
		}
		if len(matches) == 1 {
			e.reportProgress("  Updating stack environment variable %q%s on service %q...", variable.Name, stackEnvScopeLabel(variable.EnvType), service.Name)
			updated, updateErr := e.target.UpdateStackServiceEnvVar(ctx, matches[0].ID, TargetUpdateStackServiceEnvVarInput{Value: variable.Value, Secret: variable.Secret})
			if updateErr != nil {
				return e.recordStackConfigurationMutationError(state, operation, "stack environment variable update", updateErr)
			}
			current = replaceStackEnvVar(current, matches[0].ID, updated)
			if err := e.completeStackConfigurationMutation(state, operation, updated.ID); err != nil {
				return err
			}
			continue
		}
		e.reportProgress("  Creating stack environment variable %q%s on service %q...", variable.Name, stackEnvScopeLabel(variable.EnvType), service.Name)
		created, createErr := e.target.CreateStackServiceEnvVar(ctx, service.ID, TargetCreateStackServiceEnvVarInput{
			Name: variable.Name, Value: variable.Value, Secret: variable.Secret, EnvType: variable.EnvType,
		})
		if createErr != nil {
			return e.recordStackConfigurationMutationError(state, operation, "stack environment variable creation", createErr)
		}
		current = append(current, created)
		if err := e.completeStackConfigurationMutation(state, operation, created.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *MigrationExecutor) ensureStackServiceCronSchedules(ctx context.Context, state *MigrationState, service TargetStackService, desired []PreparedStackCronSchedule) error {
	current, err := e.target.ListStackServiceCronSchedules(ctx, service.ID)
	if err != nil {
		return err
	}
	for _, cron := range desired {
		matches := matchingStackCronSchedules(current, cron.Name, cron.EnvType)
		if len(matches) > 1 {
			return &TargetAmbiguousMatchError{Resource: "stack service cron schedule", Name: cron.Name, Count: len(matches)}
		}
		operation := operationKey("stack_cron", service.Name, cron.Name, optionalStringValue(cron.EnvType))
		matchesValue := len(matches) == 1 && stackCronScheduleMatches(matches[0], cron)
		run, err := e.beginStackConfigurationMutation(state, operation, matchesValue, service.ID)
		if err != nil || !run {
			return err
		}
		if len(matches) == 1 {
			e.reportProgress("  Updating stack cron schedule %q%s on service %q...", cron.Title, stackEnvScopeLabel(cron.EnvType), service.Name)
			updated, updateErr := e.target.UpdateStackServiceCronSchedule(ctx, matches[0].ID, TargetUpdateStackServiceCronScheduleInput{
				Disabled: &cron.Disabled, Title: &cron.Title, Crontab: &cron.Crontab, Command: &cron.Command, Workload: cron.Workload, EnvType: cron.EnvType,
			})
			if updateErr != nil {
				return e.recordStackConfigurationMutationError(state, operation, "stack cron schedule update", updateErr)
			}
			current = replaceStackCronSchedule(current, matches[0].ID, updated)
			if err := e.completeStackConfigurationMutation(state, operation, updated.ID); err != nil {
				return err
			}
			continue
		}
		e.reportProgress("  Creating stack cron schedule %q%s on service %q...", cron.Title, stackEnvScopeLabel(cron.EnvType), service.Name)
		created, createErr := e.target.CreateStackServiceCronSchedule(ctx, service.ID, TargetCreateStackServiceCronScheduleInput{
			Name: cron.Name, Title: cron.Title, Crontab: cron.Crontab, Command: cron.Command, Workload: cron.Workload, Disabled: &cron.Disabled, EnvType: cron.EnvType,
		})
		if createErr != nil {
			return e.recordStackConfigurationMutationError(state, operation, "stack cron schedule creation", createErr)
		}
		current = append(current, created)
		if err := e.completeStackConfigurationMutation(state, operation, created.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *MigrationExecutor) beginStackConfigurationMutation(state *MigrationState, operation string, matches bool, targetID int) (bool, error) {
	current, exists := state.App.Operations[operation]
	if exists && current.Status == MigrationOperationSucceeded {
		if !matches {
			return false, errors.Errorf("target stack configuration changed after operation %q completed", operation)
		}
		return false, nil
	}
	if matches {
		if exists {
			if err := promoteAppOperationForRecovery(state, operation); err != nil {
				return false, err
			}
		} else if err := state.MarkAppOperationIntent(operation); err != nil {
			return false, err
		}
		if err := state.MarkAppOperationSuccessWithIDs(operation, targetID, 0); err != nil {
			return false, err
		}
		return false, SaveMigrationState(e.statePath, state)
	}
	if exists && current.Status == MigrationOperationAmbiguous {
		retryID := "app:" + state.Source.ID + ":" + operation
		if !e.ambiguousRetryAuthorized(retryID) {
			return false, ambiguousRetryRequiredError("target stack configuration result is ambiguous", retryID)
		}
	}
	if err := state.MarkAppOperationIntent(operation); err != nil {
		return false, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return false, err
	}
	return true, nil
}

func (e *MigrationExecutor) completeStackConfigurationMutation(state *MigrationState, operation string, targetID int) error {
	if err := state.MarkAppOperationSuccessWithIDs(operation, targetID, 0); err != nil {
		return err
	}
	return SaveMigrationState(e.statePath, state)
}

func (e *MigrationExecutor) recordStackConfigurationMutationError(state *MigrationState, operation, label string, err error) error {
	return e.recordAppMutationError(state, operation, label, "app:"+state.Source.ID+":"+operation, err)
}

func (e *MigrationExecutor) targetStackConfigurationMatches(ctx context.Context, revisionID int, desired PreparedStackConfiguration) (bool, error) {
	inspections, err := e.target.InspectStackRevision(ctx, revisionID)
	if err != nil {
		return false, err
	}
	byName, err := indexStackInspections(inspections)
	if err != nil {
		return false, err
	}
	for name, configuration := range desired.Services {
		service, ok := byName[name]
		if !ok || (len(configuration.VersionOptions) != 0 && !stackServiceOptionsMatch(service.StackService.Options, configuration.VersionOptions)) {
			return false, nil
		}
		settings := map[string]string{}
		for _, setting := range service.StackService.Settings {
			settings[setting.Name] = setting.Value
		}
		for setting, value := range configuration.Settings {
			if settings[setting] != value {
				return false, nil
			}
		}
		envVars, err := e.target.ListStackServiceEnvVars(ctx, service.StackService.ID)
		if err != nil {
			return false, err
		}
		for _, variable := range configuration.EnvVars {
			matches := matchingStackEnvVars(envVars, variable.Name, variable.EnvType)
			if len(matches) != 1 || !stackEnvVarMatches(matches[0], variable) {
				return false, nil
			}
		}
		crons, err := e.target.ListStackServiceCronSchedules(ctx, service.StackService.ID)
		if err != nil {
			return false, err
		}
		for _, cron := range configuration.CronSchedules {
			matches := matchingStackCronSchedules(crons, cron.Name, cron.EnvType)
			if len(matches) != 1 || !stackCronScheduleMatches(matches[0], cron) {
				return false, nil
			}
		}
	}
	return true, nil
}

func migrationCreatesTargetStack(plan Plan, prepared PreparedMigration) bool {
	for _, instance := range prepared.Instances {
		instancePlan := planInstance(plan, instance.Source.UUID)
		if instancePlan != nil && instancePlan.Stack.CreateTarget {
			return true
		}
	}
	return false
}

func stackConfigurationStarted(state *MigrationState) bool {
	if state == nil {
		return false
	}
	for operation := range state.App.Operations {
		if strings.HasPrefix(operation, "stack_") {
			return true
		}
	}
	return false
}

func sortedPreparedStackServiceNames(configuration PreparedStackConfiguration) []string {
	names := make([]string, 0, len(configuration.Services))
	for name := range configuration.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stackServiceOptionsMatch(current []TargetStackServiceOption, desired []TargetStackServiceOptionInput) bool {
	if len(current) != len(desired) {
		return false
	}
	byVersion := map[string]TargetStackServiceOption{}
	for _, option := range current {
		byVersion[option.Version] = option
	}
	for _, option := range desired {
		actual, ok := byVersion[option.Version]
		if !ok || actual.Default != option.Default || actual.Disabled != option.Disabled {
			return false
		}
	}
	return true
}

func selectedStackServiceVersion(options []TargetStackServiceOptionInput) string {
	for _, option := range options {
		if option.Default {
			return option.Version
		}
	}
	return ""
}

func matchingStackEnvVars(items []TargetStackServiceEnvVar, name string, envType *string) []TargetStackServiceEnvVar {
	result := []TargetStackServiceEnvVar{}
	for _, item := range items {
		if item.Name == name && sameOptionalString(item.EnvType, envType) && item.Workload == "" && item.Container == "" {
			result = append(result, item)
		}
	}
	return result
}

func stackEnvVarMatches(actual TargetStackServiceEnvVar, desired PreparedStackEnvVar) bool {
	if desired.Secret {
		return actual.ValueSecretID != nil
	}
	return actual.ValueSecretID == nil && actual.Value != nil && *actual.Value == desired.Value
}

func replaceStackEnvVar(items []TargetStackServiceEnvVar, id int, replacement TargetStackServiceEnvVar) []TargetStackServiceEnvVar {
	for index := range items {
		if items[index].ID == id {
			items[index] = replacement
			return items
		}
	}
	return append(items, replacement)
}

func matchingStackCronSchedules(items []TargetStackServiceCronSchedule, name string, envType *string) []TargetStackServiceCronSchedule {
	result := []TargetStackServiceCronSchedule{}
	for _, item := range items {
		if item.Name == name && sameOptionalString(item.EnvType, envType) {
			result = append(result, item)
		}
	}
	return result
}

func stackCronScheduleMatches(actual TargetStackServiceCronSchedule, desired PreparedStackCronSchedule) bool {
	return actual.Title == desired.Title && actual.Crontab == desired.Crontab && actual.Command == desired.Command &&
		actual.Disabled == desired.Disabled && sameOptionalString(actual.Workload, desired.Workload)
}

func replaceStackCronSchedule(items []TargetStackServiceCronSchedule, id int, replacement TargetStackServiceCronSchedule) []TargetStackServiceCronSchedule {
	for index := range items {
		if items[index].ID == id {
			items[index] = replacement
			return items
		}
	}
	return append(items, replacement)
}

func stackEnvScopeLabel(envType *string) string {
	if envType == nil {
		return ""
	}
	return fmt.Sprintf(" (%s environments)", strings.ToLower(*envType))
}
