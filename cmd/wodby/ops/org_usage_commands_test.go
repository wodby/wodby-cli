package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const orgUsageTestCSV = "period_start,period_end,project_id,project_name,project_title,item,quantity,unit,amount_usd,invoice_share_usd\n" +
	"2026-10-15T00:00:00Z,2026-11-15T00:00:00Z,4,shop,Shop,app_services,720,service-hour,7.2,\n"

// newTestOrgCommand wraps the usage command in an org command with an --output flag, as the CLI registers it.
func newTestOrgCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "org"}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newOrgUsageCommand())
	return cmd
}

// orgUsageTestServer serves organization 3's usage report, periods and CSV, and records the query of each request.
func orgUsageTestServer(t *testing.T, queries map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		queries[r.URL.Path] = r.URL.RawQuery
		switch r.URL.Path {
		case "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]interface{}{map[string]interface{}{"id": 3, "name": "acme"}})
		case "/v1/orgs/3/usage":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"period":        map[string]interface{}{"start": "2026-10-15T00:00:00Z", "end": "2026-11-15T00:00:00Z", "current": true},
				"amount":        12.3456,
				"invoiceAmount": nil,
				"projects": []interface{}{
					map[string]interface{}{"projectId": 4, "name": "shop", "title": "Shop", "deleted": false, "amount": 7.2, "invoiceShare": nil,
						"items": []interface{}{map[string]interface{}{"item": "app_services", "title": "App services", "quantity": 720, "unit": "service-hour", "amount": 7.2, "invoiceShare": nil}}},
					map[string]interface{}{"projectId": nil, "name": "", "title": "", "deleted": false, "amount": 5.1456, "invoiceShare": nil,
						"items": []interface{}{map[string]interface{}{"item": "ci_minutes", "title": "CI minutes", "quantity": 1234.5678, "unit": "minute", "amount": 5.1456, "invoiceShare": nil}}},
				},
			})
		case "/v1/orgs/3/usage/periods":
			_ = json.NewEncoder(w).Encode([]interface{}{
				map[string]interface{}{"start": "2026-10-15T00:00:00Z", "end": "2026-11-15T00:00:00Z", "current": true},
				map[string]interface{}{"start": "2026-09-15T00:00:00Z", "end": "2026-10-15T00:00:00Z", "current": false},
			})
		case "/v1/orgs/3/usage/csv":
			if accept := r.Header.Get("Accept"); accept != "text/csv" {
				t.Errorf("Accept = %q, want text/csv", accept)
			}
			if r.Header.Get("X-API-KEY") != "secret" {
				t.Errorf("CSV request has no API key")
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			_, _ = w.Write([]byte(orgUsageTestCSV))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

// runOrgUsageCommand runs an org subcommand and returns its output.
func runOrgUsageCommand(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newTestOrgCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestOrgUsageCommands(t *testing.T) {
	queries := map[string]string{}
	server := orgUsageTestServer(t, queries)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	report := runOrgUsageCommand(t, "usage", "--period-start", "2026-10-15T00:00:00Z")
	if got := queries["/v1/orgs/3/usage"]; got != "periodStart=2026-10-15T00%3A00%3A00Z" {
		t.Fatalf("usage query = %q", got)
	}
	for _, want := range []string{"(current)", "12.35 USD", "Invoiced:  unknown", "Shop", "App services", "720", "service-hour", "7.2", "Organization", "1234.57"} {
		if !strings.Contains(report, want) {
			t.Fatalf("usage output misses %q:\n%s", want, report)
		}
	}

	periods := runOrgUsageCommand(t, "usage", "periods", "--org", "3")
	if !strings.Contains(periods, "2026-09-15T00:00:00Z") {
		t.Fatalf("periods output misses the past period:\n%s", periods)
	}

	if got := runOrgUsageCommand(t, "usage", "csv", "--org", "3"); got != orgUsageTestCSV {
		t.Fatalf("csv output = %q, want %q", got, orgUsageTestCSV)
	}
	path := filepath.Join(t.TempDir(), "usage.csv")
	saved := runOrgUsageCommand(t, "usage", "csv", "--org", "3", "--period-start", "2026-09-15T00:00:00Z", "--save-to", path)
	if !strings.Contains(saved, "Saved") {
		t.Fatalf("csv --save-to output = %q", saved)
	}
	if got := queries["/v1/orgs/3/usage/csv"]; got != "periodStart=2026-09-15T00%3A00%3A00Z" {
		t.Fatalf("csv query = %q", got)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != orgUsageTestCSV {
		t.Fatalf("saved CSV = %q, %v", content, err)
	}

	asJSON := runOrgUsageCommand(t, "usage", "--org", "3", "-o", "json")
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(asJSON), &decoded); err != nil || decoded["projects"] == nil {
		t.Fatalf("json output = %s (%v)", asJSON, err)
	}
}

func TestOrgUsageCSVReportsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "billing access required"})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newTestOrgCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"usage", "csv", "--org", "3"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "billing access required") {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(out.String(), `{"message"`) {
		t.Fatalf("the error body was written as CSV:\n%s", out.String())
	}
}

func TestOrgUsageRejectsAMalformedPeriodStart(t *testing.T) {
	configureTestAPI(t, "http://127.0.0.1:1/v1")
	var out bytes.Buffer
	cmd := newTestOrgCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"usage", "--org", "3", "--period-start", "October"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid --period-start") {
		t.Fatalf("Execute() error = %v", err)
	}
}
