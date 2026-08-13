package wodby1

import (
	"fmt"
	"strings"
)

const allBackupComponents = "*"

// SourceBackupSelection pins backup identities independently per data
// component. The internal "*" component is used only for an explicit
// user-selected whole backup before the source API returns its components.
type SourceBackupSelection map[string]map[string]string

// ResolveSourceBackups resolves user-facing backup selectors to immutable
// source instance and backup UUID pairs. The export is used only for resolving
// app/instance names; Wodby 1 validates that each selected backup belongs to
// that instance and is successful when it creates the selected export.
func ResolveSourceBackups(export Export, sourceKind string, values []string) (SourceBackupSelection, error) {
	result := SourceBackupSelection{}
	if len(values) == 0 {
		return result, nil
	}
	apps := export.AppExports()
	for _, raw := range values {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			instanceSelector, backupUUID, scoped := strings.Cut(value, "=")
			if sourceKind == "instance" && !scoped {
				if len(apps) != 1 || len(apps[0].Instances) != 1 {
					return nil, fmt.Errorf("source instance export does not contain exactly one instance")
				}
				instanceSelector = apps[0].Instances[0].UUID
				backupUUID = value
			} else if !scoped {
				return nil, fmt.Errorf("--source-backup value %q must use INSTANCE=BACKUP_UUID for an app or APP/INSTANCE=BACKUP_UUID for a server", value)
			}
			instanceSelector = strings.TrimSpace(instanceSelector)
			backupUUID = strings.TrimSpace(backupUUID)
			if instanceSelector == "" || backupUUID == "" {
				return nil, fmt.Errorf("--source-backup value %q contains an empty instance or backup selector", value)
			}
			if err := validateSourceUUID(backupUUID); err != nil {
				return nil, fmt.Errorf("invalid backup UUID in --source-backup value %q: %w", value, err)
			}
			_, instance, err := resolveSelectedInstance(apps, sourceKind, instanceSelector)
			if err != nil {
				return nil, fmt.Errorf("resolve --source-backup instance %q: %w", instanceSelector, err)
			}
			if result[instance.UUID] == nil {
				result[instance.UUID] = map[string]string{}
			}
			if existing, found := result[instance.UUID][allBackupComponents]; found && existing != backupUUID {
				return nil, fmt.Errorf("--source-backup selects conflicting backups for instance %q", instance.Name)
			}
			result[instance.UUID][allBackupComponents] = backupUUID
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("--source-backup did not contain a backup selector")
	}
	return result, nil
}

// PlanSourceBackups returns the immutable backup selection persisted by an
// applied plan. It is used on resume so refreshing protected download URLs can
// never silently switch to a newer backup.
func PlanSourceBackups(plan Plan) (SourceBackupSelection, error) {
	result := SourceBackupSelection{}
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, item := range instance.Imports {
				if item.Action == "skip" || strings.TrimSpace(item.BackupUUID) == "" {
					continue
				}
				component := normalizeBackupComponent(item.Component)
				if component == "" {
					return nil, fmt.Errorf("applied plan contains an empty backup component for source instance %q", instance.SourceUUID)
				}
				if result[instance.SourceUUID] == nil {
					result[instance.SourceUUID] = map[string]string{}
				}
				backupUUID := strings.TrimSpace(item.BackupUUID)
				if existing, found := result[instance.SourceUUID][component]; found && existing != backupUUID {
					return nil, fmt.Errorf("applied plan contains multiple backup files for source instance %q component %q", instance.SourceUUID, component)
				}
				result[instance.SourceUUID][component] = backupUUID
			}
		}
	}
	return result, nil
}

// ExportSourceBackups returns the component-level backup identities present in
// an export. Different components may intentionally come from different
// successful Wodby 1 backups.
func ExportSourceBackups(export Export) (SourceBackupSelection, error) {
	result := SourceBackupSelection{}
	for _, app := range export.AppExports() {
		for _, instance := range app.Instances {
			for _, backup := range instance.Backups {
				backupUUID := strings.TrimSpace(backup.BackupUUID)
				if backupUUID == "" {
					return nil, fmt.Errorf("source instance %q backup component %q is missing its backup UUID", instance.Name, backup.Component)
				}
				component := normalizeBackupComponent(backup.Component)
				if component == "" {
					return nil, fmt.Errorf("source instance %q backup is missing its component", instance.Name)
				}
				if result[instance.UUID] == nil {
					result[instance.UUID] = map[string]string{}
				}
				if existing, found := result[instance.UUID][component]; found && existing != backupUUID {
					return nil, fmt.Errorf("source instance %q export contains multiple backup files for component %q", instance.Name, component)
				}
				result[instance.UUID][component] = backupUUID
			}
		}
	}
	return result, nil
}

func normalizeBackupComponent(component string) string {
	return strings.ToLower(strings.TrimSpace(component))
}
