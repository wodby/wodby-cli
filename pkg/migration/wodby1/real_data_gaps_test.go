package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func filesImportInspection(name string) TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService: TargetStackService{Name: name},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Imports: []TargetServiceImportCapability{{Name: "files", Volume: "data"}},
		}},
	}
}

// WordPress stacks name their NFS storage wp-content-nfs, not files-nfs.
func TestResolveImportDestinationUsesTheOnlyFilesImport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		services []string
		want     string
	}{
		{name: "wordpress", services: []string{"wp-content-nfs"}, want: "wp-content-nfs"},
		{name: "drupal", services: []string{"files-nfs"}, want: "files-nfs"},
		{name: "files-nfs preferred", services: []string{"files-nfs", "wp-content-nfs"}, want: "files-nfs"},
		{name: "ambiguous", services: []string{"one-nfs", "two-nfs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspections := []TargetStackServiceInspection{}
			effective := map[string]bool{}
			for _, name := range tc.services {
				inspections = append(inspections, filesImportInspection(name))
				effective[name] = true
			}
			destination, findings := resolveImportDestination(App{Name: "demo"}, Instance{Name: "prod"},
				Backup{Component: "files"}, &ImportPlan{}, inspections, effective)
			if tc.want == "" {
				if destination != nil || !containsBlockingReviewItems(findings) {
					t.Fatalf("destination = %#v, findings = %#v", destination, findings)
				}
				return
			}
			if destination == nil || destination.ServiceName != tc.want || destination.ImportName != "files" {
				t.Fatalf("destination = %#v, findings = %#v", destination, findings)
			}
		})
	}
}

func smtpInstance(id, envType string, relay bool) PreparedInstance {
	variables := []EnvVar{{Name: "RELAY_PORT", Value: "587", Enabled: true}}
	if relay {
		variables = append(variables,
			EnvVar{Name: "RELAY_HOST", Value: "smtp.example.com", Enabled: true},
			EnvVar{Name: "RELAY_USER", Value: "mailer", Enabled: true},
			EnvVar{Name: "RELAY_PASSWORD", Value: "secret", Enabled: true, Secret: true},
		)
	}
	source := Service{Name: "opensmtpd", Enabled: true, EnvVars: variables}
	return PreparedInstance{
		Source:        Instance{UUID: id, Name: id, Services: []Service{source}},
		TargetEnvType: envType,
		Services: map[string]PreparedService{"opensmtpd": {
			Source: source, Target: TargetStackServiceInspection{StackService: TargetStackService{Name: "opensmtpd"}},
			InstanceEnvVars: variables,
		}},
	}
}

func smtpProviderServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/v1/providers/by-name/")
		if r.Method != http.MethodGet || name == r.URL.Path {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(TargetProvider{ID: 8, RevID: 80, Name: name, Title: name})
	}))
}

// Production relays mail while development does not: the integration goes to
// the production app environment only, not to the shared stack.
func TestPrepareSMTPIntegrationLinksOnlyInstancesThatRelay(t *testing.T) {
	server := smtpProviderServer(t)
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)
	app := PreparedAppMigration{
		App:       AppExport{App: App{UUID: "app-1", Name: "demo", Title: "Demo"}},
		Instances: []PreparedInstance{smtpInstance("prod", "prod", true), smtpInstance("dev", "dev", false)},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"opensmtpd": {EnvVars: []PreparedStackEnvVar{{Name: "RELAY_PORT", Value: "587"}}},
		}},
	}
	item, relayInstances, findings, err := client.prepareSMTPIntegration(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if containsBlockingReviewItems(findings) || item == nil {
		t.Fatalf("item = %#v, findings = %#v", item, findings)
	}
	if !relayInstances["prod"] || relayInstances["dev"] || len(relayInstances) != 1 {
		t.Fatalf("relay instances = %#v", relayInstances)
	}
	if !hasReviewMessage(findings, SeverityMigration, "linked to the app environments of prod only") {
		t.Fatalf("findings = %#v", findings)
	}

	findings, err = client.prepareAppIntegrations(context.Background(), &app, &AppPlan{}, PlanTarget{OrgID: 1})
	if err != nil || containsBlockingReviewItems(findings) {
		t.Fatalf("err = %v, findings = %#v", err, findings)
	}
	if links := app.StackConfiguration.Services["opensmtpd"].Integrations; len(links) != 0 {
		t.Fatalf("relay must not be linked stack-wide: %#v", links)
	}
	prod, dev := app.Instances[0], app.Instances[1]
	if len(prod.ServiceIntegrations) != 1 || prod.ServiceIntegrations[0].ServiceName != "opensmtpd" ||
		prod.ServiceIntegrations[0].Name != "smtp" || len(dev.ServiceIntegrations) != 0 {
		t.Fatalf("instance links: prod %#v, dev %#v", prod.ServiceIntegrations, dev.ServiceIntegrations)
	}
	for _, instance := range app.Instances {
		for _, variable := range instance.Services["opensmtpd"].InstanceEnvVars {
			if strings.HasPrefix(variable.Name, "RELAY_") {
				t.Fatalf("instance %s keeps relay variable %s", instance.Source.UUID, variable.Name)
			}
		}
	}
	for _, variable := range app.StackConfiguration.Services["opensmtpd"].EnvVars {
		if strings.HasPrefix(variable.Name, "RELAY_") {
			t.Fatalf("stack keeps relay variable %s", variable.Name)
		}
	}
}

// A sibling read only for context counts too: it would otherwise get the relay
// once it joins the same app.
func TestPrepareSMTPIntegrationConsidersContextInstances(t *testing.T) {
	server := smtpProviderServer(t)
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)
	app := PreparedAppMigration{
		App:              AppExport{App: App{UUID: "app-1", Name: "demo", Title: "Demo"}},
		Instances:        []PreparedInstance{smtpInstance("prod", "prod", true)},
		ContextInstances: []PreparedInstance{smtpInstance("dev", "dev", false)},
	}
	_, relayInstances, findings, err := client.prepareSMTPIntegration(context.Background(), app)
	if err != nil || containsBlockingReviewItems(findings) {
		t.Fatalf("err = %v, findings = %#v", err, findings)
	}
	if !relayInstances["prod"] {
		t.Fatalf("relay instances = %#v", relayInstances)
	}
}

func TestEnsureAppServiceIntegrationCreatesOnceAndVerifies(t *testing.T) {
	links := []TargetAppServiceIntegration{}
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/30/integrations":
			writeTargetExecutionJSON(t, w, links)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/30/integrations":
			var body TargetCreateStackServiceIntegrationInput
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			creates++
			link := TargetAppServiceIntegration{ID: 90, AppServiceID: 30, Name: body.Name, IntegrationID: body.IntegrationID}
			links = append(links, link)
			writeTargetExecutionJSON(t, w, link)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), state: FileStateStore(statePath)}
	service := TargetAppService{ID: 30, Name: "opensmtpd", AppInstanceID: 20}
	link := PreparedAppServiceIntegration{ServiceName: "opensmtpd", Name: "smtp", IntegrationKey: "smtp", IntegrationID: 70}
	for attempt := 0; attempt < 2; attempt++ {
		if err := executor.ensureAppServiceIntegration(context.Background(), state, "instance-1", service, link); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 1 {
		t.Fatalf("creates = %d, want 1", creates)
	}
	// A link to another integration is never replaced silently.
	other := link
	other.IntegrationID = 71
	if err := executor.ensureAppServiceIntegration(context.Background(), state, "instance-1", service, other); err == nil ||
		!strings.Contains(err.Error(), "another integration") {
		t.Fatalf("error = %v", err)
	}
}
