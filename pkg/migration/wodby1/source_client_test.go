package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testSourceToken = strings.Repeat("a", 64)

func TestSourceClientExportsApp(t *testing.T) {
	var gotPath string
	var gotQuery string
	var gotAPIKey string
	var gotAuthorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("X-API-Key")
		gotAuthorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(Export{
			Schema:          ExportSchemaV2,
			Source:          &ExportSource{Kind: "app", UUID: "app-1"},
			SecretsIncluded: true,
			Apps: []AppExport{{
				App:       App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{sourceClientTestInstance()},
			}},
		})
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, "  token "+testSourceToken+"  ")
	if err != nil {
		t.Fatal(err)
	}

	export, err := client.ExportApp(context.Background(), "app-1")
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/v4/migrations/v2/apps/app-1/export" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotAPIKey != testSourceToken {
		t.Fatalf("X-API-Key = %q", gotAPIKey)
	}
	if gotAuthorization != "" {
		t.Fatalf("Authorization should not be sent, got %q", gotAuthorization)
	}
	if len(export.Apps) != 1 || export.Apps[0].App.Name != "demo" {
		t.Fatalf("export = %#v", export)
	}
	if len(export.Digest) != 64 {
		t.Fatalf("export digest = %q", export.Digest)
	}
	if len(export.ResponseDigest) != 64 {
		t.Fatalf("response digest = %q", export.ResponseDigest)
	}
	if len(export.ConfigMAC) != 64 {
		t.Fatalf("configuration MAC = %q", export.ConfigMAC)
	}
}

func TestDecodeExportRejectsUnsupportedSchema(t *testing.T) {
	_, err := DecodeExport([]byte(`{"schema":"old","app":{"uuid":"app-1","name":"demo"}}`))
	if err == nil {
		t.Fatal("expected unsupported schema error")
	}
}

func TestDecodeExportPreservesLargeNumbersAndRejectsTrailingJSON(t *testing.T) {
	export, err := DecodeExport([]byte(`{
		"schema":"wodby1-migration/v1",
		"app":{"uuid":"app-1","name":"demo"},
		"instances":[{
			"uuid":"instance-1",
			"name":"prod",
			"type":"prod",
			"stack":{"name":"drupal10"},
			"properties":{"large":9007199254740993}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := export.Instances[0].Properties["large"].(json.Number)
	if !ok || value.String() != "9007199254740993" {
		t.Fatalf("large value = %#v", export.Instances[0].Properties["large"])
	}

	_, err = DecodeExport([]byte(`{"schema":"wodby1-migration/v1","app":{"uuid":"app-1","name":"demo"}} {}`))
	if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeExportRequiresV2SourceIdentity(t *testing.T) {
	_, err := DecodeExport([]byte(`{"schema":"wodby1-migration/v2","apps":[]}`))
	if err == nil || !strings.Contains(err.Error(), "identify its source") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeAppExportRequiresAtLeastOneInstance(t *testing.T) {
	_, err := DecodeExport([]byte(`{
		"schema":"wodby1-migration/v2",
		"source":{"kind":"app","uuid":"app-1"},
		"apps":[{"app":{"uuid":"app-1","name":"demo"},"instances":[]}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "at least one source instance") {
		t.Fatalf("err = %v", err)
	}
}

func TestV2ExportValidationRejectsMalformedIdentities(t *testing.T) {
	valid := func() Export {
		return Export{
			Schema: ExportSchemaV2,
			Source: &ExportSource{Kind: "server", UUID: "server-1"},
			Apps: []AppExport{{
				App: App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{{
					UUID: "instance-1", Name: "prod", Type: "prod",
					Stack:    Stack{Name: "drupal10"},
					Services: []Service{{Name: "php"}},
					Domains:  []Domain{{UUID: "domain-1", Name: "example.com"}},
				}},
			}},
		}
	}
	cases := map[string]func(*Export){
		"blank app UUID": func(export *Export) {
			export.Apps[0].App.UUID = ""
		},
		"duplicate app UUID": func(export *Export) {
			export.Apps = append(export.Apps, AppExport{App: App{UUID: "app-1", Name: "other"}})
		},
		"blank instance UUID": func(export *Export) {
			export.Apps[0].Instances[0].UUID = ""
		},
		"duplicate instance name": func(export *Export) {
			export.Apps[0].Instances = append(export.Apps[0].Instances, Instance{
				UUID: "instance-2", Name: "prod", Type: "stage", Stack: Stack{Name: "drupal10"},
			})
		},
		"blank stack name": func(export *Export) {
			export.Apps[0].Instances[0].Stack.Name = ""
		},
		"duplicate service name": func(export *Export) {
			export.Apps[0].Instances[0].Services = append(
				export.Apps[0].Instances[0].Services,
				Service{Name: "php"},
			)
		},
		"blank domain UUID": func(export *Export) {
			export.Apps[0].Instances[0].Domains[0].UUID = ""
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			export := valid()
			mutate(&export)
			if err := export.Validate(); err == nil {
				t.Fatalf("expected malformed identity to fail: %#v", export)
			}
		})
	}
}

func TestSourceClientRejectsMismatchedSourceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Export{
			Schema: ExportSchemaV2,
			Source: &ExportSource{Kind: "app", UUID: "different-app"},
			Apps: []AppExport{{
				App:       App{UUID: "different-app", Name: "different"},
				Instances: []Instance{sourceClientTestInstance()},
			}},
		})
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExportApp(context.Background(), "app-1")
	if err == nil || !strings.Contains(err.Error(), "does not match requested source") {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceClientRejectsLegacySchemaWithoutSourceIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Export{
			Schema: ExportSchemaV1,
			App:    &App{UUID: "app-1", Name: "demo"},
		})
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExportApp(context.Background(), "app-1")
	if err == nil || !strings.Contains(err.Error(), "requires schema") {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceClientRequiresProtectedExport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Export{
			Schema:          ExportSchemaV2,
			Source:          &ExportSource{Kind: "app", UUID: "app-1"},
			SecretsIncluded: false,
			Apps: []AppExport{{
				App:       App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{sourceClientTestInstance()},
			}},
		})
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExportApp(context.Background(), "app-1")
	if err == nil || !strings.Contains(err.Error(), "did not include required secrets") {
		t.Fatalf("err = %v", err)
	}
}

