package wodby1

import (
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
