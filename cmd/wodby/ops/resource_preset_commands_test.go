package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// resourcePresetTestServer serves one organization with Wodby's "small" preset and a custom "jvm" preset, and records
// the bodies of writes.
func resourcePresetTestServer(t *testing.T, writes map[string]map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]interface{}{map[string]interface{}{"id": 3, "name": "acme"}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource-presets":
			if r.URL.Query().Get("orgId") != "3" {
				t.Errorf("orgId = %q, want 3", r.URL.Query().Get("orgId"))
			}
			_ = json.NewEncoder(w).Encode([]interface{}{
				map[string]interface{}{"name": "small", "title": "Small", "system": true, "requestCPU": 50, "requestMem": 128},
				map[string]interface{}{"id": 9, "name": "jvm", "title": "JVM", "system": false, "requestCPU": 100, "requestMem": 512, "limitMem": 1024},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource-presets",
			r.Method == http.MethodPut && r.URL.Path == "/v1/resource-presets/9":
			body := map[string]interface{}{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writes[r.Method] = body
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "name": body["name"], "title": body["title"], "system": false})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/resource-presets/9":
			writes[r.Method] = map[string]interface{}{}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func runResourcePresetCommand(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newResourcePresetCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestResourcePresetCommands(t *testing.T) {
	writes := map[string]map[string]interface{}{}
	server := resourcePresetTestServer(t, writes)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	listed := runResourcePresetCommand(t, "list")
	for _, want := range []string{"small", "Small", "jvm", "1024"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("list output misses %q:\n%s", want, listed)
		}
	}

	runResourcePresetCommand(t, "create", "--name", "tiny", "--title", "Tiny", "--request-mem", "48", "-o", "json")
	want := map[string]interface{}{"orgId": float64(3), "name": "tiny", "title": "Tiny", "requestMem": float64(48)}
	if !reflect.DeepEqual(writes[http.MethodPost], want) {
		t.Fatalf("POST body = %#v, want %#v", writes[http.MethodPost], want)
	}

	// An update keeps the current name, title and values for flags left out.
	runResourcePresetCommand(t, "update", "9", "--limit-mem", "2048", "-o", "json")
	want = map[string]interface{}{"name": "jvm", "title": "JVM", "requestCPU": float64(100), "requestMem": float64(512), "limitMem": float64(2048)}
	if !reflect.DeepEqual(writes[http.MethodPut], want) {
		t.Fatalf("PUT body = %#v, want %#v", writes[http.MethodPut], want)
	}

	runResourcePresetCommand(t, "delete", "9", "--yes", "-o", "json")
	if _, ok := writes[http.MethodDelete]; !ok {
		t.Fatal("delete sent no request")
	}
}

func TestResourcePresetUpdateRefusesAPresetTheOrgDoesNotOwn(t *testing.T) {
	writes := map[string]map[string]interface{}{}
	server := resourcePresetTestServer(t, writes)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newResourcePresetCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"update", "5", "--title", "Other"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "resource preset 5 is not a custom preset of organization 3") {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(writes) != 0 {
		t.Fatalf("unexpected writes %#v", writes)
	}
}
