package wodby1

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type PreparedDataImport struct {
	SourceInstanceUUID string
	Backup             Backup
	Destination        PreparedImport
}

// PrepareDataSync enforces the customer cutover contract before returning any
// backup transfer URLs to the mutation loop: Wodby 1 must be write-frozen, every
// approved component must have one fresh successful backup, and refreshed
// backups may change snapshot identity but not target component mappings.
func PrepareDataSync(
	export Export,
	prepared PreparedMigration,
	now time.Time,
	maxBackupAge time.Duration,
) ([]PreparedDataImport, error) {
	return prepareDataSync(export, prepared, now, maxBackupAge, true)
}

func prepareDataSync(
	export Export,
	prepared PreparedMigration,
	now time.Time,
	maxBackupAge time.Duration,
	requireFresh bool,
) ([]PreparedDataImport, error) {
	if maxBackupAge <= 0 {
		return nil, errors.New("maximum backup age must be positive")
	}
	if err := export.ValidateSource("app", prepared.App.App.UUID); err != nil {
		return nil, err
	}
	currentInstances := map[string]Instance{}
	for _, app := range export.AppExports() {
		for _, instance := range app.Instances {
			currentInstances[instance.UUID] = instance
		}
	}

	result := []PreparedDataImport{}
	for _, target := range prepared.Instances {
		current, found := currentInstances[target.Source.UUID]
		if !found {
			return nil, errors.Errorf("source instance %q disappeared before data synchronization", target.Source.UUID)
		}
		if !sourceMaintenanceMode(current.Properties) {
			return nil, errors.Errorf(
				"source instance %q is not in maintenance mode; freeze writes before sync-data",
				current.Name,
			)
		}
		currentByComponent := map[string][]Backup{}
		backupUUID := ""
		var backupCreated int64
		var backupUpdated int64
		for _, backup := range current.Backups {
			component := strings.ToLower(strings.TrimSpace(backup.Component))
			currentByComponent[component] = append(currentByComponent[component], backup)
			if backupUUID == "" {
				backupUUID = backup.BackupUUID
				backupCreated = backup.BackupCreated
				backupUpdated = backup.BackupUpdated
			} else if backup.BackupUUID != backupUUID ||
				backup.BackupCreated != backupCreated ||
				backup.BackupUpdated != backupUpdated {
				return nil, errors.Errorf(
					"source instance %q export combines files from different backup snapshots",
					current.Name,
				)
			}
		}
		if len(target.ImportByComponent) == 0 {
			return nil, errors.Errorf("source instance %q has no approved data component mapping", current.Name)
		}
		for component, destination := range target.ImportByComponent {
			backups := currentByComponent[component]
			if len(backups) != 1 {
				return nil, errors.Errorf(
					"source instance %q component %q has %d fresh backup files; exactly one is required",
					current.Name,
					component,
					len(backups),
				)
			}
			backup := backups[0]
			if err := validateBackup(backup, now, maxBackupAge, requireFresh); err != nil {
				return nil, errors.Wrapf(err, "source instance %q component %q", current.Name, component)
			}
			if err := validateBackupAfterFreeze(backup, current.Updated); err != nil {
				return nil, errors.Wrapf(err, "source instance %q component %q", current.Name, component)
			}
			destination.Source = backup
			result = append(result, PreparedDataImport{
				SourceInstanceUUID: current.UUID,
				Backup:             backup,
				Destination:        destination,
			})
		}
		for component := range currentByComponent {
			if _, approved := target.ImportByComponent[component]; !approved {
				return nil, errors.Errorf(
					"source instance %q backup component %q was not present in the approved plan",
					current.Name,
					component,
				)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].SourceInstanceUUID + "\x00" + result[i].Backup.Component
		right := result[j].SourceInstanceUUID + "\x00" + result[j].Backup.Component
		return left < right
	})
	return result, nil
}

func validateBackupAfterFreeze(backup Backup, instanceUpdated int64) error {
	if instanceUpdated <= 0 {
		return errors.New("source instance update time is missing; cannot prove the backup was created after write freeze")
	}
	created := backup.BackupCreated
	if created <= 0 {
		created = backup.Created
	}
	if created < instanceUpdated {
		return errors.New("backup predates the latest source instance change; create a new backup after enabling maintenance mode")
	}
	return nil
}

func validateFreshBackup(backup Backup, now time.Time, maxAge time.Duration) error {
	return validateBackup(backup, now, maxAge, true)
}

func validateBackup(backup Backup, now time.Time, maxAge time.Duration, requireFresh bool) error {
	if strings.ToLower(strings.TrimSpace(backup.Status)) != "ok" {
		return fmt.Errorf("backup status %q is not usable", backup.Status)
	}
	completed := backup.BackupUpdated
	if completed <= 0 {
		completed = backup.Updated
	}
	if completed <= 0 {
		// Compatibility with exports created before backup_updated was added.
		completed = backup.BackupCreated
	}
	if completed <= 0 {
		completed = backup.Created
	}
	if completed <= 0 {
		return errors.New("backup completion time is missing")
	}
	completedAt := time.Unix(completed, 0)
	if completedAt.After(now.Add(5 * time.Minute)) {
		return errors.New("backup completion time is unexpectedly in the future")
	}
	if requireFresh && now.Sub(completedAt) > maxAge {
		return fmt.Errorf("backup completion is older than %s", maxAge)
	}
	parsed, err := url.Parse(strings.TrimSpace(backup.URL))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("backup URL must be an absolute HTTPS URL without embedded credentials")
	}
	if backup.Size < 0 {
		return errors.New("backup size cannot be negative")
	}
	return nil
}

func sourceMaintenanceMode(properties map[string]interface{}) bool {
	value, found := properties["maintenance_mode"]
	if !found {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
}
