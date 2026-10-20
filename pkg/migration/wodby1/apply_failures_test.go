package wodby1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wodby 2 takes the backup window length in minutes, 30 to 180.
func TestMigrationBackupPresetInputUsesMinutes(t *testing.T) {
	input := migrationBackupPresetInput(10, "database", PreparedBackupDestination{Auto: true, TimeZone: "UTC", IntegrationID: 1})
	if input.Duration == nil || *input.Duration != 180 {
		t.Fatalf("duration = %v, want 180 minutes", input.Duration)
	}
	manual := migrationBackupPresetInput(10, "database", PreparedBackupDestination{IntegrationID: 1})
	if manual.Duration != nil || manual.TimeWindow != nil {
		t.Fatalf("manual preset carries a window: %+v", manual)
	}
}

func linkTestInspection(name, serviceType string, labels ...string) TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService:    TargetStackService{Name: name},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{Name: name, Type: serviceType, Labels: labels}},
	}
}

// phpMyAdmin requires a database link; Wodby 2 refuses to publish the stack
// without it, so the migration fills it from the only matching database.
func TestPrepareAddedServiceLinksFillsRequiredDatabaseLink(t *testing.T) {
	pma := TargetStackServiceInspection{
		StackService: TargetStackService{Name: "phpmyadmin"},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Name: "phpmyadmin", Type: "admin",
			Links: []TargetServiceLinkCapability{{Name: "db", Required: true, Selectors: []TargetServiceLinkSelector{
				{Type: "db", Labels: []string{"mariadb"}}, {Type: "db", Labels: []string{"mysql"}},
			}}},
		}},
	}
	for _, tc := range []struct {
		name     string
		services []TargetStackServiceInspection
		enabled  map[string]bool
		want     string
	}{
		{
			name:     "one database",
			services: []TargetStackServiceInspection{linkTestInspection("mariadb", "db", "mariadb"), linkTestInspection("valkey", "cache")},
			enabled:  map[string]bool{"mariadb": true, "valkey": true, "phpmyadmin": true},
			want:     "mariadb",
		},
		{
			name:     "database disabled",
			services: []TargetStackServiceInspection{linkTestInspection("mariadb", "db", "mariadb")},
			enabled:  map[string]bool{"phpmyadmin": true},
		},
		{
			name:     "two databases",
			services: []TargetStackServiceInspection{linkTestInspection("mariadb", "db", "mariadb"), linkTestInspection("mysql", "db", "mysql")},
			enabled:  map[string]bool{"mariadb": true, "mysql": true, "phpmyadmin": true},
		},
		{
			name:     "postgres does not match",
			services: []TargetStackServiceInspection{linkTestInspection("postgres", "db", "postgres")},
			enabled:  map[string]bool{"postgres": true, "phpmyadmin": true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := PreparedAppMigration{
				App:            AppExport{App: App{Name: "demo"}},
				Instances:      []PreparedInstance{{StackServices: tc.services, EffectiveState: tc.enabled}},
				StackAdditions: []PreparedStackServiceAddition{{Name: "phpmyadmin", Inspection: pma}},
			}
			configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
			findings := prepareAddedServiceLinks(&app, &configuration)
			links := configuration.Services["phpmyadmin"].Links
			if tc.want == "" {
				if !containsBlockingReviewItems(findings) || len(links) != 0 {
					t.Fatalf("links = %#v, findings = %#v", links, findings)
				}
				if !strings.Contains(findings[0].Message, `required link "db"`) {
					t.Fatalf("findings = %#v", findings)
				}
				return
			}
			if containsBlockingReviewItems(findings) || len(links) != 1 || links[0].Name != "db" || links[0].LinkedServiceName != tc.want {
				t.Fatalf("links = %#v, findings = %#v", links, findings)
			}
		})
	}
}

// The create response can leave out what Wodby 2 stores on the preset's
// schedule. The stored preset decides, and a real mismatch names its fields
// without leaving an operation that blocks rollback.
func TestEnsureBackupPresetJudgesTheStoredPreset(t *testing.T) {
	for _, tc := range []struct {
		name       string
		storedEnd  string
		storedDays []string
		wantErr    string
	}{
		{name: "stored preset matches", storedEnd: "05:00", storedDays: migrationBackupDays},
		// The REST API has returned stored weekdays in lower case.
		{name: "stored weekdays in lower case", storedEnd: "05:00", storedDays: []string{
			"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		}},
		{name: "stored weekdays differ", storedEnd: "05:00", storedDays: []string{"monday"}, wantErr: "timeWindow.days"},
		{name: "stored preset differs", storedEnd: "06:00", storedDays: migrationBackupDays, wantErr: "timeWindow.end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serviceID, backupName, duration := 30, "files", 180
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/backup-presets":
					writeTargetExecutionJSON(t, w, []TargetBackupPreset{})
				case r.Method == http.MethodPost && r.URL.Path == "/v1/backup-presets":
					writeTargetExecutionJSON(t, w, TargetBackupPreset{
						ID: 50, AppServiceID: &serviceID, BackupName: &backupName, IntegrationID: 1, Auto: true,
					})
				case r.Method == http.MethodGet && r.URL.Path == "/v1/backup-presets/50":
					writeTargetExecutionJSON(t, w, TargetBackupPreset{
						ID: 50, AppServiceID: &serviceID, BackupName: &backupName, IntegrationID: 1, Auto: true,
						Duration: &duration, TimeWindow: &TargetAutomationTimeWindow{
							Start: "02:00", End: tc.storedEnd, TimeZone: "UTC", Days: tc.storedDays,
						},
					})
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()
			state, statePath := newExecutorTestState(t)
			executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), state: FileStateStore(statePath)}
			err := executor.ensureBackupPreset(context.Background(), state, "instance-1",
				TargetAppService{ID: serviceID, Name: "files-nfs"}, backupName,
				PreparedBackupDestination{Auto: true, TimeZone: "UTC", IntegrationID: 1})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
			if unresolved := unresolvedRollbackOperations(state); len(unresolved) != 0 {
				t.Fatalf("rollback would be blocked by %v", unresolved)
			}
		})
	}
}