func sourceClientTestInstance() Instance {
	return Instance{
		UUID: "instance-1", Name: "prod", Type: "prod", Status: "ok",
		Stack: Stack{Name: "drupal"},
	}
}

func TestNewSourceClientRejectsInvalidToken(t *testing.T) {
	if _, err := NewSourceClient("https://api.example.com", "short"); err == nil {
		t.Fatal("expected short Wodby 1 token to be rejected")
	}
	if _, err := NewSourceClient("https://api.example.com", strings.Repeat("!", 64)); err == nil {
		t.Fatal("expected unsupported Wodby 1 token characters to be rejected")
	}
}

func TestNewSourceClientRequiresTLSOutsideLoopback(t *testing.T) {
	if _, err := NewSourceClient("http://api.example.com", testSourceToken); err == nil ||
		!strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewSourceClient("https://user@example.com", testSourceToken); err == nil ||
		!strings.Contains(err.Error(), "must not contain user credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceClientRejectsInvalidSourceUUIDBeforeNetwork(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	client, err := NewSourceClient(server.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExportApp(context.Background(), " "); err == nil {
		t.Fatal("expected invalid source UUID")
	}
	if called {
		t.Fatal("invalid source UUID must be rejected before a network request")
	}
}

func TestSourceClientRejectsCrossOriginRedirectWithoutLeakingToken(t *testing.T) {
	destinationCalled := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalled = true
		if r.Header.Get("X-API-Key") != "" {
			t.Fatal("source token leaked to redirect destination")
		}
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer destination.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/export", http.StatusFound)
	}))
	defer redirect.Close()

	client, err := NewSourceClient(redirect.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExportApp(context.Background(), "app-1")
	if err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("err = %v", err)
	}
	if destinationCalled {
		t.Fatal("cross-origin redirect destination must not be requested")
	}
}

func TestSourceClientRejectsOversizedExportExplicitly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxSourceExportSize+1)))
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExportApp(context.Background(), "app-1")
	if err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeExportV2DomainAndRedactionFields(t *testing.T) {
	export, err := DecodeExport([]byte(`{
		"schema":"wodby1-migration/v2",
		"generated_at":1720000000,
		"source":{"kind":"app","uuid":"app-1"},
		"secrets_included":false,
		"apps":[{
			"app":{
				"uuid":"app-1",
				"name":"demo",
				"status":"ok",
				"created":1700000000,
				"updated":1710000000,
				"repository":{"uuid":"repo-1","title":"Demo repo","url":"git@example.com:demo.git","status":"ok"}
			},
			"instances":[{
				"uuid":"instance-1",
				"name":"prod",
				"type":"prod",
				"status":"active",
				"updated":1710000000,
				"stack":{"uuid":"stack-1","name":"custom","custom":true,"ancestor_uuid":"base-1","ancestor_name":"drupal"},
				"basic_auth":{"enabled":true,"password_redacted":false},
				"domains":[{
					"uuid":"domain-1",
					"name":"example.com",
					"type":"user",
					"status":"active",
					"enabled":false,
					"ssl_custom":true,
					"hsts":true,
					"hsts_subdomains":true,
					"service_protocol":"http"
				}],
				"services":[{
					"name":"php",
					"enabled":true,
					"env_vars":[{"name":"EMPTY","enabled":true,"protected":true,"redacted":false}],
					"cron_jobs":[{
						"title":"Cleanup",
						"crontab":"@daily",
						"command":"cleanup",
						"enabled":true,
						"source_line":4,
						"classification":"source_only_infrastructure"
					}]
				}]
			}]
		}],
		"issues":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	instance := export.Apps[0].Instances[0]
	app := export.Apps[0].App
	if app.Status != "ok" || app.Repository == nil || app.Repository.UUID != "repo-1" {
		t.Fatalf("app = %#v", app)
	}
	if instance.Status != "active" || instance.Updated != 1710000000 {
		t.Fatalf("instance = %#v", instance)
	}
	if instance.Stack.AncestorUUID != "base-1" || instance.Stack.AncestorName != "drupal" {
		t.Fatalf("stack = %#v", instance.Stack)
	}
	if instance.BasicAuth == nil || instance.BasicAuth.PasswordRedacted == nil || *instance.BasicAuth.PasswordRedacted {
		t.Fatalf("basic auth = %#v", instance.BasicAuth)
	}
	domain := instance.Domains[0]
	if domain.Enabled == nil || *domain.Enabled || !domain.SSLCustom || !domain.HSTS ||
		!domain.HSTSSubdomains || domain.ServiceProtocol != "http" {
		t.Fatalf("domain = %#v", domain)
	}
	envVar := instance.Services[0].EnvVars[0]
	if envVar.Redacted == nil || *envVar.Redacted || envVar.IsRedacted() {
		t.Fatalf("env var = %#v", envVar)
	}
	cron := instance.Services[0].CronJobs[0]
	if cron.SourceLine != 4 || cron.Classification != "source_only_infrastructure" {
		t.Fatalf("cron = %#v", cron)
	}
}
