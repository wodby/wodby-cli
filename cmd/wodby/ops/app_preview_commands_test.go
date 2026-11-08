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

// withOutputParentCommand returns a parent command with the persistent --output flag, holding the given subcommands
// the way the app, app service and cluster commands hold them.
func withOutputParentCommand(use string, children ...func(outputOptions) *cobra.Command) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: use}
	addOutputFlag(cmd, &out)
	for _, child := range children {
		cmd.AddCommand(child(out))
	}
	return cmd
}

// appPreviewRequest is one request the app preview test server received.
type appPreviewRequest struct {
	method string
	path   string
	body   map[string]interface{}
}

// appPreviewTestServer serves app 7 with the given preview settings and records every request.
func appPreviewTestServer(t *testing.T, previewSettings map[string]interface{}, pullRequests map[string]interface{}, requests *[]appPreviewRequest) *httptest.Server {
	t.Helper()
	app := map[string]interface{}{
		"id":                7,
		"name":              "shop",
		"previewsAvailable": true,
		"settings":          map[string]interface{}{"previewEnvironments": previewSettings},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := appPreviewRequest{method: r.Method, path: r.URL.Path}
		if raw, _ := io.ReadAll(r.Body); len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &request.body); err != nil {
				t.Errorf("decode body %q: %v", raw, err)
			}
		}
		*requests = append(*requests, request)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/7":
			_ = json.NewEncoder(w).Encode(app)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/apps/settings/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7, "settings": request.body})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/preview-pull-requests/7":
			_ = json.NewEncoder(w).Encode(pullRequests)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps/preview-environments/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": 99})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps/preview-password/regenerate/7":
			_ = json.NewEncoder(w).Encode(app)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

// runAppPreviewTestCommand runs the app settings and preview commands with args and returns the output and error.
func runAppPreviewTestCommand(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := withOutputParentCommand("app", newAppSettingsCommand, newAppPreviewCommand)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// storedAppPreviewSettings are preview settings as the API returns them for an app with previews on.
func storedAppPreviewSettings() map[string]interface{} {
	return map[string]interface{}{
		"enabled":              true,
		"baseAppEnvironmentId": 11,
		"environmentType":      "test",
		"imports":              []interface{}{map[string]interface{}{"appServiceName": "mariadb", "importName": "db", "backupName": "nightly"}},
		"buildDrafts":          false,
		"runCronJobs":          false,
		"maxConcurrent":        3,
		"idleTtlDays":          7,
		"basicAuth":            true,
		"basicAuthUsername":    "preview",
	}
}

func TestAppSettingsUpdateKeepsUnsetPreviewSettings(t *testing.T) {
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	output, err := runAppPreviewTestCommand(t, "", "settings", "update", "7", "--preview-max", "5", "--preview-build-drafts",
		"--preview-import", "php=files=weekly", "--preview-import", "mariadb=db=nightly", "-o", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodPut {
		t.Fatalf("requests = %#v, want GET then PUT", requests)
	}
	want := map[string]interface{}{"previewEnvironments": map[string]interface{}{
		"enabled":              true,
		"baseAppEnvironmentId": float64(11),
		"environmentType":      "test",
		"imports": []interface{}{
			map[string]interface{}{"appServiceName": "php", "importName": "files", "backupName": "weekly"},
			map[string]interface{}{"appServiceName": "mariadb", "importName": "db", "backupName": "nightly"},
		},
		"buildDrafts":   true,
		"runCronJobs":   false,
		"maxConcurrent": float64(5),
		"idleTtlDays":   float64(7),
		"basicAuth":     true,
	}}
	if !reflect.DeepEqual(requests[1].body, want) {
		t.Fatalf("PUT body = %#v, want %#v", requests[1].body, want)
	}
}

func TestAppSettingsUpdateTurnsPreviewsOffAndClearsImports(t *testing.T) {
	stored := storedAppPreviewSettings()
	stored["baseAppEnvironmentId"] = nil
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, stored, nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	output, err := runAppPreviewTestCommand(t, "", "settings", "update", "7", "--previews=false", "--clear-preview-imports", "--preview-basic-auth=false")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	settings, _ := requests[len(requests)-1].body["previewEnvironments"].(map[string]interface{})
	if settings["enabled"] != false || settings["basicAuth"] != false || !reflect.DeepEqual(settings["imports"], []interface{}{}) {
		t.Fatalf("PUT settings = %#v", settings)
	}
	if _, ok := settings["baseAppEnvironmentId"]; ok {
		t.Fatalf("PUT settings send a null base environment: %#v", settings)
	}
	if _, ok := settings["basicAuthUsername"]; ok {
		t.Fatalf("PUT settings send the read-only basic auth username: %#v", settings)
	}
	// The table shows the updated settings.
	if !strings.Contains(output, "enabled:") || !strings.Contains(output, "false") {
		t.Fatalf("output misses the settings:\n%s", output)
	}
}

func TestAppSettingsUpdateSendsRawBodyAsIs(t *testing.T) {
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	output, err := runAppPreviewTestCommand(t, "", "settings", "update", "7", "--data", `{"previewEnvironments":{"enabled":false}}`, "-o", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	want := []appPreviewRequest{{method: http.MethodPut, path: "/v1/apps/settings/7", body: map[string]interface{}{"previewEnvironments": map[string]interface{}{"enabled": false}}}}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestAppSettingsUpdateRejectsBadInput(t *testing.T) {
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"settings", "update", "7"}, want: "pass at least one update flag"},
		{args: []string{"settings", "update", "7", "--preview-import", "php=files"}, want: "expected APP_SERVICE=IMPORT=BACKUP"},
		{args: []string{"settings", "update", "7", "--preview-import", "php=files=weekly", "--clear-preview-imports"}, want: "not both"},
	} {
		_, err := runAppPreviewTestCommand(t, "", test.args...)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
	if len(requests) != 0 {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestAppSettingsGetShowsPreviewSettings(t *testing.T) {
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	output, err := runAppPreviewTestCommand(t, "", "settings", "get", "7")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	for _, want := range []string{"mariadb=db=nightly", "preview", "previews available:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output misses %q:\n%s", want, output)
		}
	}

	output, err = runAppPreviewTestCommand(t, "", "settings", "get", "7", "-o", "json")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(output), &settings); err != nil {
		t.Fatalf("decode %q: %v", output, err)
	}
	if _, ok := settings["previewEnvironments"]; !ok {
		t.Fatalf("JSON output = %s, want the settings object", output)
	}
}

func TestAppPreviewPullRequests(t *testing.T) {
	pullRequests := map[string]interface{}{
		"configured": true,
		"pullRequests": []interface{}{
			map[string]interface{}{"number": 42, "title": "Add checkout", "author": "octocat", "headRef": "checkout", "draft": false, "fromFork": false, "hasPreview": true, "previewAppEnvironmentId": 31},
			map[string]interface{}{"number": 43, "title": "Fork change", "author": "someone", "headRef": "patch-1", "draft": false, "fromFork": true, "hasPreview": false, "skipReason": "pull request from a fork"},
		},
	}
	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), pullRequests, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	output, err := runAppPreviewTestCommand(t, "", "preview", "pull-requests", "7")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	for _, want := range []string{"Add checkout", "octocat", "31", "pull request from a fork"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output misses %q:\n%s", want, output)
		}
	}
	if requests[0].method != http.MethodGet || requests[0].path != "/v1/apps/preview-pull-requests/7" {
		t.Fatalf("request = %#v", requests[0])
	}

	pullRequests["configured"] = false
	pullRequests["pullRequests"] = []interface{}{}
	output, err = runAppPreviewTestCommand(t, "", "preview", "pull-requests", "7")
	if err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "Preview environments are not configured for app 7") {
		t.Fatalf("output misses the not-configured note:\n%s", output)
	}
}

