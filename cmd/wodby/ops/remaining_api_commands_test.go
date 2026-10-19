package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestClusterOperationCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{name: "update kubernetes version", command: newClusterCommand, args: []string{"update-kubernetes-version", "2", "--version", "1.31", "-o", "json"},
			method: http.MethodPost, path: "/v1/clusters/2/actions/update-kubernetes-version", body: map[string]interface{}{"version": "1.31"}},
		{name: "scale", command: newClusterCommand, args: []string{"scale", "2", "--min-nodes", "1", "--max-nodes", "3", "-o", "json"},
			method: http.MethodPost, path: "/v1/clusters/2/actions/scale", body: map[string]interface{}{"minNodeCount": float64(1), "maxNodeCount": float64(3)}},
		{name: "recycle nodes", command: newClusterCommand, args: []string{"recycle-nodes", "2", "--yes", "-o", "json"},
			method: http.MethodPost, path: "/v1/clusters/2/actions/recycle-nodes", noBody: true},
		{name: "update k3s public ip", command: newClusterCommand, args: []string{"update-k3s-public-ip", "2", "--ip", "203.0.113.5"},
			method: http.MethodPost, path: "/v1/clusters/2/actions/update-k3s-public-ip", body: map[string]interface{}{"publicIp": "203.0.113.5"}},
		{name: "create k3s", command: newClusterCommand, args: []string{"create-k3s", "--org", "1", "--name", "edge", "--title", "Edge", "--available-to-all-projects"},
			method: http.MethodPost, path: "/v1/clusters/actions/create-k3s",
			body:     map[string]interface{}{"orgId": float64(1), "name": "edge", "title": "Edge", "disableMonitoring": false, "availableToAllProjects": true},
			response: map[string]interface{}{"id": 5, "name": "edge", "status": "pending"}, wantInOut: "edge"},
		{name: "create wodby cloud", command: newClusterCommand, args: []string{"create-wodby-cloud", "--org", "1", "--region", "fra1", "--machine-type", "s-2", "--max-nodes", "3", "-o", "json"},
			method: http.MethodPost, path: "/v1/clusters/actions/create-wodby-cloud",
			body:     map[string]interface{}{"orgId": float64(1), "demo": false, "region": "fra1", "machineType": "s-2", "maxNodeCount": float64(3)},
			response: map[string]interface{}{"cluster": map[string]interface{}{"id": 5, "status": "pending"}, "taskId": 9}, wantInOut: `"taskId": 9`},
		{name: "k3s install command", command: newClusterCommand, args: []string{"k3s-install-command", "2"},
			method: http.MethodGet, path: "/v1/clusters/k3s-install-command/2", response: map[string]interface{}{"command": "curl -sfL https://example.com/install | sh -"}, wantInOut: "curl -sfL"},
		{name: "machine types", command: newClusterCommand, args: []string{"machine-types"},
			method: http.MethodGet, path: "/v1/wodby-cloud/machine-types", response: []interface{}{map[string]interface{}{"name": "s-2", "title": "Small"}}, wantInOut: "Small"},
	})
}

func TestStackServiceProviderCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{name: "stack create", command: newStackCommand, args: []string{"create", "--data", `{"orgId":1,"name":"blog","title":"Blog","services":[]}`},
			method: http.MethodPost, path: "/v1/stacks", body: map[string]interface{}{"orgId": float64(1), "name": "blog", "title": "Blog", "services": []interface{}{}}},
		{name: "stack discard draft", command: newStackCommand, args: []string{"discard-draft", "4", "--yes", "-o", "json"},
			method: http.MethodPost, path: "/v1/stacks/4/actions/discard-draft", noBody: true},
		{name: "infra stacks", command: newStackCommand, args: []string{"infra", "--provider", "aws"},
			method: http.MethodGet, path: "/v1/catalog/infra-stacks", query: "provider=aws", response: []interface{}{map[string]interface{}{"id": 1, "name": "eks-infra"}}, wantInOut: "eks-infra"},
		{name: "helm value list", command: newStackCommand, args: []string{"helm-value", "list", "8"},
			method: http.MethodGet, path: "/v1/stack-revisions/8/helm-values", response: []interface{}{}},
		{name: "helm value create", command: newStackCommand, args: []string{"helm-value", "create", "4", "--name", "ingress.class", "--value", "nginx"},
			method: http.MethodPost, path: "/v1/stacks/4/configuration/helm-values", body: map[string]interface{}{"name": "ingress.class", "value": "nginx", "secret": false}},
		{name: "helm value update", command: newStackCommand, args: []string{"helm-value", "update", "6", "--value", "traefik", "--secret"},
			method: http.MethodPut, path: "/v1/stack-helm-values/6", body: map[string]interface{}{"value": "traefik", "secret": true}},
		{name: "helm value delete", command: newStackCommand, args: []string{"helm-value", "delete", "6", "--yes"},
			method: http.MethodDelete, path: "/v1/stack-helm-values/6"},
		{name: "token create", command: newStackCommand, args: []string{"token", "create", "4", "--name", "HASH_SALT", "--regex", "[a-z]{32}", "--secret", "--env-type", "prod"},
			method: http.MethodPost, path: "/v1/stacks/4/configuration/tokens", body: map[string]interface{}{"name": "HASH_SALT", "regex": "[a-z]{32}", "secret": true, "envType": "prod"}},
		{name: "token update", command: newStackCommand, args: []string{"token", "update", "7", "--value", "abc"},
			method: http.MethodPut, path: "/v1/stack-tokens/7", body: map[string]interface{}{"value": "abc", "secret": false}},
		{name: "token list", command: newStackCommand, args: []string{"tokens", "list", "8"},
			method: http.MethodGet, path: "/v1/stack-revisions/8/tokens", response: []interface{}{map[string]interface{}{"id": 7, "name": "HASH_SALT"}}, wantInOut: "HASH_SALT"},
		{name: "token delete", command: newStackCommand, args: []string{"token", "delete", "7", "--yes"},
			method: http.MethodDelete, path: "/v1/stack-tokens/7"},
		{name: "annotation create", command: newStackCommand, args: []string{"annotation", "create", "4", "--name", "team", "--value", "web"},
			method: http.MethodPost, path: "/v1/stacks/4/configuration/annotations", body: map[string]interface{}{"name": "team", "value": "web"}},
		{name: "annotation list", command: newStackCommand, args: []string{"annotation", "list", "8"},
			method: http.MethodGet, path: "/v1/stack-revisions/8/annotations", response: []interface{}{}},
		{name: "annotation delete", command: newStackCommand, args: []string{"annotation", "delete", "9", "--yes"},
			method: http.MethodDelete, path: "/v1/stack-annotations/9"},
		{name: "service delete", command: newServiceCommand, args: []string{"delete", "3", "--yes"},
			method: http.MethodDelete, path: "/v1/services/3"},
		{name: "service update from git", command: newServiceCommand, args: []string{"update-from-git", "3", "--git-ref", "1.2.0", "--git-ref-type", "tag", "-o", "json"},
			method: http.MethodPost, path: "/v1/services/3/actions/update-from-git", body: map[string]interface{}{"gitRef": "1.2.0", "gitRefType": "tag"}},
		{name: "inheriting services", command: newServiceCommand, args: []string{"inheriting", "php"},
			method: http.MethodGet, path: "/v1/services/by-name/php/inheriting-services", response: []interface{}{map[string]interface{}{"id": 4, "name": "drupal-php"}}, wantInOut: "drupal-php"},
		{name: "service configs", command: newServiceCommand, args: []string{"configs", "12", "--option-version", "8.3", "--platform", "linux/arm64"},
			method: http.MethodGet, path: "/v1/service-revisions/12/configs", query: "optionVersion=8.3&platform=linux%2Farm64",
			response: []interface{}{map[string]interface{}{"name": "php-ini", "filepath": "/usr/local/etc/php/php.ini", "status": "ready"}}, wantInOut: "php.ini"},
		{name: "provider update", command: newProviderCommand, args: []string{"update", "5", "--title", "Acme Cloud"},
			method: http.MethodPut, path: "/v1/providers/5", body: map[string]interface{}{"title": "Acme Cloud"}},
		{name: "provider delete", command: newProviderCommand, args: []string{"delete", "5", "--yes"},
			method: http.MethodDelete, path: "/v1/providers/5"},
	})
}

func TestEnvironmentResourceCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{name: "backup download url", command: newBackupCommand, args: []string{"download", "11"},
			method: http.MethodGet, path: "/v1/backups/11/download", response: map[string]interface{}{"url": "https://storage.example.com/b.tar.gz", "expiresAt": "2026-10-19T12:00:00Z"}, wantInOut: "storage.example.com"},
		{name: "environment backup download url", command: newAppEnvironmentCommand, args: []string{"backup", "download", "11", "-o", "json"},
			method: http.MethodGet, path: "/v1/backups/11/download", response: map[string]interface{}{"url": "https://storage.example.com/b.tar.gz"}, wantInOut: "storage.example.com"},
		{name: "app service pods", command: newTopLevelAppServiceCommand, args: []string{"pods", "21", "--all", "--workload", "web"},
			method: http.MethodGet, path: "/v1/app-services/21/pods", query: "mode=all&workload=web", response: []interface{}{map[string]interface{}{"name": "web-0", "phase": "Running"}}, wantInOut: "web-0"},
		{name: "environment service pods", command: newAppEnvironmentCommand, args: []string{"service", "pods", "21"},
			method: http.MethodGet, path: "/v1/app-services/21/pods", response: []interface{}{}},
		{name: "app service cron schedule get", command: newAppEnvironmentCommand, args: []string{"service", "cron-schedule", "get", "4"},
			method: http.MethodGet, path: "/v1/app-service-cron-schedules/4", response: map[string]interface{}{"id": 4, "name": "hourly"}, wantInOut: "hourly"},
		{name: "auth get", command: newAppEnvironmentCommand, args: []string{"auth", "get", "6"},
			method: http.MethodGet, path: "/v1/app-auths/6", response: map[string]interface{}{"id": 6, "login": "admin"}, wantInOut: "admin"},
		{name: "access get by id", command: newAppEnvironmentCommand, args: []string{"access", "get-by-id", "6"},
			method: http.MethodGet, path: "/v1/app-accesses/6"},
		{name: "port update", command: newTopLevelAppPortCommand, args: []string{"update", "31", "--public-port", "30080"},
			method: http.MethodPut, path: "/v1/app-ports/31", body: map[string]interface{}{"publicPort": float64(30080)}},
		{name: "port publish", command: newTopLevelAppPortCommand, args: []string{"publish", "31"},
			method: http.MethodPost, path: "/v1/app-ports/31/actions/publish", noBody: true},
		{name: "environment port publish with port", command: newAppEnvironmentCommand, args: []string{"port", "publish", "31", "--public-port", "30080"},
			method: http.MethodPost, path: "/v1/app-ports/31/actions/publish", body: map[string]interface{}{"publicPort": float64(30080)}},
		{name: "port unpublish", command: newAppEnvironmentCommand, args: []string{"port", "unpublish", "31"},
			method: http.MethodPost, path: "/v1/app-ports/31/actions/unpublish", noBody: true, response: map[string]interface{}{"id": 31, "name": "ssh", "private": true}, wantInOut: "ssh"},
		{name: "port settings list", command: newAppEnvironmentCommand, args: []string{"port", "settings", "list", "31"},
			method: http.MethodGet, path: "/v1/app-ports/31/settings", response: []interface{}{map[string]interface{}{"id": 1, "name": "IDLE_TIMEOUT", "value": "300"}}, wantInOut: "IDLE_TIMEOUT"},
		{name: "port setting set", command: newAppEnvironmentCommand, args: []string{"port", "settings", "set", "31", "IDLE_TIMEOUT", "--value", "300"},
			method: http.MethodPut, path: "/v1/app-ports/31/settings/IDLE_TIMEOUT", body: map[string]interface{}{"value": "300"}},
		{name: "port settings replace", command: newAppEnvironmentCommand, args: []string{"port", "settings", "replace", "31", "--setting", "IDLE_TIMEOUT=300", "--setting", "CONNECTION_LIMIT=50"},
			method: http.MethodPut, path: "/v1/app-ports/31/settings", response: []interface{}{},
			body: map[string]interface{}{"settings": []interface{}{
				map[string]interface{}{"name": "IDLE_TIMEOUT", "value": "300"},
				map[string]interface{}{"name": "CONNECTION_LIMIT", "value": "50"},
			}}},
		{name: "port setting delete", command: newAppEnvironmentCommand, args: []string{"port", "settings", "delete", "31", "IDLE_TIMEOUT", "--yes"},
			method: http.MethodDelete, path: "/v1/app-ports/31/settings/IDLE_TIMEOUT"},
		{name: "endpoint list", command: newAppEnvironmentCommand, args: []string{"endpoint", "list", "3"},
			method: http.MethodGet, path: "/v1/app-endpoints", query: "appEnvironmentId=3", response: []interface{}{map[string]interface{}{"id": 5, "name": "http"}}, wantInOut: "http"},
		{name: "endpoint get", command: newAppEnvironmentCommand, args: []string{"endpoints", "get", "5"},
			method: http.MethodGet, path: "/v1/app-endpoints/5"},
		{name: "annotation list", command: newAppEnvironmentCommand, args: []string{"annotation", "list", "3", "--type", "ingress"},
			method: http.MethodGet, path: "/v1/app-annotations", query: "appEnvironmentId=3&types=ingress", response: []interface{}{}},
		{name: "annotation get", command: newAppEnvironmentCommand, args: []string{"annotation", "get", "4"},
			method: http.MethodGet, path: "/v1/app-annotations/4"},
		{name: "annotation create", command: newAppEnvironmentCommand, args: []string{"annotation", "create", "3", "--name", "nginx.ingress.kubernetes.io/proxy-body-size", "--value", "50m", "--service", "21"},
			method: http.MethodPost, path: "/v1/app-annotations",
			body: map[string]interface{}{"appEnvironmentId": float64(3), "appServiceId": float64(21), "name": "nginx.ingress.kubernetes.io/proxy-body-size", "value": "50m", "type": "ingress"}},
		{name: "annotation update", command: newAppEnvironmentCommand, args: []string{"annotation", "update", "4", "--name", "nginx.ingress.kubernetes.io/proxy-body-size", "--value", "100m"},
			method: http.MethodPut, path: "/v1/app-annotations/4", body: map[string]interface{}{"name": "nginx.ingress.kubernetes.io/proxy-body-size", "value": "100m"}},
		{name: "annotation delete", command: newAppEnvironmentCommand, args: []string{"annotation", "delete", "4", "--yes"},
			method: http.MethodDelete, path: "/v1/app-annotations/4"},
	})
}

func TestOrgAdminCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{name: "member invite", command: newMemberCommand, args: []string{"invite", "--org", "1", "--email", "dev@example.com", "--role", "admin", "--team", "2", "--project", "3,4"},
			method: http.MethodPost, path: "/v1/org-memberships",
			body: map[string]interface{}{"orgId": float64(1), "email": "dev@example.com", "role": "admin", "teamIds": []interface{}{float64(2)}, "projectIds": []interface{}{float64(3), float64(4)}}},
		{name: "member update", command: newMemberCommand, args: []string{"update", "5", "--role", "owner"},
			method: http.MethodPut, path: "/v1/org-memberships/5", body: map[string]interface{}{"role": "owner"}},
		{name: "member remove", command: newMemberCommand, args: []string{"remove", "5", "--yes"},
			method: http.MethodDelete, path: "/v1/org-memberships/5"},
		{name: "project member list", command: newProjectCommand, args: []string{"member", "list", "3"},
			method: http.MethodGet, path: "/v1/project-memberships", query: "projectId=3", response: []interface{}{map[string]interface{}{"id": 8, "role": "write"}}, wantInOut: "write"},
		{name: "project member add", command: newProjectCommand, args: []string{"member", "add", "3", "--member", "5", "--team", "2", "--role", "write"},
			method: http.MethodPost, path: "/v1/project-memberships", response: []interface{}{},
			body: map[string]interface{}{"projectId": float64(3), "orgMembershipIds": []interface{}{float64(5)}, "teamIds": []interface{}{float64(2)}, "role": "write"}},
		{name: "project member update", command: newProjectCommand, args: []string{"members", "update", "8", "--role", "admin"},
			method: http.MethodPut, path: "/v1/project-memberships/8", body: map[string]interface{}{"role": "admin"}},
		{name: "project member remove", command: newProjectCommand, args: []string{"member", "remove", "8", "--yes"},
			method: http.MethodDelete, path: "/v1/project-memberships/8"},
		{name: "team list", command: newTeamCommand, args: []string{"list", "--org", "1"},
			method: http.MethodGet, path: "/v1/teams", query: "orgId=1", response: []interface{}{map[string]interface{}{"id": 2, "name": "devs"}}, wantInOut: "devs"},
		{name: "team get by name", command: newTeamCommand, args: []string{"get-by-name", "devs", "--org", "1"},
			method: http.MethodGet, path: "/v1/teams/by-name/devs", query: "orgId=1"},
		{name: "team create", command: newTeamCommand, args: []string{"create", "--org", "1", "--name", "devs", "--title", "Developers", "--member", "5=leader", "--member", "6=member"},
			method: http.MethodPost, path: "/v1/teams",
			body: map[string]interface{}{"orgId": float64(1), "name": "devs", "title": "Developers", "members": []interface{}{
				map[string]interface{}{"orgMembershipId": float64(5), "role": "leader"},
				map[string]interface{}{"orgMembershipId": float64(6), "role": "member"},
			}}},
		{name: "team update", command: newTeamCommand, args: []string{"update", "2", "--name", "devs", "--title", "Dev team", "--member", "5=member"},
			method: http.MethodPut, path: "/v1/teams/2", body: map[string]interface{}{"name": "devs", "title": "Dev team", "members": []interface{}{
				map[string]interface{}{"orgMembershipId": float64(5), "role": "member"},
			}}},
		{name: "team delete", command: newTeamCommand, args: []string{"delete", "2", "--yes"},
			method: http.MethodDelete, path: "/v1/teams/2"},
		{name: "sso provider list", command: newOrgCommand, args: []string{"sso-provider", "list", "--org", "1"},
			method: http.MethodGet, path: "/v1/org-sso-providers", query: "orgId=1", response: []interface{}{map[string]interface{}{"id": 3, "name": "okta"}}, wantInOut: "okta"},
		{name: "sso provider get", command: newOrgCommand, args: []string{"sso", "get", "3"},
			method: http.MethodGet, path: "/v1/org-sso-providers/3"},
		{name: "sso provider create", command: newOrgCommand, args: []string{"sso-provider", "create", "--data", `{"orgId":1,"name":"okta","issuerUrl":"https://example.okta.com","clientId":"abc","clientSecret":"s","enabled":true,"jitEnabled":false,"domains":["example.com"]}`},
			method: http.MethodPost, path: "/v1/org-sso-providers",
			body: map[string]interface{}{"orgId": float64(1), "name": "okta", "issuerUrl": "https://example.okta.com", "clientId": "abc", "clientSecret": "s", "enabled": true, "jitEnabled": false, "domains": []interface{}{"example.com"}}},
		{name: "sso provider update", command: newOrgCommand, args: []string{"sso-provider", "update", "3", "--data", `{"orgId":1,"name":"okta","enabled":false,"jitEnabled":false,"domains":[]}`},
			method: http.MethodPut, path: "/v1/org-sso-providers/3", body: map[string]interface{}{"orgId": float64(1), "name": "okta", "enabled": false, "jitEnabled": false, "domains": []interface{}{}}},
		{name: "sso provider delete", command: newOrgCommand, args: []string{"sso-provider", "delete", "3", "--yes"},
			method: http.MethodDelete, path: "/v1/org-sso-providers/3"},
		{name: "sso domain verify", command: newOrgCommand, args: []string{"sso-provider", "verify-domain", "3", "example.com"},
			method: http.MethodPost, path: "/v1/org-sso-providers/3/actions/verify-domain", body: map[string]interface{}{"domain": "example.com"},
			response: map[string]interface{}{"id": 1, "domain": "example.com", "verified": true}, wantInOut: "example.com"},
	})
}

