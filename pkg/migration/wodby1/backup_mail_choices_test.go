package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseBackupWindow(t *testing.T) {
	for _, tc := range []struct {
		window, start, end string
		minutes            int
		wantErr            bool
	}{
		{window: "01:00-04:00", start: "01:00", end: "04:00", minutes: 180},
		{window: " 23:30-0:30 ", start: "23:30", end: "00:30", minutes: 60},
		{window: "02:00-02:10", wantErr: true},
		{window: "02:00-06:00", wantErr: true},
		{window: "2am-4am", wantErr: true},
	} {
		start, end, minutes, err := ParseBackupWindow(tc.window)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: no error", tc.window)
			}
			continue
		}
		if err != nil || start != tc.start || end != tc.end || minutes != tc.minutes {
			t.Fatalf("%q = %s, %s, %d, %v", tc.window, start, end, minutes, err)
		}
	}
	if err := validateTargetChoices(5, "", "", 0); err == nil {
		t.Fatal("a backup integration needs its bucket")
	}
}

// integrationTestClient answers GET /v1/integrations/{id} with integrations
// of organization 1.
func integrationTestClient(t *testing.T, ids ...int) *TargetClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, id := range ids {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/integrations/"+itoa(id) {
				_ = json.NewEncoder(w).Encode(TargetIntegration{ID: id, OrgID: 1, ProviderRevID: 3, Status: "OK"})
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return mustTargetExecutionClient(t, server.URL)
}

func itoa(value int) string { return strconv.Itoa(value) }

// Backups can go to the member's own storage, in a window they chose, instead
// of Wodby Blob storage at 02:00-05:00.
func TestBackupsUseTheMembersStorageAndWindow(t *testing.T) {
	client := integrationTestClient(t, 40)
	app := &PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "shop"}},
		Instances: []PreparedInstance{{Source: Instance{
			UUID: "i-1", Name: "prod", BackupConfig: &BackupConfig{Enabled: true, Provider: "aws_s3"},
		}}},
	}
	target := PlanTarget{
		OrgID: 1, OrgCapabilities: &TargetOrgCapabilities{AutoBackups: true},
		BackupIntegrationID: 40, BackupBucket: "shop-backups", BackupWindow: "23:00-01:00",
	}
	prepared, findings, err := client.prepareBackupIntegrations(context.Background(), app, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 0 {
		t.Fatalf("own storage must not recreate the Wodby 1 mirror: %#v", prepared)
	}
	destination := app.Instances[0].BackupDestination
	if destination == nil || destination.IntegrationID != 40 || destination.Bucket != "shop-backups" || destination.IntegrationKey != "" {
		t.Fatalf("destination = %#v, findings = %#v", destination, findings)
	}
	input := migrationBackupPresetInput(9, "database", *destination)
	if input.TimeWindow == nil || input.TimeWindow.Start != "23:00" || input.TimeWindow.End != "01:00" || *input.Duration != 120 {
		t.Fatalf("preset input = %#v", input)
	}

	// Without the automatic-backups feature, own storage is refused.
	target.OrgCapabilities.AutoBackups = false
	app.Instances[0].BackupDestination = nil
	_, findings, err = client.prepareBackupIntegrations(context.Background(), app, target)
	if err != nil || !containsBlockingReviewItems(findings) {
		t.Fatalf("findings = %#v, %v", findings, err)
	}
}

// The member's SMTP integration relays mail of every instance that sends
// through OpenSMTPD, linked per app environment, without copied relay values.
func TestMailRelaysThroughTheMembersSMTPIntegration(t *testing.T) {
	client := integrationTestClient(t, 50)
	opensmtpd := TargetStackServiceInspection{StackService: TargetStackService{Name: "opensmtpd"}}
	app := &PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "shop"}},
		Instances: []PreparedInstance{
			{
				Source: Instance{UUID: "i-1", Name: "prod", Services: []Service{{Name: "opensmtpd", Enabled: true}}},
				Services: map[string]PreparedService{"opensmtpd": {
					Target: opensmtpd, InstanceEnvVars: []EnvVar{{Name: "RELAY_HOST", Value: "smtp.example.com"}, {Name: "KEEP", Value: "1"}},
				}},
			},
			{Source: Instance{UUID: "i-2", Name: "dev"}},
		},
	}
	findings, err := client.linkOwnSMTPIntegration(context.Background(), app, PlanTarget{OrgID: 1, SMTPIntegrationID: 50})
	if err != nil {
		t.Fatal(err)
	}
	links := app.Instances[0].ServiceIntegrations
	if len(links) != 1 || links[0].IntegrationID != 50 || links[0].IntegrationKey != "" || links[0].ServiceName != "opensmtpd" {
		t.Fatalf("links = %#v", links)
	}
	if vars := app.Instances[0].Services["opensmtpd"].InstanceEnvVars; len(vars) != 1 || vars[0].Name != "KEEP" {
		t.Fatalf("relay variables were not removed: %#v", vars)
	}
	if len(app.Instances[1].ServiceIntegrations) != 0 || len(findings) != 1 || !strings.Contains(findings[0].Message, "prod") {
		t.Fatalf("findings = %#v", findings)
	}
	// A link to an existing integration needs no resolution.
	if err := bindPreparedIntegrationIDs(&PreparedMigration{Instances: app.Instances}, map[string]int{}); err != nil {
		t.Fatal(err)
	}

	// An integration the target cannot use blocks the plan.
	findings, err = integrationTestClient(t).linkOwnSMTPIntegration(context.Background(), app, PlanTarget{OrgID: 1, SMTPIntegrationID: 51})
	if err != nil || !containsBlockingReviewItems(findings) {
		t.Fatalf("unknown integration: %#v, %v", findings, err)
	}
}
