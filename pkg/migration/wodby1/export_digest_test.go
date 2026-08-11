package wodby1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExportDigestsSeparateConfigurationAndBackupSnapshots(t *testing.T) {
	base := Export{
		Schema:          ExportSchemaV2,
		GeneratedAt:     100,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok", Updated: 10},
			Instances: []Instance{{
				UUID:       "instance-1",
				Name:       "prod",
				Type:       "prod",
				Status:     "ok",
				Updated:    20,
				Stack:      Stack{Name: "drupal10"},
				Properties: map[string]interface{}{"maintenance_mode": false},
				Services: []Service{{
					Name:    "php",
					Enabled: true,
					EnvVars: []EnvVar{{
						Name: "APP_SECRET", Value: "first", Secret: true, Enabled: true, Origin: "custom",
					}},
				}},
				Backups: []Backup{{
					UUID: "file-1", BackupUUID: "backup-1", Component: "db",
					Status: "ok", Size: 42, BackupCreated: 30,
					URL:         "https://backups.example.test/file?signature=one",
					MirroredURL: "https://mirror.example.test/file?signature=one",
				}},
			}},
		}},
	}

	rotatedURL := cloneExportForTest(t, base)
	rotatedURL.GeneratedAt = 200
	rotatedURL.Apps[0].Instances[0].Backups[0].URL = "https://backups.example.test/file?signature=two"
	rotatedURL.Apps[0].Instances[0].Backups[0].MirroredURL = "https://mirror.example.test/file?signature=two"
	assertSameExportDigest(t, base, rotatedURL)
	assertSameConfigDigest(t, base, rotatedURL)
	assertSameBackupDigest(t, base, rotatedURL)

	writeFrozen := cloneExportForTest(t, base)
	writeFrozen.Apps[0].App.Updated = 11
	writeFrozen.Apps[0].Instances[0].Updated = 21
	writeFrozen.Apps[0].Instances[0].Properties["maintenance_mode"] = true
	assertSameConfigDigest(t, base, writeFrozen)

	newBackup := cloneExportForTest(t, writeFrozen)
	newBackup.Apps[0].Instances[0].Backups[0].UUID = "file-2"
	newBackup.Apps[0].Instances[0].Backups[0].BackupUUID = "backup-2"
	newBackup.Apps[0].Instances[0].Backups[0].BackupCreated = 40
	assertDifferentBackupDigest(t, base, newBackup)
	assertSameConfigDigest(t, base, newBackup)

	changedSecret := cloneExportForTest(t, base)
	changedSecret.Apps[0].Instances[0].Services[0].EnvVars[0].Value = "second"
	assertSameExportDigest(t, base, changedSecret)
	assertSameConfigDigest(t, base, changedSecret)

	firstMAC, err := base.AuthenticatedConfigDigest(testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	secondMAC, err := changedSecret.AuthenticatedConfigDigest(testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	if firstMAC == secondMAC {
		t.Fatal("authenticated configuration digest did not detect a secret value change")
	}

	changedPublicConfig := cloneExportForTest(t, base)
	changedPublicConfig.Apps[0].Instances[0].Stack.Name = "drupal11"
	assertDifferentExportDigest(t, base, changedPublicConfig)
	firstPublicConfig, err := base.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	secondPublicConfig, err := changedPublicConfig.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	if firstPublicConfig == secondPublicConfig {
		t.Fatal("public configuration digest did not detect a stack change")
	}
}

func TestPublicDigestsExcludeLowEntropyAndFreeFormSecrets(t *testing.T) {
	base := Export{
		Schema:          ExportSchemaV2,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{
				UUID: "app-1",
				Name: "demo",
				Repository: &Repository{
					UUID: "repo-1",
					URL:  "https://git.example.test/repository?token=1234",
				},
			},
			Instances: []Instance{{
				UUID:      "instance-1",
				Name:      "prod",
				Type:      "prod",
				Stack:     Stack{Name: "drupal10"},
				BasicAuth: &BasicAuth{Enabled: true, Login: "user", Password: "1234", Secret: true},
				Services: []Service{{
					Name:          "php",
					Configuration: map[string]interface{}{"token": "1234"},
					EnvVars: []EnvVar{{
						Name: "APP_SECRET", Value: "1234", Enabled: true, Protected: true,
					}},
					CronJobs: []CronJob{{
						Crontab: "0 * * * *",
						Command: "curl https://example.test/?token=1234",
						Source:  "0 * * * * curl https://example.test/?token=1234",
						Enabled: true,
					}},
				}},
			}},
		}},
		Issues: []ExportIssue{{
			Code: "cron.invalid",
			Details: map[string]interface{}{
				"line": "curl https://example.test/?token=1234",
			},
		}},
	}
	rotated := cloneExportForTest(t, base)
	rotated.Apps[0].App.Repository.URL = "https://git.example.test/repository?token=5678"
	rotated.Apps[0].Instances[0].BasicAuth.Password = "5678"
	rotated.Apps[0].Instances[0].Services[0].Configuration["token"] = "5678"
	rotated.Apps[0].Instances[0].Services[0].EnvVars[0].Value = "5678"
	rotated.Apps[0].Instances[0].Services[0].CronJobs[0].Command = "curl https://example.test/?token=5678"
	rotated.Apps[0].Instances[0].Services[0].CronJobs[0].Source = "0 * * * * curl https://example.test/?token=5678"
	rotated.Issues[0].Details["line"] = "curl https://example.test/?token=5678"

	assertSameExportDigest(t, base, rotated)
	assertSameConfigDigest(t, base, rotated)
	firstMAC, err := base.AuthenticatedConfigDigest(strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	secondMAC, err := rotated.AuthenticatedConfigDigest(strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	if firstMAC == secondMAC {
		t.Fatal("authenticated configuration digest did not bind protected/free-form values")
	}
}

func TestPlanHashSurvivesWriteFreezeAndFreshBackupButBindsConfiguration(t *testing.T) {
	base := Export{
		Schema:          ExportSchemaV2,
		GeneratedAt:     100,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok", Updated: 10},
			Instances: []Instance{{
				UUID:       "instance-1",
				Name:       "prod",
				Type:       "prod",
				Status:     "ok",
				Updated:    20,
				Stack:      Stack{Name: "drupal10"},
				Properties: map[string]interface{}{"maintenance_mode": false},
				Services: []Service{{
					Name:    "php",
					Enabled: true,
					EnvVars: []EnvVar{{
						Name: "APP_VALUE", Value: "first", Enabled: true, Origin: "custom",
					}},
				}},
				Backups: []Backup{{
					UUID: "file-1", BackupUUID: "backup-1", Component: "db",
					Status: "ok", Size: 42, BackupCreated: 30,
					URL: "https://backups.example.test/file?signature=one",
				}},
			}},
		}},
	}
	options := PlanOptions{SourceKind: "app", SourceID: "app-1"}
	configMAC, err := base.AuthenticatedConfigDigest(testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	base.ConfigMAC = configMAC
	first, err := BuildPlan(base, options)
	if err != nil {
		t.Fatal(err)
	}

	fresh := cloneExportForTest(t, base)
	fresh.GeneratedAt = 200
	fresh.Apps[0].App.Updated = 11
	fresh.Apps[0].Instances[0].Updated = 21
	fresh.Apps[0].Instances[0].Properties["maintenance_mode"] = true
	fresh.Apps[0].Instances[0].Backups[0].UUID = "file-2"
	fresh.Apps[0].Instances[0].Backups[0].BackupUUID = "backup-2"
	fresh.Apps[0].Instances[0].Backups[0].BackupCreated = 40
	fresh.Apps[0].Instances[0].Backups[0].URL = "https://backups.example.test/file?signature=two"
	fresh.ConfigMAC, err = fresh.AuthenticatedConfigDigest(testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(fresh, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash != second.PlanHash {
		t.Fatalf("write freeze or refreshed backup changed plan hash: %q != %q", first.PlanHash, second.PlanHash)
	}

	changed := cloneExportForTest(t, fresh)
	changed.Apps[0].Instances[0].Services[0].EnvVars[0].Value = "second"
	changed.ConfigMAC, err = changed.AuthenticatedConfigDigest(testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	third, err := BuildPlan(changed, options)
	if err != nil {
		t.Fatal(err)
	}
	if third.PlanHash == second.PlanHash {
		t.Fatal("source configuration change did not change the migration plan hash")
	}
}

func cloneExportForTest(t *testing.T, value Export) Export {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := DecodeExport(data)
	if err != nil {
		t.Fatal(err)
	}
	return cloned
}

func assertSameExportDigest(t *testing.T, left Export, right Export) {
	t.Helper()
	leftDigest, err := left.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("content digest differs for transport-only changes: %q != %q", leftDigest, rightDigest)
	}
}

func assertDifferentExportDigest(t *testing.T, left Export, right Export) {
	t.Helper()
	leftDigest, err := left.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest == rightDigest {
		t.Fatal("public export digest did not detect a structural configuration change")
	}
}

func assertSameConfigDigest(t *testing.T, left Export, right Export) {
	t.Helper()
	leftDigest, err := left.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("configuration digest differs: %q != %q", leftDigest, rightDigest)
	}
}

func assertSameBackupDigest(t *testing.T, left Export, right Export) {
	t.Helper()
	leftDigest, err := left.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("backup digest differs for URL rotation: %q != %q", leftDigest, rightDigest)
	}
}

func assertDifferentBackupDigest(t *testing.T, left Export, right Export) {
	t.Helper()
	leftDigest, err := left.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.BackupDigest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest == rightDigest {
		t.Fatal("backup digest did not detect a changed backup snapshot")
	}
}
