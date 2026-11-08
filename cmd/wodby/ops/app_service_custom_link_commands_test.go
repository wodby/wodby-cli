package ops

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// appServiceCustomLinkTestCommand returns an app service command holding the custom link and link candidate commands.
func appServiceCustomLinkTestCommand() *cobra.Command {
	return withOutputParentCommand("aps", newAppServiceCustomLinkCommand, newAppServiceLinkCandidatesCommand)
}

func TestAppServiceCustomLinkCommands(t *testing.T) {
	runRESTRouteCases(t, []restRouteCase{
		{
			name:    "list",
			command: appServiceCustomLinkTestCommand,
			args:    []string{"custom-link", "list", "5"},
			method:  http.MethodGet,
			path:    "/v1/app-services/5/custom-links",
			response: []interface{}{map[string]interface{}{
				"appServiceId": 5,
				"name":         "db",
				"values": []interface{}{
					map[string]interface{}{"name": "host", "secret": false, "hasValue": true, "value": "db.example.com"},
					map[string]interface{}{"name": "password", "secret": true, "hasValue": true, "value": nil},
				},
			}},
			wantInOut: "db.example.com",
		},
		{
			name:    "set from flags",
			command: appServiceCustomLinkTestCommand,
			args:    []string{"custom-links", "set", "5", "db", "--value", "host=db.example.com", "--value", "password=a=b", "-o", "json"},
			method:  http.MethodPut,
			path:    "/v1/app-services/5/custom-links/db",
			body: map[string]interface{}{"values": []interface{}{
				map[string]interface{}{"name": "host", "value": "db.example.com"},
				map[string]interface{}{"name": "password", "value": "a=b"},
			}},
		},
		{
			name:    "set from raw body",
			command: appServiceCustomLinkTestCommand,
			args:    []string{"custom-link", "set", "5", "db", "--data", `{"values":[{"name":"host","value":"h"}]}`, "-o", "json"},
			method:  http.MethodPut,
			path:    "/v1/app-services/5/custom-links/db",
			body:    map[string]interface{}{"values": []interface{}{map[string]interface{}{"name": "host", "value": "h"}}},
		},
		{
			name:    "delete",
			command: appServiceCustomLinkTestCommand,
			args:    []string{"custom-link", "delete", "5", "db", "--yes", "-o", "json"},
			method:  http.MethodDelete,
			path:    "/v1/app-services/5/custom-links/db",
			noBody:  true,
		},
		{
			name:    "link candidates",
			command: appServiceCustomLinkTestCommand,
			args:    []string{"link-candidates", "5", "db"},
			method:  http.MethodGet,
			path:    "/v1/app-services/5/links/db/candidates",
			response: []interface{}{map[string]interface{}{
				"appServiceId": 812, "appServiceName": "mariadb", "appServiceTitle": "MariaDB", "crossApp": true,
				"appId": 4, "appTitle": "Shared data", "appInstanceId": 9, "appInstanceTitle": "Production",
			}},
			wantInOut: "812",
		},
	})
}

func TestAppServiceCustomLinkListShowsSecretRows(t *testing.T) {
	rows := appServiceCustomLinkValueRows([]interface{}{map[string]interface{}{
		"name": "db",
		"values": []interface{}{
			map[string]interface{}{"name": "password", "secret": true, "hasValue": true, "value": nil},
		},
	}})
	if len(rows) != 1 || rows[0]["link"] != "db" || rows[0]["name"] != "password" || rows[0]["secret"] != true {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestAppServiceCustomLinkWritesNeedValuesAndConfirmation(t *testing.T) {
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
		{args: []string{"custom-link", "set", "5", "db"}, want: "--value is required"},
		{args: []string{"custom-link", "set", "5", "db", "--value", "host"}, want: "expected NAME=VALUE"},
		{args: []string{"custom-link", "delete", "5", "db"}, stdin: "n\n", want: "aborted"},
	} {
		var out bytes.Buffer
		cmd := appServiceCustomLinkTestCommand()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(test.stdin))
		cmd.SetArgs(test.args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}
