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

func TestCommandsForNewRESTRoutes(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{name: "cluster sharing get", command: newClusterCommand, args: []string{"sharing", "get", "2"}, method: http.MethodGet, path: "/v1/clusters/sharing/2",
			response: map[string]interface{}{"ownershipScope": "org", "availableToAllProjects": true, "sharedProjects": []interface{}{}}, wantInOut: "org"},
		{name: "workspace pull without scripts", command: newAppEnvironmentCommand, args: []string{"workspace", "pull", "3", "--no-scripts"},
			method: http.MethodPost, path: "/v1/workspaces/3/actions/pull", body: map[string]interface{}{"scripts": false}, response: map[string]interface{}{"id": 50, "status": "queued"}},
		{name: "workspace pull", command: newAppEnvironmentCommand, args: []string{"workspace", "pull", "3"},
			method: http.MethodPost, path: "/v1/workspaces/3/actions/pull", body: map[string]interface{}{"scripts": true}, response: map[string]interface{}{"id": 50}},
		{name: "workspace git diff", command: newAppEnvironmentCommand, args: []string{"workspace", "git-diff", "3", "web/index.php", "--original-path", "index.php"},
			method: http.MethodGet, path: "/v1/workspaces/3/git-diff", query: "originalPath=index.php&path=web%2Findex.php",
			response: map[string]interface{}{"available": true, "path": "web/index.php", "diff": "@@ -1 +1 @@\n-a\n+b\n"}, wantInOut: "+b"},
		{name: "environment migrate", command: newAppEnvironmentCommand, args: []string{"migrate", "3", "--target-cluster", "9", "--volume", "21=fast", "-o", "json"},
			method: http.MethodPost, path: "/v1/app-environments/3/actions/migrate",
			body:     map[string]interface{}{"targetClusterId": float64(9), "volumes": []interface{}{map[string]interface{}{"appServiceVolumeId": float64(21), "storageClassName": "fast"}}},
			response: map[string]interface{}{"success": true, "taskId": 70, "migration": map[string]interface{}{"id": 5, "status": "pending"}}, wantInOut: "pending"},
		{name: "environment migration list", command: newAppEnvironmentCommand, args: []string{"migration", "list", "3"},
			method: http.MethodGet, path: "/v1/app-environment-migrations", query: "appEnvironmentId=3", response: []interface{}{map[string]interface{}{"id": 5, "phase": "restoring_target"}}, wantInOut: "restoring_target"},
		{name: "environment migration get", command: newAppEnvironmentCommand, args: []string{"migration", "get", "5"}, method: http.MethodGet, path: "/v1/app-environment-migrations/5"},
		{name: "environment migration rollback", command: newAppEnvironmentCommand, args: []string{"migration", "rollback", "5", "--yes"},
			method: http.MethodPost, path: "/v1/app-environment-migrations/5/actions/rollback", noBody: true},
		{name: "environment migration cleanup", command: newAppEnvironmentCommand, args: []string{"migration", "cleanup", "5", "--yes"},
			method: http.MethodPost, path: "/v1/app-environment-migrations/5/actions/cleanup", noBody: true},
		{name: "environment default route settings", command: newAppEnvironmentCommand, args: []string{"route-settings", "list", "3"},
			method: http.MethodGet, path: "/v1/app-environments/route-settings/3", response: []interface{}{}},
		{name: "environment default route settings replace", command: newAppEnvironmentCommand, args: []string{"route-settings", "replace", "3", "--setting", "REQUEST_TIMEOUT=120", "--setting", "NO_INDEX=true"},
			method: http.MethodPut, path: "/v1/app-environments/route-settings/3",
			body: map[string]interface{}{"settings": []interface{}{map[string]interface{}{"name": "REQUEST_TIMEOUT", "value": "120"}, map[string]interface{}{"name": "NO_INDEX", "value": "true"}}}, response: []interface{}{}},
		{name: "environment default route settings clear", command: newAppEnvironmentCommand, args: []string{"route-settings", "replace", "3", "--none"},
			method: http.MethodPut, path: "/v1/app-environments/route-settings/3", body: map[string]interface{}{"settings": []interface{}{}}, response: []interface{}{}},
		{name: "route settings replace", command: rootRouteCommand, args: []string{"settings", "replace", "17", "--setting", "HSTS=true"},
			method: http.MethodPut, path: "/v1/app-routes/17/settings", body: map[string]interface{}{"settings": []interface{}{map[string]interface{}{"name": "HSTS", "value": "true"}}}, response: []interface{}{}},
		{name: "route bulk disable", command: rootRouteCommand, args: []string{"bulk", "3", "--routes", "17,18", "--operation", "disable"},
			method: http.MethodPost, path: "/v1/app-routes/actions/bulk", body: map[string]interface{}{"appEnvironmentId": float64(3), "routeIds": []interface{}{float64(17), float64(18)}, "operation": "disable"}},
		{name: "org settings", command: newOrgCommand, args: []string{"settings", "update", "1", "--build-image-retention", "three_months"},
			method: http.MethodPut, path: "/v1/orgs/1/settings", body: map[string]interface{}{"buildImageCleanup": map[string]interface{}{"retention": "three_months"}}},
		{name: "environment build image retention", command: newAppEnvironmentCommand, args: []string{"settings", "update", "3", "--build-image-retention", "one_year"},
			method: http.MethodPut, path: "/v1/app-environments/settings/3", body: map[string]interface{}{"buildImageCleanup": map[string]interface{}{"retention": "one_year"}}},
		{name: "build void images", command: newBuildCommand, args: []string{"void-images", "33", "--yes"}, method: http.MethodPost, path: "/v1/app-builds/33/actions/void-images", noBody: true,
			response: map[string]interface{}{"id": 33, "imageVoidStatus": "voided"}, wantInOut: "voided"},
		{name: "environment build void images", command: newAppEnvironmentCommand, args: []string{"build", "void-images", "33", "--yes"}, method: http.MethodPost, path: "/v1/app-builds/33/actions/void-images", noBody: true},
		{name: "stack ignore service revision", command: newStackCommand, args: []string{"ignore-service-revision", "4", "--stack-service", "12", "--service-revision", "99"},
			method: http.MethodPost, path: "/v1/stacks/4/actions/ignore-service-revision", body: map[string]interface{}{"stackServiceId": float64(12), "serviceRevId": float64(99)}},
		{name: "integration usages", command: newIntegrationCommand, args: []string{"usages", "8", "--page", "2", "--page-size", "10"},
			method: http.MethodGet, path: "/v1/integrations/usages/8", query: "page=2&pageSize=10",
			response: map[string]interface{}{"items": []interface{}{map[string]interface{}{"category": "current", "kind": "app_environment_ci", "target": map[string]interface{}{"type": "app_environment", "title": "Dev"}}}, "totalCount": 1}, wantInOut: "Dev"},
		{name: "cluster create available to all projects", command: newClusterCommand, args: []string{"create", "--org", "1", "--integration", "2", "--name", "prod", "--title", "Prod", "--available-to-all-projects"},
			method: http.MethodPost, path: "/v1/clusters"},
	})
}

