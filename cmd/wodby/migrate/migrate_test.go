package migrate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

func TestWodby1AppCommandPrintsDryRunReviewAndPlanFile(t *testing.T) {
	var gotPath string
	port := 80
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchema,
			App:    &wodby1.App{UUID: "app-1", Name: "demo", Title: "Demo"},
			Instances: []wodby1.Instance{{
				UUID:  "inst-1",
				Name:  "prod",
				Type:  "prod",
				Stack: wodby1.Stack{Name: "drupal10"},
				Domains: []wodby1.Domain{{
					Name:       "example.com",
					Primary:    true,
					Service:    "nginx",
					PortNumber: &port,
				}},
			}},
		})
	}))
	defer server.Close()

	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := newWodby1AppCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--source-token", "secret",
		"--target-env-map", "prod=production",
		"--plan-file", planPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/migrations/apps/app-1/export" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(out.String(), "Wodby 1 migration review") {
		t.Fatalf("output = %s", out.String())
	}

	content, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(content, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Apps[0].Instances[0].TargetEnv != "production" {
		t.Fatalf("plan env = %#v", plan.Apps[0].Instances[0])
	}
}

func TestWodby1CommandRejectsDryRunAndExecute(t *testing.T) {
	cmd := newWodby1AppCommand()
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", "https://example.com",
		"--source-token", "secret",
		"--dry-run",
		"--execute",
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v", err)
	}
}

func TestWodby1CommandWritesJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchema,
			App:    &wodby1.App{UUID: "app-1", Name: "demo"},
			Instances: []wodby1.Instance{{
				UUID:  "inst-1",
				Name:  "prod",
				Type:  "prod",
				Stack: wodby1.Stack{Name: "drupal10"},
			}},
		})
	}))
	defer server.Close()

	cmd := newWodby1AppCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--source-token", "secret",
		"--output", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Schema != "wodby1-migration-plan/v1" {
		t.Fatalf("plan schema = %q", plan.Schema)
	}
}