func TestRemainingCommandInputErrors(t *testing.T) {
	tests := []struct {
		name    string
		command func() *cobra.Command
		args    []string
		want    string
	}{
		{name: "team member without role", command: newTeamCommand, args: []string{"create", "--org", "1", "--name", "devs", "--title", "Devs", "--member", "5"}, want: "invalid --member"},
		{name: "team member with unknown role", command: newTeamCommand, args: []string{"create", "--org", "1", "--name", "devs", "--title", "Devs", "--member", "5=owner"}, want: "invalid --member"},
		{name: "project member without member or team", command: newProjectCommand, args: []string{"member", "add", "3", "--role", "read"}, want: "pass --member or --team"},
		{name: "service configs without option version", command: newServiceCommand, args: []string{"configs", "12"}, want: "--option-version is required"},
		{name: "infra stacks without provider", command: newStackCommand, args: []string{"infra"}, want: "--provider is required"},
		{name: "invite without email", command: newMemberCommand, args: []string{"invite", "--org", "1", "--role", "member"}, want: "--email is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")
			var out bytes.Buffer
			cmd := test.command()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBackupDownloadSavesFile(t *testing.T) {
	var server *httptest.Server
	var fileRequestAuth string
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/backups/11/download":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"url": server.URL + "/files/backup.tar.gz", "expiresAt": "2026-10-19T12:00:00Z"})
		case "/files/backup.tar.gz":
			fileRequestAuth = r.Header.Get("X-API-KEY") + r.Header.Get("Authorization")
			_, _ = w.Write([]byte("backup-content"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	target := filepath.Join(t.TempDir(), "backup.tar.gz")
	var out bytes.Buffer
	cmd := newBackupCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"download", "11", "--save-to", target})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, out.String())
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "backup-content" {
		t.Fatalf("file content = %q", content)
	}
	if fileRequestAuth != "" {
		t.Fatalf("download request sent Wodby credentials: %q", fileRequestAuth)
	}
	if !strings.Contains(out.String(), "Saved 14 bytes") {
		t.Fatalf("output = %q", out.String())
	}
}

func newTopLevelAppServiceCommand() *cobra.Command {
	return newAppServiceCommand("aps", []string{"app-service", "app-services"}, "Manage app services", instanceFilterFlag)
}

func newTopLevelAppPortCommand() *cobra.Command {
	return newAppPortCommand("port", []string{"ports"}, "Manage app ports", instanceFilterFlag)
}

func TestTeamUpdateKeepsCurrentValues(t *testing.T) {
	var putBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/teams/2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 2, "name": "devs", "title": "Developers", "members": []interface{}{
				map[string]interface{}{"id": 40, "orgMembershipId": 5, "role": "leader"},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/teams/2":
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 2})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newTeamCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"update", "2", "--title", "Dev team"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, out.String())
	}
	want := map[string]interface{}{"name": "devs", "title": "Dev team", "members": []interface{}{
		map[string]interface{}{"orgMembershipId": float64(5), "role": "leader"},
	}}
	if !reflect.DeepEqual(putBody, want) {
		t.Fatalf("PUT body = %#v, want %#v", putBody, want)
	}
}