func TestAvailableToAllProjectsReachesRequests(t *testing.T) {
	tests := []struct {
		name    string
		command func() *cobra.Command
		args    []string
		path    string
	}{
		{name: "cluster create", command: newClusterCommand, args: []string{"create", "--org", "1", "--integration", "2", "--name", "prod", "--title", "Prod", "--available-to-all-projects"}, path: "/v1/clusters"},
		{name: "integration create", command: newIntegrationCommand, args: []string{"create", "--org", "1", "--provider", "3", "--name", "git", "--title", "Git", "--kind", "git", "--available-to-all-projects"}, path: "/v1/integrations"},
		{name: "service import", command: newServiceCommand, args: []string{"import", "--org", "1", "--integration", "2", "--remote-git-repo", "acme/services", "--git-ref", "main", "--git-ref-type", "branch", "--available-to-all-projects"}, path: "/v1/services/actions/import"},
		{name: "stack import", command: newStackCommand, args: []string{"import", "--org", "1", "--integration", "2", "--remote-git-repo", "acme/stacks", "--git-ref", "main", "--git-ref-type", "branch", "--available-to-all-projects"}, path: "/v1/stacks/actions/import"},
		{name: "stack duplicate", command: newStackCommand, args: []string{"duplicate", "4", "--org", "1", "--available-to-all-projects"}, path: "/v1/stacks/4/actions/duplicate"},
		{name: "service create from manifest", command: newServiceCommand, args: []string{"create-from-manifest", "-", "--org", "1", "--available-to-all-projects"}, path: "/v1/services/actions/create-from-manifest"},
		{name: "stack validate manifest", command: newStackCommand, args: []string{"validate-manifest", "-", "--org", "1", "--available-to-all-projects"}, path: "/v1/stacks/actions/validate-manifest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotPath string
			var body map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode([]interface{}{map[string]interface{}{"id": 1}})
					return
				}
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&body)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 5, "valid": true})
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			var out bytes.Buffer
			cmd := test.command()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader("name: acme\n"))
			// JSON output does not follow the created resource's task.
			cmd.SetArgs(append(test.args, "-o", "json"))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\n%s", err, out.String())
			}
			if gotPath != test.path || body["availableToAllProjects"] != true {
				t.Fatalf("request %s body %#v, want %s with availableToAllProjects", gotPath, body, test.path)
			}
		})
	}

	// Providers cannot be available to all projects, so their import does not offer the flag.
	if flag := lookupCommandFlag(t, newProviderCommand(), []string{"import"}, availableToAllProjectsFlagName); flag {
		t.Fatal("provider import must not offer --available-to-all-projects")
	}
	if flag := lookupCommandFlag(t, newAppCommand(), []string{"sharing", "update"}, availableToAllProjectsFlagName); flag {
		t.Fatal("app sharing must not offer --available-to-all-projects")
	}
}

