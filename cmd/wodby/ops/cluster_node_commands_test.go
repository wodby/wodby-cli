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
		{
			name:    "list nodes",
			command: newClusterCommand,
			args:    []string{"nodes", "4"},
			method:  http.MethodGet,
			path:    "/v1/cluster-nodes/4",
			response: []interface{}{map[string]interface{}{
				"name": "pool-1-abc", "ready": true, "schedulable": true, "version": "v1.35.2", "machineType": "m6i.xlarge",
				"zone": "eu-central-1a", "gpuNodePool": false, "internalIp": "10.0.1.7",
				"capacity":  map[string]interface{}{"cpu": 4, "memory": 16624558080, "disk": 85899345920, "gpus": 0},
				"createdAt": "2026-11-12T10:00:00Z",
			}},
			wantInOut: "15.5 GiB",
		},
		{
			name:    "list nodes vertically with the zone and the disk",
			command: newClusterCommand,
			args:    []string{"node", "4", "-o", "vertical"},
			method:  http.MethodGet,
			path:    "/v1/cluster-nodes/4",
			response: []interface{}{map[string]interface{}{
				"name": "pool-1-abc", "ready": true, "schedulable": true, "version": "v1.35.2", "zone": "eu-central-1a", "gpuNodePool": false,
				"capacity": map[string]interface{}{"cpu": 4, "memory": 16624558080, "disk": 85899345920, "gpus": 0},
			}},
			wantInOut: "80 GiB",
		},
		{
			name:    "k3s private addresses",
			command: newClusterCommand,
			args:    []string{"k3s-private-addresses", "4"},
			method:  http.MethodGet,
			path:    "/v1/cluster-k3s-private-addresses/4",
			response: []interface{}{map[string]interface{}{
				"address": "10.0.0.2", "networkInterface": "enp7s0", "mtu": 1450, "persistent": true, "persistentSource": "systemd-networkd",
			}},
			wantInOut: "systemd-networkd",
		},
		{
			name:     "switch k3s server address",
			command:  newClusterCommand,
			args:     []string{"switch-k3s-server-address", "4", "--address", "10.0.0.2", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/switch-k3s-server-address",
			body:     map[string]interface{}{"address": "10.0.0.2"},
			response: map[string]interface{}{"success": true, "taskId": 84},
		},
		{
			name:     "force switch k3s server address",
			command:  newClusterCommand,
			args:     []string{"switch-k3s-server-address", "4", "--address", "10.0.0.2", "--force", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/switch-k3s-server-address",
			body:     map[string]interface{}{"address": "10.0.0.2", "force": true},
			response: map[string]interface{}{"success": true, "taskId": 84},
		},
		{
			name:     "switch k3s server address from raw body",
			command:  newClusterCommand,
			args:     []string{"switch-k3s-server-address", "4", "--data", `{"address":"10.0.0.3"}`, "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/switch-k3s-server-address",
			body:     map[string]interface{}{"address": "10.0.0.3"},
			response: map[string]interface{}{"success": true, "taskId": 84},
		},
		{
			name:     "update k3s max pods",
			command:  newClusterCommand,
			args:     []string{"update-k3s-max-pods", "4", "--max-pods", "150", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/4/actions/update-k3s-max-pods",
			body:     map[string]interface{}{"maxPods": float64(150)},
			response: map[string]interface{}{"success": true, "taskId": 85},
		},
	})
}

func TestK3SOperationCommandsRequireTheirValue(t *testing.T) {
	configureTestAPI(t, "http://127.0.0.1:1/v1")
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"switch-k3s-server-address", "4"}, want: "--address"},
		{args: []string{"update-k3s-max-pods", "4"}, want: "--max-pods"},
	} {
		cmd := newClusterCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(test.args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want one naming %s", test.args, err, test.want)
		}
	}
}

func TestFormatByteSize(t *testing.T) {
	for size, want := range map[float64]string{
		512:         "512 B",
		1536:        "1.5 KiB",
		268435456:   "256 MiB",
		16624558080: "15.5 GiB",
		85899345920: "80 GiB",
	} {
		if got := formatByteSize(size); got != want {
			t.Errorf("formatByteSize(%v) = %q, want %q", size, got, want)
		}
	}
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
