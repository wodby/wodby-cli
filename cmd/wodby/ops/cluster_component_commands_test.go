package ops

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// clusterComponentTestCommand returns a cluster command holding the component and GPU node pool commands.
func clusterComponentTestCommand() *cobra.Command {
	return withOutputParentCommand("cluster", newClusterComponentCommand, newClusterGPUNodePoolCommand)
}

func TestClusterComponentCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{
			name:    "list",
			command: clusterComponentTestCommand,
			args:    []string{"component", "list", "3"},
			method:  http.MethodGet,
			path:    "/v1/cluster-components/3",
			response: []interface{}{map[string]interface{}{
				"name": "cilium", "version": "1.16.1", "source": "pod", "manager": "wodby", "namespace": "kube-system",
				"workload": "cilium", "count": 3, "observedAt": "2026-11-01T10:00:00Z", "status": "update_available",
				"latestVersion": "1.16.5", "updatePath": "component_update",
			}},
			wantInOut: "1.16.5",
		},
		{
			name:    "updates",
			command: clusterComponentTestCommand,
			args:    []string{"components", "updates", "3"},
			method:  http.MethodGet,
			path:    "/v1/cluster-component-updates/3",
			response: []interface{}{map[string]interface{}{
				"kind": "cilium", "name": "cilium", "currentVersion": "1.16.1", "targetVersion": "1.16.5", "components": []interface{}{"cilium"},
			}},
			wantInOut: "1.16.5",
		},
		{
			name:     "update from the name",
			command:  clusterComponentTestCommand,
			args:     []string{"component", "update", "3", "cilium", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/3/actions/update-component",
			body:     map[string]interface{}{"name": "cilium"},
			response: map[string]interface{}{"success": true, "taskId": 77},
		},
		{
			name:     "update from raw body",
			command:  clusterComponentTestCommand,
			args:     []string{"component", "update", "3", "--data", `{"name":"coredns"}`, "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/3/actions/update-component",
			body:     map[string]interface{}{"name": "coredns"},
			response: map[string]interface{}{"success": true, "taskId": 77},
		},
		{
			name:     "remove GPU node pool",
			command:  clusterComponentTestCommand,
			args:     []string{"gpu-node-pool", "3", "--remove", "--yes", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/3/actions/gpu-node-pool",
			body:     map[string]interface{}{"gpuNodePool": nil},
			response: map[string]interface{}{"success": true, "taskId": 78},
		},
		{
			name:     "GPU node pool from raw body",
			command:  clusterComponentTestCommand,
			args:     []string{"gpu-node-pool", "3", "--data", `{"gpuNodePool":{"machineType":"g2","minNodeCount":1,"maxNodeCount":2}}`, "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/clusters/3/actions/gpu-node-pool",
			body:     map[string]interface{}{"gpuNodePool": map[string]interface{}{"machineType": "g2", "minNodeCount": float64(1), "maxNodeCount": float64(2)}},
			response: map[string]interface{}{"success": true, "taskId": 78},
		},
	})
}

// clusterGPUNodePoolTestServer serves cluster 3 with the given GPU node pool and records the body of the pool update.
func clusterGPUNodePoolTestServer(t *testing.T, pool interface{}, posted *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters/3":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 3, "name": "main", "gpuNodePool": pool})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/clusters/3/actions/gpu-node-pool":
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, posted); err != nil {
				t.Errorf("decode body %q: %v", raw, err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": 78})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

// runClusterComponentTestCommand runs the cluster component and GPU node pool commands with args.
func runClusterComponentTestCommand(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := clusterComponentTestCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestClusterGPUNodePoolKeepsUnsetValues(t *testing.T) {
	var posted map[string]interface{}
	server := clusterGPUNodePoolTestServer(t, map[string]interface{}{"machineType": "g1", "minNodeCount": 0, "maxNodeCount": 2, "nodeDiskSize": nil}, &posted)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	if output, err := runClusterComponentTestCommand(t, "", "gpu-node-pool", "3", "--max", "4", "-o", "json"); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	want := map[string]interface{}{"gpuNodePool": map[string]interface{}{"machineType": "g1", "minNodeCount": float64(0), "maxNodeCount": float64(4)}}
	if !reflect.DeepEqual(posted, want) {
		t.Fatalf("POST body = %#v, want %#v", posted, want)
	}
}

func TestClusterGPUNodePoolNewPoolNeedsEveryValue(t *testing.T) {
	var posted map[string]interface{}
	server := clusterGPUNodePoolTestServer(t, nil, &posted)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	_, err := runClusterComponentTestCommand(t, "", "gpu-node-pool", "3", "--machine-type", "g1", "--max", "2")
	if err == nil || !strings.Contains(err.Error(), "--min is required") {
		t.Fatalf("Execute() error = %v", err)
	}
	if output, err := runClusterComponentTestCommand(t, "", "gpu-node-pool", "3", "--machine-type", "g1", "--min", "0", "--max", "2", "--disk-size", "100", "-o", "json"); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	want := map[string]interface{}{"gpuNodePool": map[string]interface{}{"machineType": "g1", "minNodeCount": float64(0), "maxNodeCount": float64(2), "nodeDiskSize": float64(100)}}
	if !reflect.DeepEqual(posted, want) {
		t.Fatalf("POST body = %#v, want %#v", posted, want)
	}
}

func TestClusterGPUNodePoolRejectsBadInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	for _, test := range []struct {
		args  []string
		stdin string
		want  string
	}{
		{args: []string{"gpu-node-pool", "3"}, want: "pass pool values, --remove or --data/--file"},
		{args: []string{"gpu-node-pool", "3", "--remove", "--max", "2"}, want: "--remove cannot be combined"},
		{args: []string{"gpu-node-pool", "3", "--remove"}, stdin: "n\n", want: "aborted"},
		{args: []string{"component", "update", "3"}, want: "update NAME is required"},
		{args: []string{"component", "update", "3", "cilium", "--data", `{"name":"x"}`}, want: "not both"},
	} {
		if _, err := runClusterComponentTestCommand(t, test.stdin, test.args...); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}