func TestSharingUpdateKeepsUnchangedValues(t *testing.T) {
	current := map[string]interface{}{
		"ownershipScope": "project", "ownerProjectId": 7, "availableToAllProjects": false,
		"sharedProjects": []interface{}{
			map[string]interface{}{"projectId": 7, "role": "owner"},
			map[string]interface{}{"projectId": 8, "role": "read"},
		},
	}
	tests := []struct {
		name string
		args []string
		want map[string]interface{}
	}{
		{
			name: "only the owner changes",
			args: []string{"--owner", "org"},
			want: map[string]interface{}{"ownershipScope": "org", "sharedProjects": []interface{}{
				map[string]interface{}{"projectId": float64(7), "access": "write"},
				map[string]interface{}{"projectId": float64(8), "access": "read"},
			}},
		},
		{
			name: "shares are replaced",
			args: []string{"--share", "9=write"},
			want: map[string]interface{}{"ownershipScope": "project", "ownerProjectId": float64(7), "sharedProjects": []interface{}{
				map[string]interface{}{"projectId": float64(9), "access": "write"},
			}},
		},
		{
			name: "all shares are removed and the resource opens to every project",
			args: []string{"--owner", "org", "--no-shares", "--available-to-all-projects"},
			want: map[string]interface{}{"ownershipScope": "org", "sharedProjects": []interface{}{}, "availableToAllProjects": true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var put map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/services/sharing/5" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if r.Method == http.MethodPut {
					_ = json.NewDecoder(r.Body).Decode(&put)
				}
				_ = json.NewEncoder(w).Encode(current)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			var out bytes.Buffer
			cmd := newServiceCommand()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"sharing", "update", "5"}, test.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\n%s", err, out.String())
			}
			if !reflect.DeepEqual(put, test.want) {
				t.Fatalf("PUT body = %#v, want %#v", put, test.want)
			}
		})
	}
}

func TestSharingUpdateRejectsInvalidShares(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			t.Error("no update may be sent")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ownershipScope": "org", "sharedProjects": []interface{}{}})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")
	for _, args := range [][]string{
		{"--share", "9=admin"},
		{"--share", "9=read", "--no-shares"},
		{"--owner", "project"},
	} {
		var out bytes.Buffer
		cmd := newClusterCommand()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"sharing", "update", "2"}, args...))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected an error for %v", args)
		}
	}
}

func lookupCommandFlag(t *testing.T, root *cobra.Command, path []string, flag string) bool {
	t.Helper()
	cmd, _, err := root.Find(path)
	if err != nil {
		t.Fatal(err)
	}
	return cmd.Flags().Lookup(flag) != nil
}
