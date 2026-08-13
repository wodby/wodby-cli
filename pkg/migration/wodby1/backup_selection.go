package wodby1

import (
	"fmt"
	"strings"
)

// ResolveSourceBackups resolves user-facing backup selectors to immutable
// source instance and backup UUID pairs. The export is used only for resolving
// app/instance names; Wodby 1 validates that each selected backup belongs to
// that instance and is successful when it creates the selected export.
func ResolveSourceBackups(export Export, sourceKind string, values []string) (map[string]string, error) {
	result := map[string]string{}
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
			if existing, found := result[instance.UUID]; found && existing != backupUUID {
				return nil, fmt.Errorf("--source-backup selects conflicting backups for instance %q", instance.Name)
			}
			result[instance.UUID] = backupUUID
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
func PlanSourceBackups(plan Plan) (map[string]string, error) {
	result := map[string]string{}
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, item := range instance.Imports {
				if item.Action == "skip" || strings.TrimSpace(item.BackupUUID) == "" {
					continue
				}
				backupUUID := strings.TrimSpace(item.BackupUUID)
				if existing, found := result[instance.SourceUUID]; found && existing != backupUUID {
					return nil, fmt.Errorf("applied plan contains multiple backup snapshots for source instance %q", instance.SourceUUID)
				}
				result[instance.SourceUUID] = backupUUID
			}
		}
	}
	return result, nil
}

// ExportSourceBackups returns the backup snapshots present in an export. Every
// component of one instance must belong to the same Wodby 1 backup.
func ExportSourceBackups(export Export) (map[string]string, error) {
	result := map[string]string{}
	for _, app := range export.AppExports() {
		for _, instance := range app.Instances {
			for _, backup := range instance.Backups {
				backupUUID := strings.TrimSpace(backup.BackupUUID)
				if backupUUID == "" {
					return nil, fmt.Errorf("source instance %q backup component %q is missing its backup UUID", instance.Name, backup.Component)
				}
				if existing, found := result[instance.UUID]; found && existing != backupUUID {
					return nil, fmt.Errorf("source instance %q export contains multiple backup snapshots", instance.Name)
				}
				result[instance.UUID] = backupUUID
			}
		}
	}
	return result, nil
}
