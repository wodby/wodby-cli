package ops

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// restRouteCase runs one CLI command against a recording API server and checks the request it sends.
type restRouteCase struct {
	name      string
	command   func() *cobra.Command
	args      []string
	method    string
	path      string
	query     string
	body      map[string]interface{}
	noBody    bool
	stdin     string
	response  interface{}
	wantInOut string
}

func runRESTRouteCases(t *testing.T, cases []restRouteCase) {
	t.Helper()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
				gotBody, _ = io.ReadAll(r.Body)
				response := test.response
				if response == nil {
					response = map[string]interface{}{"success": true}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			var out bytes.Buffer
			cmd := test.command()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader(test.stdin))
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\n%s", err, out.String())
			}
			if gotMethod != test.method || gotPath != test.path {
				t.Fatalf("request = %s %s, want %s %s", gotMethod, gotPath, test.method, test.path)
			}
			if gotQuery != test.query {
				t.Fatalf("query = %q, want %q", gotQuery, test.query)
			}
			switch {
			case test.noBody:
				if len(bytes.TrimSpace(gotBody)) > 0 && string(bytes.TrimSpace(gotBody)) != "null" {
					t.Fatalf("body = %s, want none", gotBody)
				}
			case test.body != nil:
				var body map[string]interface{}
				if err := json.Unmarshal(gotBody, &body); err != nil {
					t.Fatalf("decode body %q: %v", gotBody, err)
				}
				if !reflect.DeepEqual(body, test.body) {
					t.Fatalf("body = %#v, want %#v", body, test.body)
				}
			}
			if test.wantInOut != "" && !strings.Contains(out.String(), test.wantInOut) {
				t.Fatalf("output does not contain %q:\n%s", test.wantInOut, out.String())
			}
		})
	}
}

