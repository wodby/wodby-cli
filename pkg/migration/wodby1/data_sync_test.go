package wodby1

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidateBackupAfterFreeze(t *testing.T) {
	t.Run("rejects backup from before maintenance update", func(t *testing.T) {
		err := validateBackupAfterFreeze(Backup{BackupCreated: 99}, 100)
		if err == nil || !strings.Contains(err.Error(), "predates") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("accepts backup started at freeze timestamp", func(t *testing.T) {
		if err := validateBackupAfterFreeze(Backup{BackupCreated: 100}, 100); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("accepts backup started after freeze", func(t *testing.T) {
		if err := validateBackupAfterFreeze(Backup{BackupCreated: 101}, 100); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects later source mutation", func(t *testing.T) {
		err := validateBackupAfterFreeze(Backup{BackupCreated: 100}, 101)
		if err == nil || !strings.Contains(err.Error(), "predates") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidateFreshBackupUsesCompletionTimeForMaximumAge(t *testing.T) {
	now := time.Unix(10_000, 0)
	backup := Backup{
		Status:        "ok",
		URL:           "https://backups.example.test/file?expires=11000&signature=value",
		BackupCreated: 1_000,
		BackupUpdated: 9_900,
	}
	if err := validateFreshBackup(backup, now, time.Hour); err != nil {
		t.Fatalf("fresh completed backup rejected because it started long ago: %v", err)
	}

	backup.BackupUpdated = 6_000
	err := validateFreshBackup(backup, now, time.Hour)
	if err == nil || !strings.Contains(err.Error(), "completion is older") {
		t.Fatalf("error = %v, want stale completion", err)
	}
}

func TestValidateFreshBackupFallsBackToFileUpdateForLegacyExport(t *testing.T) {
	now := time.Unix(10_000, 0)
	backup := Backup{
		Status:        "ok",
		URL:           "https://backups.example.test/file",
		Updated:       9_900,
		BackupCreated: 1_000,
	}
	if err := validateFreshBackup(backup, now, time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshDataImportObtainsNewURLForSameBoundSnapshot(t *testing.T) {
	now := time.Unix(10_000, 0)
	base, prepared := refreshDataImportFixture()
	imports, err := PrepareDataSync(base, prepared, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	configDigest, err := base.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	backupDigest, err := base.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	refreshed := cloneExportForTest(t, base)
	refreshed.Apps[0].Instances[0].Backups[0].URL =
		"https://backups.example.test/file?expires=12000&signature=new"
	refreshes := 0
	executor := &MigrationExecutor{
		now:          func() time.Time { return now.Add(2 * time.Hour) },
		maxBackupAge: time.Hour,
		refreshSource: func(context.Context) (Export, error) {
			refreshes++
			return refreshed, nil
		},
	}
	got, err := executor.refreshDataImport(
		context.Background(),
		imports[0],
		prepared,
		Plan{Source: PlanSource{ConfigDigest: configDigest}},
		backupDigest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 || !strings.Contains(got.Backup.URL, "signature=new") {
		t.Fatalf("refreshes=%d import=%#v", refreshes, got)
	}
}

func TestBoundBackupStillValidatesSafetyMetadata(t *testing.T) {
	now := time.Unix(20_000, 0)
	for _, test := range []struct {
		name      string
		mutate    func(*Backup)
		wantError string
	}{
		{
			name: "status",
			mutate: func(backup *Backup) {
				backup.Status = "failed"
			},
			wantError: "status",
		},
		{
			name: "URL",
			mutate: func(backup *Backup) {
				backup.URL = "http://backups.example.test/file"
			},
			wantError: "absolute HTTPS URL",
		},
		{
			name: "size",
			mutate: func(backup *Backup) {
				backup.Size = -1
			},
			wantError: "size cannot be negative",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			export, prepared := refreshDataImportFixture()
			test.mutate(&export.Apps[0].Instances[0].Backups[0])
			_, err := prepareDataSync(export, prepared, now, time.Hour, false)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestRefreshDataImportRejectsReplacementSnapshot(t *testing.T) {
	now := time.Unix(10_000, 0)
	base, prepared := refreshDataImportFixture()
	imports, err := PrepareDataSync(base, prepared, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	configDigest, err := base.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	backupDigest, err := base.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	replacement := cloneExportForTest(t, base)
	replacement.Apps[0].Instances[0].Backups[0].UUID = "file-2"
	replacement.Apps[0].Instances[0].Backups[0].BackupUUID = "snapshot-2"
	executor := &MigrationExecutor{
		now:          func() time.Time { return now },
		maxBackupAge: time.Hour,
		refreshSource: func(context.Context) (Export, error) {
			return replacement, nil
		},
	}
	_, err = executor.refreshDataImport(
		context.Background(),
		imports[0],
		prepared,
		Plan{Source: PlanSource{ConfigDigest: configDigest}},
		backupDigest,
	)
	if err == nil || !strings.Contains(err.Error(), "snapshot changed") {
		t.Fatalf("error = %v", err)
	}
}

func refreshDataImportFixture() (Export, PreparedMigration) {
	backup := Backup{
		UUID: "file-1", BackupUUID: "snapshot-1", Component: "database",
		URL:           "https://backups.example.test/file?expires=11000&signature=old",
		Status:        "ok",
		BackupCreated: 9_900,
		BackupUpdated: 9_950,
	}
	source := Instance{
		UUID:       "instance-1",
		Name:       "prod",
		Type:       "prod",
		Stack:      Stack{Name: "drupal"},
		Updated:    9_800,
		Properties: map[string]interface{}{"maintenance_mode": true},
		Backups:    []Backup{backup},
	}
	destination := PreparedImport{
		Source:      backup,
		ServiceName: "database",
		ImportName:  "database",
	}
	export := Export{
		Schema:          ExportSchemaV2,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App:       App{UUID: "app-1", Name: "demo"},
			Instances: []Instance{source},
		}},
	}
	prepared := PreparedMigration{
		App: export.Apps[0],
		Instances: []PreparedInstance{{
			Source:            source,
			ImportByComponent: map[string]PreparedImport{"database": destination},
		}},
	}
	return export, prepared
}
