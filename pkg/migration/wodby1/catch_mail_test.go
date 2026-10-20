package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mailpitInspection() TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService:    TargetStackService{Name: "mail-catcher", Disabled: true},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{Name: "mailpit"}},
	}
}

// A non-production app environment can catch its mail in the stack's Mailpit
// service, found by service identity; a stack without one keeps the Wodby 1
// delivery and says so.
func TestEnableCatchMailEnablesTheStacksMailpit(t *testing.T) {
	effective := map[string]bool{"mail-catcher": false}
	name, findings := enableCatchMail(App{Name: "shop"}, Instance{Name: "dev"}, []TargetStackServiceInspection{mailpitInspection()}, effective, nil)
	if name != "mail-catcher" || !effective["mail-catcher"] || len(findings) != 1 || findings[0].Severity != SeverityMigration {
		t.Fatalf("name = %q, effective = %v, findings = %#v", name, effective, findings)
	}
	name, findings = enableCatchMail(App{Name: "shop"}, Instance{Name: "dev"}, nil, map[string]bool{}, nil)
	if name != "" || len(findings) != 1 || !strings.Contains(findings[0].Message, "no Mailpit") {
		t.Fatalf("name = %q, findings = %#v", name, findings)
	}
}

func sendmailInstance(uuid, name, envType, catcher string, relay bool) PreparedInstance {
	php := TargetStackServiceInspection{
		StackService: TargetStackService{Name: "php"},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Name: "php", Links: []TargetServiceLinkCapability{{Name: targetMailDeliveryLinkName}},
		}},
	}
	opensmtpd := Service{Name: "opensmtpd", Enabled: true}
	if relay {
		opensmtpd.EnvVars = []EnvVar{{Name: "RELAY_HOST", Value: "smtp.example.com", Enabled: true}}
	}
	return PreparedInstance{
		Source: Instance{
			UUID: uuid, Name: name, Properties: map[string]interface{}{"mail_service": "opensmtpd"},
			Services: []Service{{Name: "php", Enabled: true}, opensmtpd},
		},
		TargetEnvType:    envType,
		CatchMailService: catcher,
		Services: map[string]PreparedService{
			"php":       {Target: php},
			"opensmtpd": {Target: TargetStackServiceInspection{StackService: TargetStackService{Name: "opensmtpd"}}},
		},
		EffectiveState: map[string]bool{"php": true, "opensmtpd": true, "mail-catcher": catcher != ""},
	}
}

// Production keeps its Wodby 1 relay while a caught environment's sendmail
// goes to Mailpit, each linked to its own app environment.
func TestCaughtMailLinksToMailpitAndKeepsTheRelayElsewhere(t *testing.T) {
	app := &PreparedAppMigration{
		App:                AppExport{App: App{UUID: "app-1", Name: "shop", Title: "Shop"}},
		Instances:          []PreparedInstance{sendmailInstance("i-1", "prod", "PROD", "", true), sendmailInstance("i-2", "dev", "DEV", "mail-catcher", true)},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}},
	}
	prepareMailDeliveryLinks(app)
	if links := app.Instances[1].ServiceLinks; len(links) != 1 || links[0].LinkedServiceName != "mail-catcher" {
		t.Fatalf("dev links = %#v", links)
	}
	if links := app.Instances[0].ServiceLinks; len(links) != 1 || links[0].LinkedServiceName != "opensmtpd" {
		t.Fatalf("prod links = %#v", links)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TargetProvider{ID: 7, RevID: 70, Name: "custom-smtp", Title: "Custom SMTP"})
	}))
	defer server.Close()
	_, relayInstances, _, err := mustTargetExecutionClient(t, server.URL).prepareSMTPIntegration(context.Background(), *app)
	if err != nil {
		t.Fatal(err)
	}
	if len(relayInstances) != 1 || !relayInstances["i-1"] {
		t.Fatalf("relay instances = %v, want production only", relayInstances)
	}
}
