package wodby1

import (
	"context"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

var migrationBackupDays = []string{
	"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY",
}

func (e *MigrationExecutor) ensureBackupPresets(
	ctx context.Context,
	state *MigrationState,
	prepared PreparedMigration,
	instances map[string]TargetAppInstance,
) error {
	for _, item := range prepared.Instances {
		destination := item.BackupDestination
		if destination == nil {
			continue
		}
		instance, ok := instances[item.Source.UUID]
		if !ok || instance.ID <= 0 {
			return errors.Errorf("target instance for backup presets is missing for source %q", item.Source.UUID)
		}
		services, err := e.target.ListAppServices(ctx, instance.ID)
		if err != nil {
			return errors.Wrap(err, "list target services before creating backup presets")
		}
		byName := map[string]TargetAppService{}
		for _, service := range services {
			byName[service.Name] = service
		}
		capabilities := targetBackupCapabilities(item)
		if len(capabilities) == 0 {
			return errors.Errorf("target instance %q has no enabled service with a backup capability", item.Source.Name)
		}
		e.reportProgress("Step: configure backup destination for target instance %q (ID %d).", item.Source.Name, instance.ID)
		for _, capability := range capabilities {
			service, ok := byName[capability.serviceName]
			if !ok || service.ID <= 0 || service.Disabled {
				return errors.Errorf("target backup service %q is missing or disabled", capability.serviceName)
			}
			if err := e.ensureBackupPreset(ctx, state, item.Source.UUID, service, capability.backupName, *destination); err != nil {
				return err
			}
		}
	}
	return nil
}

type preparedBackupCapability struct {
	serviceName string
	backupName  string
}

func targetBackupCapabilities(instance PreparedInstance) []preparedBackupCapability {
	result := []preparedBackupCapability{}
	seen := map[string]bool{}
	for _, inspection := range instance.StackServices {
		name := inspection.StackService.Name
		if !instance.EffectiveState[name] || inspection.ServiceRevision.Manifest == nil {
			continue
		}
		for _, backup := range inspection.ServiceRevision.Manifest.Backups {
			backupName := strings.TrimSpace(backup.Name)
			key := name + "\x00" + backupName
			if backupName == "" || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, preparedBackupCapability{serviceName: name, backupName: backupName})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].serviceName != result[j].serviceName {
			return result[i].serviceName < result[j].serviceName
		}
		return result[i].backupName < result[j].backupName
	})
	return result
}

func (e *MigrationExecutor) ensureBackupPreset(
	ctx context.Context,
	state *MigrationState,
	sourceID string,
	service TargetAppService,
	backupName string,
	destination PreparedBackupDestination,
) error {
	operation := operationKey("backup_preset", shortDigest(service.Name, backupName))
	desired := migrationBackupPresetInput(service.ID, backupName, destination)
	resource := state.Instances[sourceID]
	if resource == nil {
		return errors.Errorf("migration state is missing source instance %q", sourceID)
	}
	if current, exists := resource.Operations[operation]; exists && current.Status == MigrationOperationSucceeded {
		preset, err := e.target.GetBackupPreset(ctx, current.TargetID)
		if err != nil {
			return errors.Wrap(err, "read saved target backup preset")
		}
		if !backupPresetMatches(preset, desired) {
			return errors.Errorf("target backup preset for service %q and capability %q changed after migration", service.Name, backupName)
		}
		e.reportProgress("  Reusing backup preset ID %d for service %q (%s).", preset.ID, service.Name, backupName)
		return nil
	}
	existing, err := e.target.ListBackupPresets(ctx, service.ID, backupName)
	if err != nil {
		return err
	}
	if len(existing) > 1 {
		return &TargetAmbiguousMatchError{Resource: "backup preset", Name: service.Name + "/" + backupName, Count: len(existing)}
	}
	if len(existing) == 1 {
		if !backupPresetMatches(existing[0], desired) {
			return errors.Errorf("target service %q already has a different backup preset for capability %q", service.Name, backupName)
		}
		if _, exists := resource.Operations[operation]; exists {
			if err := promoteInstanceOperationForRecovery(state, sourceID, operation); err != nil {
				return err
			}
		} else if err := state.MarkInstanceOperationIntent(sourceID, operation); err != nil {
			return err
		}
		if err := state.MarkInstanceOperationSuccessWithIDs(sourceID, operation, existing[0].ID, 0); err != nil {
			return err
		}
		if err := SaveMigrationState(e.statePath, state); err != nil {
			return err
		}
		e.reportProgress("  Adopted matching backup preset ID %d for service %q (%s).", existing[0].ID, service.Name, backupName)
		return nil
	}
	retryID := "instance:" + sourceID + ":" + operation
	if current, exists := resource.Operations[operation]; exists && current.Status == MigrationOperationAmbiguous && !e.ambiguousRetryAuthorized(retryID) {
		return ambiguousRetryRequiredError("target backup preset creation is ambiguous", retryID)
	}
	if err := state.MarkInstanceOperationIntent(sourceID, operation); err != nil {
		return err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return err
	}
	e.reportProgress("  Creating %s backup preset on service %q using integration ID %d...", backupName, service.Name, destination.IntegrationID)
	created, err := e.target.CreateBackupPreset(ctx, desired)
	if err != nil {
		return e.recordInstanceMutationError(state, sourceID, operation, "backup preset creation", err)
	}
	if !backupPresetMatches(created, desired) {
		_ = state.MarkInstanceOperationAmbiguousWithIDs(sourceID, operation, created.ID, 0)
		_ = SaveMigrationState(e.statePath, state)
		return errors.Errorf("created backup preset for service %q does not match the migration input", service.Name)
	}
	if err := state.MarkInstanceOperationSuccessWithIDs(sourceID, operation, created.ID, 0); err != nil {
		return err
	}
	if err := SaveMigrationState(e.statePath, state); err != nil {
		return err
	}
	stateLabel := "enabled"
	if destination.Disabled {
		stateLabel = "disabled"
	} else if !destination.Auto {
		stateLabel = "manual"
	}
	e.reportProgress("  Backup preset created (ID %d, %s).", created.ID, stateLabel)
	return nil
}

