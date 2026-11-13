package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStackOriginSyncAndGitUpdateFlags(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{
			name:     "preview sync origin sends only the preview options",
			command:  newStackCommand,
			args:     []string{"preview-sync-origin", "31", "--delete-services", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/stacks/31/actions/preview-sync-origin",
			body:     map[string]interface{}{"deleteStackServices": true},
			response: map[string]interface{}{"previousVersion": "1.0.0", "version": "1.1.0", "serviceUpdates": []interface{}{}, "changes": []interface{}{}},
		},
		{
			name:     "preview sync origin without options",
			command:  newStackCommand,
			args:     []string{"preview-sync-origin", "31", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/stacks/31/actions/preview-sync-origin",
			noBody:   true,
			response: nil,
		},
		{
			name:     "sync origin may update stack services",
			command:  newStackCommand,
			args:     []string{"sync-origin", "31", "--update-services", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/stacks/31/actions/sync-origin",
			body:     map[string]interface{}{"updateStackServices": true},
			response: map[string]interface{}{"id": 31, "name": "drupal"},
		},
		{
			name:     "stack update from git keeps service versions",
			command:  newStackCommand,
			args:     []string{"update-from-git", "7", "--git-ref", "main", "--git-ref-type", "branch", "--service-updates", "keep", "-o", "json"},
			method:   http.MethodPost,
			path:     "/v1/stacks/7/actions/update-from-git",
			body:     map[string]interface{}{"gitRef": "main", "gitRefType": "branch", "serviceUpdates": "keep"},
			response: map[string]interface{}{"success": true},
		},
		{
			name:     "backup presets that apply to an app environment and a database",
			command:  newBackupPresetCommand,
			args:     []string{"list", "--applicable-app-environment", "21", "--applicable-database", "8", "-o", "json"},
			method:   http.MethodGet,
			path:     "/v1/backup-presets",
			query:    "applicableAppInstanceId=21&applicableDatabaseId=8",
			response: []interface{}{map[string]interface{}{"id": 3, "projectIds": []interface{}{5}}},
		},
	})
}

func TestStackPreviewSyncOriginTableOutput(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     []string
	}{
		{
			name: "updates, error and changes",
			response: `{"previousVersion":"1.0.0","version":"1.1.0","error":"stack services need newer versions",
				"serviceUpdates":[{"stackServiceId":12,"name":"php","title":"PHP","serviceTitle":"PHP","previousVersion":"8.3","version":"8.4","reason":"option 3","refusal":null}],
				"changes":[{"sequenceId":1,"level":"info","message":"Add stack service redis"}]}`,
			want: []string{"Version: 1.0.0 -> 1.1.0", "Error: stack services need newer versions", "--update-services", "php", "option 3", "Changes:", "Add stack service redis"},
		},
		{
			name:     "nothing to change",
			response: `{"previousVersion":"0.4.0","version":"0.4.0","error":null,"serviceUpdates":[],"changes":[]}`,
			want:     []string{"Version: 0.4.0 -> 0.4.0", "The sync would change nothing."},
		},
		{
			name:     "already synced",
			response: `null`,
			want:     []string{"already follows the origin's latest revision"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var raw json.RawMessage = []byte(test.response)
				_ = json.NewEncoder(w).Encode(raw)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			var out bytes.Buffer
			cmd := newStackCommand()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"preview-sync-origin", "31"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\n%s", err, out.String())
			}
			for _, want := range test.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}
