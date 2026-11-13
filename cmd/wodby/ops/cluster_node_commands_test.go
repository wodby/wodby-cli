package ops

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClusterNodeAndAdvisoryCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{
			name:    "host checks",
			command: newClusterCommand,
			args:    []string{"host-checks", "4"},
			method:  http.MethodGet,
			path:    "/v1/cluster-host-checks/4",
			response: []interface{}{map[string]interface{}{
				"name": "mptcp_disabled", "title": "MPTCP disabled", "advisory": "CVE-2026-98432", "advisoryTitle": "Linux kernel TCP bug",
				"node": "server-1", "state": "protected", "message": "In place", "kernel": "6.8.0-45-generic",
			}},
			wantInOut: "MPTCP disabled",
		},
		{
			name:    "security advisories",
			command: newClusterCommand,
			args:    []string{"security-advisories", "4"},
			method:  http.MethodGet,
			path:    "/v1/cluster-security-advisories/4",
			response: []interface{}{map[string]interface{}{
				"advisory": "CVE-2026-98432", "title": "Linux kernel TCP bug", "state": "not_fixed", "action": "recycle_nodes", "message": "Recycle the nodes",
			}},
			wantInOut: "recycle_nodes",
		},
		{
			name:    "list k3s nodes",
			command: newClusterCommand,
			args:    []string{"k3s-node", "list", "4"},
			method:  http.MethodGet,
			path:    "/v1/cluster-k3s-nodes/4",
			response: []interface{}{map[string]interface{}{
				"name": "worker-1", "role": "agent", "ready": true, "schedulable": true, "version": "v1.36.6+k3s1",
				"internalIp": "10.0.0.3", "localVolumes": []interface{}{"app/data"}, "createdAt": "2026-11-12T10:00:00Z",
			}},
			wantInOut: "1 volume",
		},
		{
			name:      "k3s join command",
			command:   newClusterCommand,
			args:      []string{"k3s-node", "join-command", "4"},
			method:    http.MethodGet,
			path:      "/v1/clusters/k3s-join-command/4",
			response:  map[string]interface{}{"command": "curl -sfL https://example.test/join | sh -"},
			wantInOut: "curl -sfL https://example.test/join | sh -",
		},
		{
			name:     "remove k3s node",
			command:  newClusterCommand,
			args:     []string{"k3s-node", "remove", "4", "worker-1", "--yes", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/remove-k3s-node",
			body:     map[string]interface{}{"nodeName": "worker-1"},
			response: map[string]interface{}{"success": true, "taskId": 81},
		},
		{
			name:     "force remove k3s node after the prompt",
			command:  newClusterCommand,
			args:     []string{"k3s-node", "remove", "4", "worker-1", "--force", "-o", "json"},
			stdin:    "y\n",
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/remove-k3s-node",
			body:     map[string]interface{}{"nodeName": "worker-1", "force": true},
			response: map[string]interface{}{"success": true, "taskId": 82},
		},
		{
			name:     "remove k3s node from raw body",
			command:  newClusterCommand,
			args:     []string{"k3s-node", "remove", "4", "--data", `{"nodeName":"worker-2"}`, "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/remove-k3s-node",
			body:     map[string]interface{}{"nodeName": "worker-2"},
			response: map[string]interface{}{"success": true, "taskId": 83},
		},
	})
}

func TestK3SNodeRemoveRefusesWithoutConfirmation(t *testing.T) {
	configureTestAPI(t, "http://127.0.0.1:1/v1")
	cmd := newClusterCommand()
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"k3s-node", "remove", "4", "worker-1"})
	if err := cmd.Execute(); err == nil || err.Error() != "aborted" {
		t.Fatalf("Execute() error = %v, want aborted", err)
	}
}