func TestAppPreviewCreateAndRegeneratePassword(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{
			name:     "create from flags",
			command:  func() *cobra.Command { return withOutputParentCommand("app", newAppPreviewCommand) },
			args:     []string{"preview", "create", "7", "--pull-request", "42", "--repo", "github:1", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/apps/preview-environments/7",
			body:     map[string]interface{}{"number": float64(42), "remoteGitRepoId": "github:1"},
			response: map[string]interface{}{"success": true, "taskId": 99},
		},
		{
			name:     "create from raw body",
			command:  func() *cobra.Command { return withOutputParentCommand("app", newAppPreviewCommand) },
			args:     []string{"preview", "create", "7", "--data", `{"number":43}`, "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/apps/preview-environments/7",
			body:     map[string]interface{}{"number": float64(43)},
			response: map[string]interface{}{"success": true, "taskId": 99},
		},
		{
			name:      "regenerate password",
			command:   func() *cobra.Command { return withOutputParentCommand("app", newAppPreviewCommand) },
			args:      []string{"preview", "regenerate-password", "7", "--yes"},
			method:    http.MethodPost,
			path:      "/v1/apps/preview-password/regenerate/7",
			noBody:    true,
			response:  map[string]interface{}{"id": 7, "settings": map[string]interface{}{"previewEnvironments": storedAppPreviewSettings()}},
			wantInOut: "preview",
		},
	})

	var requests []appPreviewRequest
	server := appPreviewTestServer(t, storedAppPreviewSettings(), nil, &requests)
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")
	if _, err := runAppPreviewTestCommand(t, "", "preview", "create", "7"); err == nil || !strings.Contains(err.Error(), "--pull-request is required") {
		t.Fatalf("create without --pull-request error = %v", err)
	}
	if _, err := runAppPreviewTestCommand(t, "n\n", "preview", "regenerate-password", "7"); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("declined regenerate-password error = %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("unexpected requests %#v", requests)
	}
}
