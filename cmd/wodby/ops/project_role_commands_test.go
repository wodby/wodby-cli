package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestProjectCommand wraps the project role and permission commands in a project command with an --output flag,
// as the CLI registers them.
func newTestProjectCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "project"}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newProjectRoleCommand(), newProjectPermissionsCommand())
	return cmd
}

// projectRoleTestServer serves one organization with a custom role 7 and records the bodies of writes.
func projectRoleTestServer(t *testing.T, writes map[string]map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]interface{}{map[string]interface{}{"id": 3, "name": "acme"}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/project-roles":
			if r.URL.Query().Get("orgId") != "3" {
				t.Errorf("orgId = %q, want 3", r.URL.Query().Get("orgId"))
			}
			_ = json.NewEncoder(w).Encode([]interface{}{
				map[string]interface{}{"role": "read", "system": true, "name": "read", "title": "Read"},
				map[string]interface{}{"id": 7, "role": "custom", "system": false, "name": "deployer", "title": "Deployer", "membershipCount": 2},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/project-roles/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 7, "role": "custom", "name": "deployer", "title": "Deployer", "description": "Deploys apps",
				"projectPermissions": []interface{}{"project.view"},
				"envPermissions":     map[string]interface{}{"prod": []interface{}{"env.view", "env.logs", "env.deploy"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/project-permissions":
			_ = json.NewEncoder(w).Encode([]interface{}{
				map[string]interface{}{"name": "env.deploy", "title": "Deploy", "scope": "environment", "alwaysGranted": false},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/project-roles",
			r.Method == http.MethodPut && r.URL.Path == "/v1/project-roles/7":
			body := map[string]interface{}{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writes[r.Method] = body
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7, "role": "custom", "name": body["name"], "title": body["title"]})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/project-roles/7":
			writes[r.Method] = map[string]interface{}{}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

// runProjectRoleCommand runs a project subcommand and returns its output.
func runProjectRoleCommand(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newTestProjectCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestProjectRoleCommands(t *testing.T) {
	writes := map[string]map[string]interface{}{}
	server := projectRoleTestServer(t, writes)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	listed := runProjectRoleCommand(t, "role", "list")
	for _, want := range []string{"Read", "deployer", "Deployer"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("list output misses %q:\n%s", want, listed)
		}
	}
	if got := runProjectRoleCommand(t, "role", "get", "7"); !strings.Contains(got, "Deploys apps") {
		t.Fatalf("get output misses the description:\n%s", got)
	}
	if got := runProjectRoleCommand(t, "permissions"); !strings.Contains(got, "env.deploy") {
		t.Fatalf("permissions output misses env.deploy:\n%s", got)
	}

	runProjectRoleCommand(t, "role", "create", "--name", "qa", "--title", "QA", "--description", "Tests staging",
		"--project-permission", "project.view,app.view", "--env-permission", "staging=env.deploy", "--env-permission", "staging=env.shell", "--env-permission", "prod=", "-o", "json")
	want := map[string]interface{}{
		"orgId": float64(3), "name": "qa", "title": "QA", "description": "Tests staging",
		"projectPermissions": []interface{}{"project.view", "app.view"},
		"envPermissions":     map[string]interface{}{"staging": []interface{}{"env.deploy", "env.shell"}, "prod": []interface{}{}},
	}
	if !reflect.DeepEqual(writes[http.MethodPost], want) {
		t.Fatalf("POST body = %#v, want %#v", writes[http.MethodPost], want)
	}

	// A role created with only a name and title sends empty permission lists, which the API requires.
	runProjectRoleCommand(t, "role", "create", "--name", "viewer", "--title", "Viewer", "--org", "5", "-o", "json")
	want = map[string]interface{}{"orgId": float64(5), "name": "viewer", "title": "Viewer", "projectPermissions": []interface{}{}, "envPermissions": map[string]interface{}{}}
	if !reflect.DeepEqual(writes[http.MethodPost], want) {
		t.Fatalf("POST body = %#v, want %#v", writes[http.MethodPost], want)
	}

	// An update keeps the current name, description and permissions for flags left out.
	runProjectRoleCommand(t, "role", "update", "7", "--title", "Release manager", "--env-permission", "prod=env.deploy,env.backup", "-o", "json")
	want = map[string]interface{}{
		"name": "deployer", "title": "Release manager", "description": "Deploys apps",
		"projectPermissions": []interface{}{"project.view"},
		"envPermissions":     map[string]interface{}{"prod": []interface{}{"env.deploy", "env.backup"}},
	}
	if !reflect.DeepEqual(writes[http.MethodPut], want) {
		t.Fatalf("PUT body = %#v, want %#v", writes[http.MethodPut], want)
	}

	runProjectRoleCommand(t, "role", "update", "7", "--data", `{"name":"deployer","title":"Deployer","projectPermissions":[],"envPermissions":{}}`, "-o", "json")
	want = map[string]interface{}{"name": "deployer", "title": "Deployer", "projectPermissions": []interface{}{}, "envPermissions": map[string]interface{}{}}
	if !reflect.DeepEqual(writes[http.MethodPut], want) {
		t.Fatalf("raw PUT body = %#v, want %#v", writes[http.MethodPut], want)
	}

	runProjectRoleCommand(t, "role", "delete", "7", "--yes", "-o", "json")
	if _, ok := writes[http.MethodDelete]; !ok {
		t.Fatal("delete sent no request")
	}
}

func TestProjectRoleCreateRejectsAMalformedEnvPermission(t *testing.T) {
	writes := map[string]map[string]interface{}{}
	server := projectRoleTestServer(t, writes)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newTestProjectCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"role", "create", "--name", "qa", "--title", "QA", "--env-permission", "env.deploy"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "ENV_TYPE=PERMISSION") {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(writes) != 0 {
		t.Fatalf("unexpected writes %#v", writes)
	}
}