func TestCommandsForExistingRESTRoutes(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "provider.yml")
	if err := os.WriteFile(manifest, []byte("name: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRESTRouteCases(t, []restRouteCase{
		{name: "org subscription", command: newOrgCommand, args: []string{"subscription", "7"}, method: http.MethodGet, path: "/v1/orgs/7/subscription",
			response: map[string]interface{}{"status": "active", "plan": map[string]interface{}{"name": "team"}}, wantInOut: "team"},
		{name: "member get", command: newMemberCommand, args: []string{"get", "5"}, method: http.MethodGet, path: "/v1/org-memberships/5"},
		{name: "workspace git status", command: newAppEnvironmentCommand, args: []string{"workspace", "git-status", "3"}, method: http.MethodGet, path: "/v1/workspaces/3/git-status",
			response: map[string]interface{}{"available": true, "branch": "feature-x"}, wantInOut: "feature-x"},
		{name: "auth list", command: newAppEnvironmentCommand, args: []string{"auth", "list", "3"}, method: http.MethodGet, path: "/v1/app-auths", query: "appInstanceId=3",
			response: []interface{}{map[string]interface{}{"id": 1, "login": "admin"}}, wantInOut: "admin"},
		{name: "auth create", command: newAppEnvironmentCommand, args: []string{"auth", "create", "3", "--login", "admin", "--password", "s3cret", "--realm", "Staging", "--route", "9"},
			method: http.MethodPost, path: "/v1/app-auths", body: map[string]interface{}{"appInstanceId": float64(3), "login": "admin", "password": "s3cret", "realm": "Staging", "appRouteId": float64(9)}},
		{name: "auth update", command: newAppEnvironmentCommand, args: []string{"auth", "update", "4", "--login", "admin", "--realm", "Staging"},
			method: http.MethodPut, path: "/v1/app-auths/4", body: map[string]interface{}{"login": "admin", "realm": "Staging"}},
		{name: "auth delete", command: newAppEnvironmentCommand, args: []string{"auth", "delete", "4", "--yes"}, method: http.MethodDelete, path: "/v1/app-auths/4"},
		{name: "integration search", command: newIntegrationCommand, args: []string{"search", "--data", `{"types":["git"]}`},
			method: http.MethodPost, path: "/v1/integrations/actions/search", body: map[string]interface{}{"types": []interface{}{"git"}}},
		{name: "integration resolve", command: newIntegrationCommand, args: []string{"resolve", "--data", `{"providerId":2,"title":"Git","kinds":["git"]}`},
			method: http.MethodPost, path: "/v1/integrations/actions/resolve", body: map[string]interface{}{"providerId": float64(2), "title": "Git", "kinds": []interface{}{"git"}},
			response: map[string]interface{}{"created": false, "integration": map[string]interface{}{"id": 8, "title": "Git"}}, wantInOut: "Git"},
		{name: "provider create variable", command: newProviderCommand, args: []string{"create-variable", "--data", `{"name":"acme","title":"Acme","fields":[]}`},
			method: http.MethodPost, path: "/v1/providers/actions/create-variable", body: map[string]interface{}{"name": "acme", "title": "Acme", "fields": []interface{}{}}},
		{name: "provider create from manifest", command: newProviderCommand, args: []string{"create-from-manifest", manifest, "--org", "1"},
			method: http.MethodPost, path: "/v1/providers/actions/create-from-manifest", body: map[string]interface{}{"manifestYaml": "name: acme\n", "orgId": float64(1)}},
		{name: "provider update from manifest from stdin", command: newProviderCommand, args: []string{"update-from-manifest", "6", "-"}, stdin: "name: acme\n",
			method: http.MethodPost, path: "/v1/providers/6/actions/update-from-manifest", body: map[string]interface{}{"manifestYaml": "name: acme\n"}},
		{name: "provider import", command: newProviderCommand, args: []string{"import", "--org", "1", "--integration", "2", "--remote-git-repo", "acme/providers", "--git-ref", "main", "--git-ref-type", "branch"},
			method: http.MethodPost, path: "/v1/providers/actions/import"},
		{name: "provider update from git", command: newProviderCommand, args: []string{"update-from-git", "6", "--git-ref", "v2", "--git-ref-type", "tag"},
			method: http.MethodPost, path: "/v1/providers/6/actions/update-from-git", body: map[string]interface{}{"gitRef": "v2", "gitRefType": "tag"}},
		{name: "provider settings", command: newProviderCommand, args: []string{"settings", "update", "6", "--data", `{"gitAutoUpdate":{"enabled":false}}`},
			method: http.MethodPut, path: "/v1/providers/settings/6", body: map[string]interface{}{"gitAutoUpdate": map[string]interface{}{"enabled": false}}},
		{name: "git repo usages", command: newGitRepoCommand, args: []string{"usages", "11"}, method: http.MethodGet, path: "/v1/git-repos/11/usages"},
		{name: "git repo update", command: newGitRepoCommand, args: []string{"update-from-git", "11", "--git-ref", "main", "--git-ref-type", "branch"},
			method: http.MethodPost, path: "/v1/git-repos/11/actions/update-from-git", body: map[string]interface{}{"gitRef": "main", "gitRefType": "branch"}},
		{name: "service update from manifest", command: newServiceCommand, args: []string{"update-from-manifest", "12", manifest, "--version", "1.1.0"},
			method: http.MethodPost, path: "/v1/services/12/actions/update-from-manifest", body: map[string]interface{}{"manifestYaml": "name: acme\n", "version": "1.1.0"}},
		{name: "stack catalog", command: newStackCommand, args: []string{"catalog"}, method: http.MethodGet, path: "/v1/catalog/stacks", response: []interface{}{}},
		{name: "stack update", command: newStackCommand, args: []string{"update", "13", "--name", "shop", "--title", "Shop"},
			method: http.MethodPut, path: "/v1/stacks/13", body: map[string]interface{}{"name": "shop", "title": "Shop"}},
		{name: "stack delete", command: newStackCommand, args: []string{"delete", "13", "--yes"}, method: http.MethodDelete, path: "/v1/stacks/13"},
		{name: "stack env var list", command: newStackCommand, args: []string{"env-var", "list", "40"}, method: http.MethodGet, path: "/v1/stack-revisions/40/env-vars", response: []interface{}{}},
		{name: "stack env var create", command: newStackCommand, args: []string{"env-var", "create", "13", "--name", "APP_ENV", "--value", "prod", "--env-type", "prod"},
			method: http.MethodPost, path: "/v1/stacks/13/configuration/env-vars", body: map[string]interface{}{"name": "APP_ENV", "value": "prod", "secret": false, "envType": "prod"}},
		{name: "stack env var update", command: newStackCommand, args: []string{"env-var", "update", "14", "--value", "x", "--secret"},
			method: http.MethodPut, path: "/v1/stack-env-vars/14", body: map[string]interface{}{"value": "x", "secret": true}},
		{name: "stack env var delete", command: newStackCommand, args: []string{"env-var", "delete", "14", "--yes"}, method: http.MethodDelete, path: "/v1/stack-env-vars/14"},
		{name: "backup void file", command: newBackupCommand, args: []string{"void-file", "15", "--yes"}, method: http.MethodPost, path: "/v1/backups/15/actions/void-file", noBody: true},
		{name: "environment backup void file", command: newAppEnvironmentCommand, args: []string{"backup", "void-file", "15", "--yes"}, method: http.MethodPost, path: "/v1/backups/15/actions/void-file", noBody: true},
		{name: "task step log url", command: newTaskCommand, args: []string{"step", "log-url", "16"}, method: http.MethodGet, path: "/v1/task-steps/16/log-url",
			response: map[string]interface{}{"url": "https://logs.example/16"}, wantInOut: "https://logs.example/16"},
		{name: "route retry certificate", command: rootRouteCommand, args: []string{"retry-certificate", "17"}, method: http.MethodPost, path: "/v1/app-routes/17/actions/retry-certificate"},
		{name: "route settings list", command: rootRouteCommand, args: []string{"settings", "list", "17"}, method: http.MethodGet, path: "/v1/app-routes/17/settings", response: []interface{}{}},
		{name: "route effective settings", command: rootRouteCommand, args: []string{"settings", "effective", "17"}, method: http.MethodGet, path: "/v1/app-routes/17/effective-settings",
			response: []interface{}{map[string]interface{}{"name": "HSTS", "value": "true", "source": "APP_DEFAULT"}}, wantInOut: "APP_DEFAULT"},
		{name: "route setting set", command: rootRouteCommand, args: []string{"settings", "set", "17", "REQUEST_TIMEOUT", "--value", "120"},
			method: http.MethodPut, path: "/v1/app-routes/17/settings/REQUEST_TIMEOUT", body: map[string]interface{}{"value": "120"}},
		{name: "route setting delete", command: rootRouteCommand, args: []string{"settings", "delete", "17", "REQUEST_TIMEOUT", "--yes"}, method: http.MethodDelete, path: "/v1/app-routes/17/settings/REQUEST_TIMEOUT"},
		{name: "environment route retry certificate", command: newAppEnvironmentCommand, args: []string{"route", "retry-certificate", "17"}, method: http.MethodPost, path: "/v1/app-routes/17/actions/retry-certificate"},
		{name: "cert delete", command: rootCertCommand, args: []string{"delete", "18", "--yes"}, method: http.MethodDelete, path: "/v1/certs/18"},
	})
}

func TestGitRepoCommandIsRegistered(t *testing.T) {
	for _, cmd := range Commands() {
		if cmd.Name() == "git-repo" {
			return
		}
	}
	t.Fatal("git-repo command is not registered")
}

func rootRouteCommand() *cobra.Command {
	return newAppRouteCommand("route", []string{"routes"}, "Manage app routes", instanceFilterFlag)
}

func rootCertCommand() *cobra.Command {
	return newAppCertCommand("cert", []string{"certs"}, "Manage app certificates", instanceFilterFlag)
}