func migrationBackupPresetInput(serviceID int, backupName string, destination PreparedBackupDestination) TargetCreateBackupPresetInput {
	auto := destination.Auto
	input := TargetCreateBackupPresetInput{
		AppServiceID: &serviceID, BackupName: &backupName,
		IntegrationID: destination.IntegrationID, Bucket: destination.Bucket,
		Disabled: destination.Disabled, Override: false, Auto: &auto,
	}
	if auto {
		duration := 3
		input.Duration = &duration
		input.TimeWindow = &TargetAutomationTimeWindowInput{
			Enabled: true, Start: "02:00", End: "05:00", TimeZone: destination.TimeZone,
			Days: append([]string(nil), migrationBackupDays...),
		}
	}
	return input
}

func backupPresetMatches(actual TargetBackupPreset, desired TargetCreateBackupPresetInput) bool {
	if actual.AppServiceID == nil || desired.AppServiceID == nil || *actual.AppServiceID != *desired.AppServiceID ||
		actual.BackupName == nil || desired.BackupName == nil || *actual.BackupName != *desired.BackupName ||
		actual.IntegrationID != desired.IntegrationID || actual.Bucket != desired.Bucket ||
		actual.Disabled != desired.Disabled || actual.Override != desired.Override || desired.Auto == nil || actual.Auto != *desired.Auto {
		return false
	}
	if !*desired.Auto {
		return actual.TimeWindow == nil && actual.Duration == nil
	}
	if actual.TimeWindow == nil || desired.TimeWindow == nil || actual.Duration == nil || desired.Duration == nil ||
		actual.TimeWindow.Start != desired.TimeWindow.Start || actual.TimeWindow.End != desired.TimeWindow.End ||
		actual.TimeWindow.TimeZone != desired.TimeWindow.TimeZone || *actual.Duration != *desired.Duration {
		return false
	}
	return sameStringSet(actual.TimeWindow.Days, desired.TimeWindow.Days)
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (e *MigrationExecutor) verifyBackupPresets(ctx context.Context, state *MigrationState, prepared PreparedMigration) error {
	for _, instance := range prepared.Instances {
		if instance.BackupDestination == nil {
			continue
		}
		resource := state.Instances[instance.Source.UUID]
		if resource == nil || resource.TargetID <= 0 {
			return errors.Errorf("migration state is missing target instance for backup verification")
		}
		services, err := e.target.ListAppServices(ctx, resource.TargetID)
		if err != nil {
			return errors.Wrap(err, "list target services for backup preset verification")
		}
		byName := map[string]TargetAppService{}
		for _, service := range services {
			byName[service.Name] = service
		}
		for _, capability := range targetBackupCapabilities(instance) {
			service, ok := byName[capability.serviceName]
			if !ok {
				return errors.Errorf("backup service %q is missing during verification", capability.serviceName)
			}
			operation := operationKey("backup_preset", shortDigest(service.Name, capability.backupName))
			op := resource.Operations[operation]
			if op.Status != MigrationOperationSucceeded || op.TargetID <= 0 {
				return errors.Errorf("backup preset for service %q (%s) was not completed by apply", service.Name, capability.backupName)
			}
			actual, err := e.target.GetBackupPreset(ctx, op.TargetID)
			if err != nil {
				return err
			}
			desired := migrationBackupPresetInput(service.ID, capability.backupName, *instance.BackupDestination)
			if !backupPresetMatches(actual, desired) {
				return errors.Errorf("backup preset for service %q (%s) no longer matches the migration", service.Name, capability.backupName)
			}
		}
	}
	return nil
}
