package wodby1

import (
	"context"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

func (e *MigrationExecutor) ensureTargetIntegrations(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	prepared PreparedMigration,
) (PreparedMigration, error) {
	if len(prepared.Integrations) == 0 {
		return prepared, nil
	}
	sort.SliceStable(prepared.Integrations, func(i, j int) bool { return prepared.Integrations[i].Key < prepared.Integrations[j].Key })
	resolved := map[string]int{}
	e.reportProgress("Step: resolve reusable target integrations without persisting their credentials.")
	for index := range prepared.Integrations {
		item := &prepared.Integrations[index]
		if item.VariableProvider != nil {
			provider, err := e.ensureVariableProvider(ctx, state, plan, item.Key, *item.VariableProvider)
			if err != nil {
				return PreparedMigration{}, err
			}
			item.ProviderID = provider.ID
			item.ProviderRevID = provider.RevID
			item.ProviderName = provider.Name
		}
		if item.ProviderID <= 0 || item.ProviderRevID <= 0 {
			return PreparedMigration{}, errors.Errorf("target provider for integration %q was not resolved", item.Key)
		}
		provider, err := e.target.GetProviderByName(ctx, item.ProviderName)
		if err != nil {
			return PreparedMigration{}, err
		}
		if provider.ID != item.ProviderID || provider.RevID != item.ProviderRevID {
			return PreparedMigration{}, errors.Errorf("target provider %q changed after the migration plan was reviewed", item.ProviderName)
		}
		operation := operationKey("integration_resolve", item.Key)
		if current, exists := state.App.Operations[operation]; exists && current.Status == MigrationOperationSucceeded {
			if current.TargetID <= 0 {
				return PreparedMigration{}, errors.Errorf("saved target integration %q has no target ID", item.Key)
			}
			existing, err := e.target.GetIntegration(ctx, current.TargetID)
			if err != nil {
				return PreparedMigration{}, errors.Wrapf(err, "read saved target integration %q", item.Key)
			}
			if existing.OrgID != plan.Target.OrgID || existing.ProviderRevID != item.ProviderRevID || !sameOptionalString(existing.Scope, item.Scope) {
				return PreparedMigration{}, errors.Errorf("saved target integration %q no longer matches the reviewed organization and scope", item.Key)
			}
			item.TargetID = existing.ID
			resolved[item.Key] = existing.ID
			e.reportProgress("  Reusing resolved %s integration ID %d from migration state.", item.Kind, existing.ID)
			continue
		}
		retryID := "app:" + state.Source.ID + ":" + operation
		if current, exists := state.App.Operations[operation]; exists && current.Status == MigrationOperationAmbiguous && !e.ambiguousRetryAuthorized(retryID) {
			return PreparedMigration{}, ambiguousRetryRequiredError("target integration resolution is ambiguous", retryID)
		}
		if err := state.MarkAppOperationIntent(operation); err != nil {
			return PreparedMigration{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return PreparedMigration{}, err
		}
		var projectID *int
		if plan.Target.ProjectID > 0 {
			projectID = &plan.Target.ProjectID
		}
		e.reportProgress("  Resolving %s integration %q through provider %q...", item.Kind, item.Title, item.ProviderName)
		result, err := e.target.ResolveIntegration(ctx, TargetResolveIntegrationInput{
			OrgID: plan.Target.OrgID, ProviderID: item.ProviderID, ProjectID: projectID,
			Name: item.Name, Title: item.Title, Kinds: []string{item.Kind},
			FieldsInput: item.Fields, Scope: item.Scope,
		})
		if err != nil {
			return PreparedMigration{}, e.recordAppMutationError(state, operation, "target integration resolution", retryID, err)
		}
		if result.Integration.OrgID != plan.Target.OrgID || result.Integration.ProviderRevID != item.ProviderRevID || !sameOptionalString(result.Integration.Scope, item.Scope) {
			_ = state.MarkAppOperationAmbiguousWithIDs(operation, result.Integration.ID, 0)
			_ = SaveMigrationState(e.statePath, state)
			return PreparedMigration{}, errors.Errorf("resolved target integration %q does not match the reviewed organization and scope", item.Key)
		}
		// Record whether this migration created the integration or reused one
		// that already existed, so rollback never deletes a shared integration
		// it merely adopted.
		if result.Created {
			if err := state.MarkAppOperationCreated(operation, result.Integration.ID, 0); err != nil {
				return PreparedMigration{}, err
			}
		} else if err := state.MarkAppOperationSuccessWithIDs(operation, result.Integration.ID, 0); err != nil {
			return PreparedMigration{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return PreparedMigration{}, err
		}
		item.TargetID = result.Integration.ID
		resolved[item.Key] = result.Integration.ID
		verb := "reused"
		if result.Created {
			verb = "created"
		}
		e.reportProgress("  Target %s integration %s (ID %d).", item.Kind, verb, result.Integration.ID)
	}
	if err := bindPreparedIntegrationIDs(&prepared, resolved); err != nil {
		return PreparedMigration{}, err
	}
	return prepared, nil
}

func (e *MigrationExecutor) ensureVariableProvider(
	ctx context.Context,
	state *MigrationState,
	plan Plan,
	key string,
	desired PreparedVariableProvider,
) (TargetProvider, error) {
	operation := operationKey("variable_provider", key)
	validate := func(provider TargetProvider) error {
		if provider.Name != desired.Name || provider.OrgID != plan.Target.OrgID || provider.Public || provider.RevID <= 0 {
			return errors.Errorf("target variable provider %q does not match the reviewed organization", desired.Name)
		}
		return nil
	}
	if current, exists := state.App.Operations[operation]; exists && current.Status == MigrationOperationSucceeded {
		provider, err := e.target.GetProviderByName(ctx, desired.Name)
		if err != nil {
			return TargetProvider{}, errors.Wrap(err, "read saved target variable provider")
		}
		if current.TargetID != provider.ID {
			return TargetProvider{}, errors.Errorf("saved target variable provider %q changed identity", desired.Name)
		}
		if err := validate(provider); err != nil {
			return TargetProvider{}, err
		}
		e.reportProgress("  Reusing custom variable provider %q (ID %d) from migration state.", provider.Name, provider.ID)
		return provider, nil
	}
	provider, err := e.target.GetProviderByName(ctx, desired.Name)
	if err == nil {
		if err := validate(provider); err != nil {
			return TargetProvider{}, err
		}
		if _, exists := state.App.Operations[operation]; exists {
			if err := promoteAppOperationForRecovery(state, operation); err != nil {
				return TargetProvider{}, err
			}
		} else if err := state.MarkAppOperationIntent(operation); err != nil {
			return TargetProvider{}, err
		}
		if err := state.MarkAppOperationSuccessWithIDs(operation, provider.ID, provider.RevID); err != nil {
			return TargetProvider{}, err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return TargetProvider{}, err
		}
		e.reportProgress("  Reusing custom variable provider %q (ID %d) created by this server migration.", provider.Name, provider.ID)
		return provider, nil
	}
	if !isTargetNotFound(err) {
		return TargetProvider{}, err
	}
	retryID := "app:" + state.Source.ID + ":" + operation
	if current, exists := state.App.Operations[operation]; exists && current.Status == MigrationOperationAmbiguous && !e.ambiguousRetryAuthorized(retryID) {
		return TargetProvider{}, ambiguousRetryRequiredError("target variable provider creation is ambiguous", retryID)
	}
	if err := state.MarkAppOperationIntent(operation); err != nil {
		return TargetProvider{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetProvider{}, err
	}
	var projectID *int
	if plan.Target.ProjectID > 0 {
		projectID = &plan.Target.ProjectID
	}
	e.reportProgress("  Creating custom variable provider %q with %d shared field(s)...", desired.Name, len(desired.Fields))
	provider, err = e.target.CreateVariableProvider(ctx, TargetCreateVariableProviderInput{
		OrgID: plan.Target.OrgID, ProjectID: projectID,
		Name: desired.Name, Title: desired.Title, Fields: desired.Fields,
	})
	if err != nil {
		return TargetProvider{}, e.recordAppMutationError(state, operation, "target variable provider creation", retryID, err)
	}
	if err := validate(provider); err != nil {
		_ = state.MarkAppOperationAmbiguousWithIDs(operation, provider.ID, provider.RevID)
		_ = SaveMigrationState(e.statePath, state)
		return TargetProvider{}, err
	}
	if err := state.MarkAppOperationSuccessWithIDs(operation, provider.ID, provider.RevID); err != nil {
		return TargetProvider{}, err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return TargetProvider{}, err
	}
	e.reportProgress("  Custom variable provider created (ID %d).", provider.ID)
	return provider, nil
}

func bindPreparedIntegrationIDs(prepared *PreparedMigration, resolved map[string]int) error {
	if prepared == nil {
		return errors.New("prepared migration is required")
	}
	for serviceName, configuration := range prepared.StackConfiguration.Services {
		for index := range configuration.Integrations {
			link := &configuration.Integrations[index]
			id := resolved[link.IntegrationKey]
			if id <= 0 {
				return errors.Errorf("stack service %q integration %q was not resolved", serviceName, link.IntegrationKey)
			}
			link.IntegrationID = id
		}
		prepared.StackConfiguration.Services[serviceName] = configuration
	}
	for index := range prepared.Instances {
		if strings.TrimSpace(prepared.Instances[index].CIIntegrationKey) != "" {
			id := resolved[prepared.Instances[index].CIIntegrationKey]
			if id <= 0 {
				return errors.Errorf("CI integration %q was not resolved", prepared.Instances[index].CIIntegrationKey)
			}
			prepared.Instances[index].CIIntegrationID = id
		}
		destination := prepared.Instances[index].BackupDestination
		if destination == nil || strings.TrimSpace(destination.IntegrationKey) == "" {
			continue
		}
		id := resolved[destination.IntegrationKey]
		if id <= 0 {
			return errors.Errorf("backup integration %q was not resolved", destination.IntegrationKey)
		}
		destination.IntegrationID = id
	}
	return nil
}

func (e *MigrationExecutor) verifyTargetIntegrations(ctx context.Context, state *MigrationState, plan Plan, prepared PreparedMigration) error {
	for _, desired := range prepared.Integrations {
		operation := state.App.Operations[operationKey("integration_resolve", desired.Key)]
		if operation.Status != MigrationOperationSucceeded || operation.TargetID <= 0 {
			return errors.Errorf("target integration %q was not completed by apply", desired.Key)
		}
		actual, err := e.target.GetIntegration(ctx, operation.TargetID)
		if err != nil {
			return err
		}
		if actual.OrgID != plan.Target.OrgID || actual.ProviderRevID != desired.ProviderRevID || !sameOptionalString(actual.Scope, desired.Scope) {
			return errors.Errorf("target integration %q no longer matches the migration target", desired.Key)
		}
	}
	return nil
}
