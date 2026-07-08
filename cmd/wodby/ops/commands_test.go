package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestCommandsExposeTopLevelOperationalSurface(t *testing.T) {
	cmds := Commands()
	names := make(map[string]bool)
	for _, cmd := range cmds {
		names[cmd.Name()] = true
	}

	for _, name := range []string{
		"user",
		"org",
		"member",
		"project",
		"env",
		"database",
		"cluster",
		"integration",
		"provider",
		"helm",
		"stack",
		"service",
		"app",
		"instance",
		"aps",
		"route",
		"port",
		"cert",
		"build",
		"deployment",
		"backup",
		"import",
		"task",
	} {
		if !names[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}

func TestListSubcommandMustBeExplicit(t *testing.T) {
	for _, test := range []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{name: "app", cmd: newAppCommand()},
		{name: "task", cmd: newTaskCommand()},
		{name: "instance build", cmd: newAppInstanceCommand("instance", "Manage app instances"), args: []string{"build"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.cmd.SetOut(io.Discard)
			test.cmd.SetErr(io.Discard)
			test.cmd.SetArgs(test.args)
			err := test.cmd.Execute()
			if err == nil {
				t.Fatal("Execute() error = nil, want explicit subcommand error")
			}
			if !strings.Contains(err.Error(), `use "list" explicitly`) {
				t.Fatalf("Execute() error = %q, want explicit list error", err)
			}
		})
	}
}

func TestDefaultTableColumnsOmitOrgID(t *testing.T) {
	for name, columns := range map[string][]string{
		"project":      projectColumns,
		"env":          envColumns,
		"database":     databaseColumns,
		"cluster":      clusterColumns,
		"integration":  integrationColumns,
		"member":       memberColumns,
		"provider":     providerColumns,
		"stack":        stackColumns,
		"service":      catalogServiceColumns,
		"databaseDb":   databaseDbColumns,
		"databaseUser": databaseUserColumns,
		"app":          appColumns,
		"task":         taskColumns,
	} {
		for _, column := range columns {
			if column == "orgId" {
				t.Fatalf("%s columns should not include orgId", name)
			}
		}
	}
}

func TestDefaultTableColumnsUseReadableRelations(t *testing.T) {
	for name, columns := range map[string][]string{
		"app":          appColumns,
		"database":     databaseColumns,
		"databaseDb":   databaseDbColumns,
		"databaseUser": databaseUserColumns,
		"instance":     instanceColumns,
		"route":        routeColumns,
		"port":         appPortColumns,
		"cert":         certColumns,
		"build":        buildColumns,
		"deployment":   deploymentColumns,
		"backup":       backupColumns,
		"import":       importColumns,
		"task":         taskColumns,
		"operation":    operationColumns,
	} {
		for _, column := range columns {
			switch column {
			case "envId", "appId", "stackId", "clusterId", "mainDomain", "appServiceId", "portId", "appRouteId", "routeId", "appCertId", "certId", "certificateId", "appInstanceId", "databaseId", "databaseDbId", "taskId", "authorId":
				t.Fatalf("%s columns should use readable relation names, got %q", name, column)
			}
		}
	}
}

func TestDefaultTaskColumnsUseCompactListShape(t *testing.T) {
	got := strings.Join(taskColumns, ",")
	want := "id,title,status,progress,projects,author,startedAt,duration"
	if got != want {
		t.Fatalf("taskColumns = %q, want %q", got, want)
	}

	getColumns := strings.Join(taskGetColumns, ",")
	for _, expected := range []string{"progress", "projects", "author", "app", "instance", "service", "database", "databaseDb", "originTask", "repeatedTask", "spawnedTasks", "startedAt", "endedAt"} {
		if !strings.Contains(getColumns, expected) {
			t.Fatalf("taskGetColumns should include %q: %s", expected, getColumns)
		}
	}
	if strings.Contains(getColumns, "jobs") {
		t.Fatalf("taskGetColumns should not include jobs: %s", getColumns)
	}
}

func TestStackColumnsShowDatesAndGetShowsServices(t *testing.T) {
	listColumns := strings.Join(stackColumns, ",")
	for _, expected := range []string{"revision", "currentVersion", "outdated", "autoUpdates", "createdAt", "updatedAt"} {
		if !strings.Contains(listColumns, expected) {
			t.Fatalf("stackColumns should include %q: %s", expected, listColumns)
		}
	}
	for _, unwanted := range []string{"public", "revId", "currentRevNumber", "latestRevNumber", "services"} {
		if strings.Contains(listColumns, unwanted) {
			t.Fatalf("stackColumns should not include %q on list: %s", unwanted, listColumns)
		}
	}

	getColumns := strings.Join(stackGetColumns, ",")
	for _, expected := range []string{"public", "revId", "currentRevNumber", "currentVersion", "latestRevNumber", "autoUpdates", "createdAt", "updatedAt", "services"} {
		if !strings.Contains(getColumns, expected) {
			t.Fatalf("stackGetColumns should include %q: %s", expected, getColumns)
		}
	}
}

func TestClusterGetColumnsShowAdditionalDetails(t *testing.T) {
	listColumns := strings.Join(clusterColumns, ",")
	for _, unwanted := range []string{"kubernetesVersion", "infraVersion", "publicIp", "scalable", "serverless"} {
		if strings.Contains(listColumns, unwanted) {
			t.Fatalf("clusterColumns should not include %q on list: %s", unwanted, listColumns)
		}
	}
	for _, expected := range []string{"autoUpdates", "nodes", "singleNode"} {
		if !strings.Contains(listColumns, expected) {
			t.Fatalf("clusterColumns should include %q: %s", expected, listColumns)
		}
	}

	getColumns := strings.Join(clusterGetColumns, ",")
	for _, expected := range []string{"autoUpdates", "kubernetesVersion", "infraVersion", "ips", "nodes", "singleNode"} {
		if !strings.Contains(getColumns, expected) {
			t.Fatalf("clusterGetColumns should include %q: %s", expected, getColumns)
		}
	}
	for _, unwanted := range []string{"instances", "scalable", "serverless"} {
		if strings.Contains(getColumns, unwanted) {
			t.Fatalf("clusterGetColumns should not include %q: %s", unwanted, getColumns)
		}
	}
}

func TestProviderColumnsShowVersionWithoutPublic(t *testing.T) {
	columns := strings.Join(providerColumns, ",")
	for _, expected := range []string{"providerVersion"} {
		if !strings.Contains(columns, expected) {
			t.Fatalf("providerColumns should include %q: %s", expected, columns)
		}
	}
	for _, unwanted := range []string{"public", "revId"} {
		if strings.Contains(columns, unwanted) {
			t.Fatalf("providerColumns should not include %q: %s", unwanted, columns)
		}
	}
}

func TestAppAndInstanceGetColumnsIncludeReadDetails(t *testing.T) {
	appList := strings.Join(appColumns, ",")
	for _, expected := range []string{"instances", "stack"} {
		if !strings.Contains(appList, expected) {
			t.Fatalf("appColumns should include %q: %s", expected, appList)
		}
	}
	if strings.Contains(appList, "clusterApp") {
		t.Fatalf("appColumns should not include clusterApp on list: %s", appList)
	}

	appGet := strings.Join(appGetColumns, ",")
	for _, expected := range []string{"clusterApp", "instances", "createdAt", "updatedAt"} {
		if !strings.Contains(appGet, expected) {
			t.Fatalf("appGetColumns should include %q: %s", expected, appGet)
		}
	}

	instanceGet := strings.Join(instanceGetColumns, ",")
	for _, expected := range []string{"autoUpdates", "serviceStatus", "routeStatus", "portStatus", "createdAt", "updatedAt"} {
		if !strings.Contains(instanceGet, expected) {
			t.Fatalf("instanceGetColumns should include %q: %s", expected, instanceGet)
		}
	}
}

func TestServiceColumnsShowAutoUpdates(t *testing.T) {
	columns := strings.Join(catalogServiceColumns, ",")
	if !strings.Contains(columns, "autoUpdates") {
		t.Fatalf("catalogServiceColumns should include autoUpdates: %s", columns)
	}
}

func TestResponseRowsTreatsEmptyWrapperAsEmpty(t *testing.T) {
	rows := responseRows(map[string]interface{}{
		"items":      []interface{}{},
		"totalCount": 0,
	})
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty", rows)
	}
}

func TestUserCommandUsesCurrentUserEndpoints(t *testing.T) {
	var out bytes.Buffer
	var requested []string
	var updateBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /v1/user":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    1,
				"email": "jane@example.com",
				"name":  "Jane Doe",
				"twofa": true,
				"defaultOrg": map[string]interface{}{
					"id":    10,
					"title": "Acme",
				},
				"defaultProjects": []map[string]interface{}{
					{"id": 20, "title": "Production"},
				},
			})
		case http.MethodPut + " /v1/user":
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    1,
				"email": "jane@example.com",
				"name":  "Jane Smith",
				"twofa": true,
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	getCmd := newUserCommand()
	getCmd.SetOut(&out)
	getCmd.SetArgs([]string{"get"})
	if err := getCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	getOutput := out.String()
	for _, expected := range []string{"email:", "jane@example.com", "default org:", "Acme [10]", "default projects:", "Production [20]"} {
		if !strings.Contains(getOutput, expected) {
			t.Fatalf("user get output should include %q: %s", expected, getOutput)
		}
	}

	out.Reset()
	updateCmd := newUserCommand()
	updateCmd.SetOut(&out)
	updateCmd.SetArgs([]string{"update", "--name", "Jane Smith"})
	if err := updateCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if updateBody["name"] != "Jane Smith" {
		t.Fatalf("update body = %#v, want name", updateBody)
	}
	if strings.Join(requested, "\n") != strings.Join([]string{"GET /v1/user", "PUT /v1/user"}, "\n") {
		t.Fatalf("requests = %#v", requested)
	}
}

func TestMemberListShowsReadableMemberInfo(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org-memberships" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("orgId"); got != "123" {
			t.Fatalf("orgId = %q, want 123", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":       8,
				"role":     "owner",
				"status":   "active",
				"joinedAt": "2026-01-02T03:04:05Z",
				"user": map[string]interface{}{
					"name":  "Jane Doe",
					"email": "jane@example.com",
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newMemberCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"member", "email", "role", "status", "joined at", "Jane Doe <jane@example.com>", "jane@example.com", "owner", "active"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("member list output should include %q: %s", expected, output)
		}
	}
}

func TestTableColumnsShowIntegrationTitleWithProviderTitle(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":   1,
			"name": "main",
			"integration": map[string]interface{}{
				"title": "Production AWS",
				"provider": map[string]interface{}{
					"title": "Amazon Web Services",
				},
			},
		},
	}, databaseColumns)

	output := out.String()
	if strings.Contains(output, "integrationId") {
		t.Fatalf("output should not include integrationId column: %s", output)
	}
	if !strings.Contains(output, "integration") {
		t.Fatalf("output should include integration column: %s", output)
	}
	if !strings.Contains(output, "Production AWS (Amazon Web Services)") {
		t.Fatalf("output should include integration and provider titles: %s", output)
	}
}

func TestTableColumnsShowProviderTitleWithVersionAndRev(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":   1,
			"name": "aws-prod",
			"providerRev": map[string]interface{}{
				"version": "2.1.0",
				"number":  7,
				"provider": map[string]interface{}{
					"title": "Amazon Web Services",
				},
			},
		},
	}, integrationColumns)

	output := out.String()
	if strings.Contains(output, "providerRevId") || strings.Contains(output, "providerId") {
		t.Fatalf("output should not include provider ID columns: %s", output)
	}
	if !strings.Contains(output, "provider") {
		t.Fatalf("output should include provider column: %s", output)
	}
	if !strings.Contains(output, "Amazon Web Services (2.1.0 #7)") {
		t.Fatalf("output should include provider title, version, and revision: %s", output)
	}
}

func TestTableColumnTitlesAreHumanReadable(t *testing.T) {
	for column, expected := range map[string]string{
		"skipRollback":       "skip rollback",
		"createdAt":          "created at",
		"startedAt":          "started at",
		"endedAt":            "ended at",
		"gitRefType":         "git ref type",
		"gitRef":             "git ref",
		"commitHash":         "commit hash",
		"clusterApp":         "cluster app",
		"needsRebuild":       "needs rebuild",
		"needsRedeploy":      "needs redeploy",
		"configurationReady": "configuration ready",
		"latestRevNumber":    "latest rev number",
		"revId":              "rev id",
		"pathType":           "path type",
		"databaseDb":         "db",
		"infraAppInstanceId": "instance id",
		"autoUpdates":        "auto updates",
		"currentVersion":     "version",
	} {
		if got := tableColumnTitle(column); got != expected {
			t.Fatalf("tableColumnTitle(%q) = %q, want %q", column, got, expected)
		}
	}
}

func TestGraphQLBaseURLDerivesRootFromRESTBaseURL(t *testing.T) {
	for endpoint, expected := range map[string]string{
		"https://apiv2.wodby.com/v1":       "https://apiv2.wodby.com",
		"https://apiv2.wodby.com/v1/":      "https://apiv2.wodby.com",
		"http://127.0.0.1:8080/v1":         "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/api/v1?x=1": "http://127.0.0.1:8080/api",
	} {
		got, err := graphQLBaseURL(endpoint)
		if err != nil {
			t.Fatalf("graphQLBaseURL(%q) error = %v", endpoint, err)
		}
		if got != expected {
			t.Fatalf("graphQLBaseURL(%q) = %q, want %q", endpoint, got, expected)
		}
	}
}

func TestOutdatedColumnFormatsFlagsAndRevisionDrift(t *testing.T) {
	for _, test := range []struct {
		name string
		row  map[string]interface{}
		want string
	}{
		{
			name: "explicit false",
			row:  map[string]interface{}{"outdated": false},
			want: "no",
		},
		{
			name: "stack flag",
			row:  map[string]interface{}{"stackOutdated": true},
			want: "yes",
		},
		{
			name: "revision behind",
			row: map[string]interface{}{
				"stackRev": map[string]interface{}{"number": 2},
				"stack":    map[string]interface{}{"latestRevNumber": 3},
			},
			want: "yes",
		},
		{
			name: "revision current",
			row: map[string]interface{}{
				"stackRevision": map[string]interface{}{"revNumber": 3},
				"stack":         map[string]interface{}{"latestRevNumber": 3},
			},
			want: "no",
		},
		{
			name: "missing value",
			row:  map[string]interface{}{},
			want: "no",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatColumnValue(test.row, "outdated"); got != test.want {
				t.Fatalf("formatColumnValue(outdated) = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAutoUpdatesColumnFormatsResourceSettings(t *testing.T) {
	for _, test := range []struct {
		name string
		row  map[string]interface{}
		want string
	}{
		{
			name: "service git update",
			row: map[string]interface{}{
				"settings": map[string]interface{}{
					"gitAutoUpdate": map[string]interface{}{"enabled": true},
				},
			},
			want: "yes",
		},
		{
			name: "stack update families",
			row: map[string]interface{}{
				"gitRepoId": "repo-1",
				"settings": map[string]interface{}{
					"gitAutoUpdate":             map[string]interface{}{"enabled": true},
					"autoServiceRevisionUpdate": map[string]interface{}{"enabled": false},
					"autoOriginStackUpdate":     map[string]interface{}{"enabled": true},
				},
			},
			want: "git=yes, services=no, origin=yes",
		},
		{
			name: "non git sourced stack update families",
			row: map[string]interface{}{
				"settings": map[string]interface{}{
					"gitAutoUpdate":             map[string]interface{}{"enabled": true},
					"autoServiceRevisionUpdate": map[string]interface{}{"enabled": false},
					"autoOriginStackUpdate":     map[string]interface{}{"enabled": true},
				},
			},
			want: "services=no, origin=yes",
		},
		{
			name: "cluster component updates",
			row: map[string]interface{}{
				"settings": map[string]interface{}{
					"autoInfrastructureUpgrade": map[string]interface{}{
						"enabled": true,
						"infra":   map[string]interface{}{"enabled": false},
						"apps":    map[string]interface{}{"enabled": true},
					},
				},
			},
			want: "infra=no, apps=yes",
		},
		{
			name: "cluster master disabled",
			row: map[string]interface{}{
				"settings": map[string]interface{}{
					"autoInfrastructureUpgrade": map[string]interface{}{"enabled": false},
				},
			},
			want: "no",
		},
		{
			name: "app instance stack upgrade",
			row: map[string]interface{}{
				"settings": map[string]interface{}{
					"autoStackUpgrade": map[string]interface{}{"enabled": true},
				},
			},
			want: "yes",
		},
		{
			name: "missing settings",
			row:  map[string]interface{}{},
			want: "no",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatColumnValue(test.row, "autoUpdates"); got != test.want {
				t.Fatalf("formatColumnValue(autoUpdates) = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClusterNodesColumnShowsReadyOverTotal(t *testing.T) {
	row := map[string]interface{}{
		"lastNodesReady":   4,
		"lastNodesTotal":   6,
		"readyNodeCount":   3,
		"totalNodeCount":   4,
		"currentNodeCount": 2,
		"maxNodeCount":     5,
	}
	if got := formatColumnValue(row, "nodes"); got != "4 / 6" {
		t.Fatalf("formatColumnValue(nodes) = %q, want 4 / 6", got)
	}
}

func TestClusterNodesColumnKeepsZeroReadyCount(t *testing.T) {
	row := map[string]interface{}{
		"lastNodesReady": 0,
		"lastNodesTotal": 2,
	}
	if got := formatColumnValue(row, "nodes"); got != "0 / 2" {
		t.Fatalf("formatColumnValue(nodes) = %q, want 0 / 2", got)
	}
}

func TestClusterNodesColumnShowsSingleNodeAsReadyAndTotal(t *testing.T) {
	row := map[string]interface{}{
		"singleNode":       true,
		"currentNodeCount": 2,
		"maxNodeCount":     5,
	}
	if got := formatColumnValue(row, "nodes"); got != "1 / 1" {
		t.Fatalf("formatColumnValue(nodes) = %q, want 1 / 1", got)
	}
}

func TestTableColumnsFormatTimestampsAsRelativeAge(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	createdAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":        1,
			"createdAt": createdAt,
		},
	}, []string{"id", "createdAt"})

	output := out.String()
	if !strings.Contains(output, "2h ago") {
		t.Fatalf("output should include relative timestamp: %s", output)
	}
	if strings.Contains(output, createdAt) {
		t.Fatalf("output should not include raw timestamp: %s", output)
	}
}

func TestTaskColumnsShowProgressProjectsStartedAndDuration(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	startedAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":          1,
			"name":        "worker-task",
			"title":       "Deploy",
			"status":      "done",
			"progress":    87,
			"projectIds":  []interface{}{12, 14},
			"createdAt":   "2026-01-02T03:00:00Z",
			"startedAt":   startedAt,
			"endedAt":     time.Now().Add(-58 * time.Minute).UTC().Format(time.RFC3339),
			"appInstance": map[string]interface{}{"title": "Prod"},
			"app":         map[string]interface{}{"title": "Drupal"},
		},
	}, taskColumns)

	output := out.String()
	for _, expected := range []string{"id", "title", "status", "progress", "projects", "author", "started at", "duration", "87%", "12, 14", "2h ago", "1h 2m"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"name", "worker-task", "created at", "app", "Drupal", "instance", "Prod", "ended at", startedAt, "2026-01-02T03:00:00Z"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTaskListEnrichesAuthor(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":        42,
					"title":     "Deploy",
					"status":    "done",
					"authorId":  8,
					"createdAt": "2026-01-02T03:00:00Z",
					"startedAt": "2026-01-02T03:04:00Z",
					"endedAt":   "2026-01-02T03:06:00Z",
				},
			})
		case "/v1/org-memberships/8":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 8,
				"user": map[string]interface{}{
					"name":  "Jane Doe",
					"email": "jane@example.com",
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"author", "Jane Doe <jane@example.com>"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"authorId", "jane@example.com\t8"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("task list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTaskGetEnrichesAuthor(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             42,
				"title":          "Deploy",
				"status":         "done",
				"progress":       100,
				"projectIds":     []int{12, 14},
				"authorId":       8,
				"originTaskId":   41,
				"repeatedTaskId": 39,
				"spawnedTaskIds": []int{43, 44},
				"createdAt":      "2026-01-02T03:00:00Z",
				"startedAt":      "2026-01-02T03:04:00Z",
				"endedAt":        "2026-01-02T03:06:00Z",
			})
		case "/v1/org-memberships/8":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 8,
				"user": map[string]interface{}{
					"name":  "Jane Doe",
					"email": "jane@example.com",
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"progress:", "100%", "projects:", "12, 14", "author:", "Jane Doe <jane@example.com>", "author id:", "8", "origin task:", "41", "repeated task:", "39", "spawned tasks:", "43, 44", "started at:", "2026-01-02 03:04", "ended at:", "2026-01-02 03:06"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "authorId:") {
		t.Fatalf("task get output should not include raw authorId: %s", output)
	}
}

func TestTaskGetDerivesAppFromAppInstance(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        42,
				"title":     "Deploy",
				"status":    "done",
				"createdAt": "2026-01-02T03:00:00Z",
				"startedAt": "2026-01-02T03:04:00Z",
				"endedAt":   "2026-01-02T03:06:00Z",
				"appInstance": map[string]interface{}{
					"title": "Prod",
					"id":    7,
					"appId": 11,
				},
				"appServiceId": 22,
				"databaseId":   33,
				"databaseDb": map[string]interface{}{
					"id":    44,
					"title": "Main DB",
				},
				"jobs": []map[string]interface{}{
					{
						"id":        "job-1",
						"title":     "Build",
						"status":    "done",
						"startedAt": "2026-01-02T03:04:00Z",
						"endedAt":   "2026-01-02T03:06:00Z",
						"steps": []map[string]interface{}{
							{
								"id":        "step-1",
								"name":      "Prepare",
								"status":    "done",
								"startedAt": "2026-01-02T03:04:00Z",
								"endedAt":   "2026-01-02T03:04:30Z",
							},
							{
								"id":        "step-2",
								"title":     "Deploy",
								"status":    "done",
								"startedAt": "2026-01-02T03:04:30Z",
								"endedAt":   "2026-01-02T03:06:00Z",
							},
						},
					},
				},
			})
		case "/v1/apps/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 11, "title": "Drupal"})
		case "/v1/app-services/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "PHP"})
		case "/v1/databases/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Postgres"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"app:", "Drupal", "app id:", "11", "instance:", "Prod", "instance id:", "7", "service:", "PHP", "service id:", "22", "database:", "Postgres", "database id:", "33", "db:", "Main DB", "db id:", "44", "duration:", "2m"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appServiceId:", "databaseId:", "databaseDbId:", "jobs:", "Build [job-1]", "Prepare [step-1]", "Deploy [step-2]"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("task get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTaskCommandExposesJobAndStepSubcommands(t *testing.T) {
	task := newTaskCommand()
	names := make(map[string]bool)
	for _, cmd := range task.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "wait", "logs", "job", "step", "cancel", "repeat"} {
		if !names[name] {
			t.Fatalf("missing task subcommand %q", name)
		}
	}
}

func TestTaskJobAndStepCommandsUseTaskPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/42" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 42,
			"jobs": []map[string]interface{}{
				{
					"id":        "job-1",
					"title":     "Build",
					"status":    "done",
					"logStatus": "uploaded",
					"system":    true,
					"startedAt": "2026-01-02T03:00:00Z",
					"endedAt":   "2026-01-02T03:02:00Z",
					"steps": []map[string]interface{}{
						{"id": "step-1", "name": "Prepare", "status": "done", "logStatus": "uploaded", "system": true, "startedAt": "2026-01-02T03:00:00Z", "endedAt": "2026-01-02T03:01:00Z"},
					},
				},
				{
					"id":        "job-2",
					"title":     "Deploy",
					"status":    "running",
					"logStatus": "pending",
					"system":    false,
					"startedAt": "2026-01-02T03:03:00Z",
					"endedAt":   "2026-01-02T03:05:00Z",
					"steps": []map[string]interface{}{
						{
							"id":        "step-2",
							"name":      "Apply",
							"status":    "running",
							"logStatus": "pending",
							"system":    false,
							"startedAt": "2026-01-02T03:03:00Z",
							"endedAt":   "2026-01-02T03:03:30Z",
						},
					},
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	for _, test := range []struct {
		name     string
		args     []string
		expected []string
	}{
		{name: "job list", args: []string{"job", "list", "42"}, expected: []string{"id", "name", "status", "log status", "system", "started at", "duration", "steps", "job-1", "Build", "done", "uploaded", "true", "2m", "job-2", "Deploy", "running", "pending"}},
		{name: "job get", args: []string{"job", "get", "42", "Deploy"}, expected: []string{"id:", "job-2", "name:", "Deploy", "status:", "running", "log status:", "pending", "system:", "false", "started at:", "2026-01-02 03:03", "duration:", "2m", "steps:", "1"}},
		{name: "step list", args: []string{"step", "list", "42"}, expected: []string{"id", "name", "status", "log status", "system", "started at", "duration", "job", "step-1", "Prepare", "uploaded", "true", "Build [job-1]", "step-2", "Apply", "30s", "Deploy [job-2]"}},
		{name: "step get", args: []string{"step", "get", "42", "step-2"}, expected: []string{"id:", "step-2", "name:", "Apply", "status:", "running", "log status:", "pending", "system:", "false", "started at:", "2026-01-02 03:03", "duration:", "30s", "job:", "Deploy [job-2]"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := newTaskCommand()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			output := out.String()
			for _, expected := range test.expected {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
		})
	}
}

func TestTaskStepLogsFetchesStepLogs(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/task-steps/step-2/logs" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("delivery"); got != "auto" {
			t.Fatalf("delivery = %q, want auto", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "pending",
			"lines": []map[string]interface{}{
				{"level": "info", "message": "applied"},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"step", "logs", "step-2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if output := out.String(); !strings.Contains(output, "[info] applied") {
		t.Fatalf("step logs output should include log line: %s", output)
	}
}

func TestTaskStepLogsDownloadsSignedURLWithoutAPIKey(t *testing.T) {
	var out bytes.Buffer

	logServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-KEY"); got != "" {
			t.Fatalf("signed URL request should not include X-API-KEY, got %q", got)
		}
		if got := r.Header.Get("X-ACCESS-TOKEN"); got != "" {
			t.Fatalf("signed URL request should not include X-ACCESS-TOKEN, got %q", got)
		}
		_, _ = w.Write([]byte(`[
			{"time":"2026-06-28T00:00:01Z","stream":"stdout","log":"persisted line 1\n"},
			{"level":"error","message":"persisted line 2"}
		]`))
	}))
	defer logServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/task-steps/step-2/logs" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("delivery"); got != "auto" {
			t.Fatalf("delivery = %q, want auto", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "persisted",
			"url":    logServer.URL + "/logs/object",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"step", "logs", "step-2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"[2026-06-28T00:00:01Z] [stdout] persisted line 1", "[error] persisted line 2"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("step logs output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, `"message"`) || strings.Contains(output, `"log"`) {
		t.Fatalf("step logs output should not include raw JSON records: %s", output)
	}
}

func TestOperationTaskResultStreamsTaskLogs(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/backups":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": 42})
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create backup",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-1",
						"title":  "Backup",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-1", "name": "Dump database", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-1/logs":
			if got := r.URL.Query().Get("delivery"); got != "auto" {
				t.Fatalf("delivery = %q, want auto", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "backup created"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBackupCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"integrationId":1,"bucket":"main"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"Operation started. Streaming task logs for task 42.", "== Dump database (completed) ==", "[info] backup created"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	wantRequests := []string{"POST /v1/backups", "GET /v1/tasks/42", "GET /v1/task-steps/step-1/logs"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestOperationTaskResultIgnoresEndedLogStream(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/backups":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": 42})
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create backup",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-1",
						"title":  "Backup",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-1", "name": "Dump database", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-1/logs":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusGone)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"title":   "Log stream ended",
				"detail":  "Log stream expired",
				"message": "Log stream expired",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBackupCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"integrationId":1,"bucket":"main"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"Operation started. Streaming task logs for task 42.", "no logs"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "Log stream expired") {
		t.Fatalf("output should not include stream-ended API error: %s", output)
	}
	wantRequests := []string{"POST /v1/backups", "GET /v1/tasks/42", "GET /v1/task-steps/step-1/logs"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestStreamTaskLogsStopsOnBackendDoneStatus(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create backup",
				"status": "done",
				"jobs":   []map[string]interface{}{},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	client, err := newRESTClient()
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := streamTaskLogs(context.Background(), cmd, client, "42", 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	if output := strings.TrimSpace(out.String()); output != "no logs\nTask completed." {
		t.Fatalf("output = %q, want no logs and completion", output)
	}
	wantRequests := []string{"GET /v1/tasks/42"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestCreateCommandsStreamReferencedTaskLogs(t *testing.T) {
	tests := []struct {
		name      string
		command   func() *cobra.Command
		args      []string
		createURL string
		queryName string
		message   string
	}{
		{
			name:      "app",
			command:   newAppCommand,
			args:      []string{"create", "--data", `{"orgId":10,"projectId":12,"envId":22,"clusterId":33,"stackRevId":70,"name":"site","instanceName":"prod"}`},
			createURL: "/v1/apps",
			queryName: "appId",
			message:   "app creation started. Streaming task logs for app 101 (task 42).",
		},
		{
			name:      "app instance",
			command:   func() *cobra.Command { return newAppInstanceCommand("instance", "Manage app instances") },
			args:      []string{"create", "--data", `{"appId":11,"envId":22,"clusterId":33,"stackRevId":70,"instanceName":"prod"}`},
			createURL: "/v1/app-instances",
			queryName: "appInstanceId",
			message:   "app instance creation started. Streaming task logs for app instance 101 (task 42).",
		},
		{
			name:      "cluster",
			command:   newClusterCommand,
			args:      []string{"create", "--data", `{"orgId":10,"integrationId":7,"name":"prod","title":"Production"}`},
			createURL: "/v1/clusters",
			queryName: "clusterId",
			message:   "cluster creation started. Streaming task logs for cluster 101 (task 42).",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var requests []string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.Method + " " + r.URL.Path {
				case http.MethodPost + " " + test.createURL:
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":    101,
						"name":  "site",
						"title": "Site",
					})
				case http.MethodGet + " /v1/tasks":
					if got := r.URL.Query().Get(test.queryName); got != "101" {
						t.Fatalf("%s = %q, want 101", test.queryName, got)
					}
					if got := r.URL.Query().Get("pageSize"); got != "10" {
						t.Fatalf("pageSize = %q, want 10", got)
					}
					_ = json.NewEncoder(w).Encode([]map[string]interface{}{
						{"id": 42, "title": "Create resource", "status": "running"},
					})
				case http.MethodGet + " /v1/tasks/42":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":     42,
						"title":  "Create resource",
						"status": "completed",
						"jobs": []map[string]interface{}{
							{
								"id":     "job-1",
								"title":  "Create",
								"status": "completed",
								"steps": []map[string]interface{}{
									{"id": "step-1", "name": "Provision resource", "status": "completed"},
								},
							},
						},
					})
				case http.MethodGet + " /v1/task-steps/step-1/logs":
					if got := r.URL.Query().Get("delivery"); got != "auto" {
						t.Fatalf("delivery = %q, want auto", got)
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"lines": []map[string]interface{}{
							{"level": "info", "message": "resource is ready"},
						},
					})
				case http.MethodGet + " /v1/app-builds", http.MethodGet + " /v1/app-deployments":
					if test.name != "app instance" {
						t.Fatalf("unexpected follow-up request for %s: %s", test.name, r.URL.Path)
					}
					if got := r.URL.Query().Get("appInstanceId"); got != "101" {
						t.Fatalf("follow-up appInstanceId = %q, want 101", got)
					}
					if got := r.URL.Query().Get("pageSize"); got != "1" {
						t.Fatalf("follow-up pageSize = %q, want 1", got)
					}
					encodeEmptyItems(w)
				default:
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := test.command()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			output := out.String()
			for _, expected := range []string{test.message, "== Provision resource (completed) ==", "[info] resource is ready", "Task completed."} {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
			if strings.Contains(output, "\nid\t") {
				t.Fatalf("output should not include the raw create result table: %s", output)
			}
			wantRequests := []string{"POST " + test.createURL, "GET /v1/tasks", "GET /v1/tasks/42", "GET /v1/task-steps/step-1/logs"}
			if test.name == "app instance" {
				wantRequests = append(wantRequests, "GET /v1/app-builds", "GET /v1/app-deployments")
			}
			if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
				t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
			}
		})
	}
}

func TestAppCreateStreamsInitialInstanceTaskWhenAppTaskMissing(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/apps":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    101,
				"orgId": 10,
				"name":  "site",
				"title": "Site",
			})
		case http.MethodGet + " /v1/tasks":
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			switch {
			case r.URL.Query().Get("appId") != "":
				if got := r.URL.Query().Get("appId"); got != "101" {
					t.Fatalf("task appId = %q, want 101", got)
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"items":      []map[string]interface{}{},
					"totalCount": 0,
				})
			case r.URL.Query().Get("appInstanceId") != "":
				if got := r.URL.Query().Get("appInstanceId"); got != "202" {
					t.Fatalf("task appInstanceId = %q, want 202", got)
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"items": []map[string]interface{}{
						{"id": 42, "title": "Create app instance", "status": "running"},
					},
					"totalCount": 1,
				})
			default:
				t.Fatalf("task query should include appId or appInstanceId: %s", r.URL.RawQuery)
			}
		case http.MethodGet + " /v1/app-instances":
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("instance appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("instance orgId = %q, want 10", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("instance pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 202, "appId": 101},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create app instance",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-1",
						"title":  "Create",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-1", "name": "Provision app", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-1/logs":
			if got := r.URL.Query().Get("delivery"); got != "auto" {
				t.Fatalf("delivery = %q, want auto", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "app is ready"},
				},
			})
		case http.MethodGet + " /v1/app-builds", http.MethodGet + " /v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("follow-up appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("follow-up pageSize = %q, want 1", got)
			}
			encodeEmptyItems(w)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"orgId":10,"projectId":12,"envId":22,"clusterId":33,"stackRevId":70,"name":"site","instanceName":"prod"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{
		"app creation started. Streaming task logs for app 101 (task 42).",
		"== Provision app (completed) ==",
		"[info] app is ready",
		"Task completed.",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "\nid\t") {
		t.Fatalf("output should not include the raw create result table: %s", output)
	}
	wantRequests := []string{
		"POST /v1/apps",
		"GET /v1/tasks",
		"GET /v1/app-instances",
		"GET /v1/tasks",
		"GET /v1/tasks/42",
		"GET /v1/task-steps/step-1/logs",
		"GET /v1/app-builds",
		"GET /v1/app-deployments",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestAppCreateStreamsBuildTaskAfterCreationTask(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/apps":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    101,
				"orgId": 10,
				"name":  "site",
				"title": "Site",
			})
		case http.MethodGet + " /v1/tasks":
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("task appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 42, "title": "Create app", "status": "running"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-create",
						"title":  "Create",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-create", "name": "Provision app", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-create/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "app is ready"},
				},
			})
		case http.MethodGet + " /v1/app-instances":
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("instance appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("instance orgId = %q, want 10", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("instance pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 202, "appId": 101},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/app-builds":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("build appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("build pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 501, "number": 1, "status": "running", "taskId": 84, "createdAt": "2026-01-02T03:04:00Z"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("deployment appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 601, "number": 1, "status": "running", "taskId": 85, "createdAt": "2026-01-02T03:05:00Z"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/84":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     84,
				"title":  "Build app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-build",
						"title":  "Build",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-build", "name": "Build image", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-build/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "image built"},
				},
			})
		case http.MethodGet + " /v1/tasks/85":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     85,
				"title":  "Deploy app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-deploy",
						"title":  "Deploy",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-deploy", "name": "Deploy services", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-deploy/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "services deployed"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"orgId":10,"projectId":12,"envId":22,"clusterId":33,"stackRevId":70,"name":"site","instanceName":"prod"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{
		"app creation started. Streaming task logs for app 101 (task 42).",
		"[info] app is ready",
		"Task completed.",
		"Build started. Streaming task logs for build 501 (task 84).",
		"== Build image (completed) ==",
		"[info] image built",
		"Deployment started. Streaming task logs for deployment 601 (task 85).",
		"== Deploy services (completed) ==",
		"[info] services deployed",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	wantRequests := []string{
		"POST /v1/apps",
		"GET /v1/tasks",
		"GET /v1/tasks/42",
		"GET /v1/task-steps/step-create/logs",
		"GET /v1/app-instances",
		"GET /v1/app-builds",
		"GET /v1/tasks/84",
		"GET /v1/task-steps/step-build/logs",
		"GET /v1/app-deployments",
		"GET /v1/tasks/85",
		"GET /v1/task-steps/step-deploy/logs",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestAppInstanceCreateStreamsDeploymentTaskWhenNoBuildTask(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/app-instances":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    202,
				"name":  "prod",
				"title": "Production",
			})
		case http.MethodGet + " /v1/tasks":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("task appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 42, "title": "Create app instance", "status": "running"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     42,
				"title":  "Create app instance",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-create",
						"title":  "Create",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-create", "name": "Provision instance", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-create/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "instance is ready"},
				},
			})
		case http.MethodGet + " /v1/app-builds":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("build appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("build pageSize = %q, want 1", got)
			}
			encodeEmptyItems(w)
		case http.MethodGet + " /v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("deployment appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 601, "number": 1, "status": "running", "taskId": 85, "createdAt": "2026-01-02T03:05:00Z"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/85":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     85,
				"title":  "Deploy app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-deploy",
						"title":  "Deploy",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-deploy", "name": "Deploy services", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-deploy/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "services deployed"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"appId":11,"envId":22,"clusterId":33,"stackRevId":70,"instanceName":"prod"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{
		"app instance creation started. Streaming task logs for app instance 202 (task 42).",
		"[info] instance is ready",
		"Task completed.",
		"Deployment started. Streaming task logs for deployment 601 (task 85).",
		"== Deploy services (completed) ==",
		"[info] services deployed",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	wantRequests := []string{
		"POST /v1/app-instances",
		"GET /v1/tasks",
		"GET /v1/tasks/42",
		"GET /v1/task-steps/step-create/logs",
		"GET /v1/app-builds",
		"GET /v1/app-deployments",
		"GET /v1/tasks/85",
		"GET /v1/task-steps/step-deploy/logs",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestBuildCreateStreamsBuildAndTriggeredDeploymentLogs(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /v1/app-builds":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 501, "appInstanceId": 202, "status": "running"},
				},
				"taskId": 84,
			})
		case http.MethodGet + " /v1/tasks/84":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     84,
				"title":  "Build app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-build",
						"title":  "Build",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-build", "name": "Build image", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-build/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "image built"},
				},
			})
		case http.MethodGet + " /v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "202" {
				t.Fatalf("deployment appInstanceId = %q, want 202", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 601, "number": 1, "status": "running", "taskId": 85, "createdAt": "2026-01-02T03:05:00Z"},
				},
				"totalCount": 1,
			})
		case http.MethodGet + " /v1/tasks/85":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     85,
				"title":  "Deploy app",
				"status": "completed",
				"jobs": []map[string]interface{}{
					{
						"id":     "job-deploy",
						"title":  "Deploy",
						"status": "completed",
						"steps": []map[string]interface{}{
							{"id": "step-deploy", "name": "Deploy services", "status": "completed"},
						},
					},
				},
			})
		case http.MethodGet + " /v1/task-steps/step-deploy/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"level": "info", "message": "services deployed"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBuildCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"appServiceIds":[22]}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{
		"Build started. Streaming task logs for build 501 (task 84).",
		"== Build image (completed) ==",
		"[info] image built",
		"Task completed.",
		"Deployment started. Streaming task logs for deployment 601 (task 85).",
		"== Deploy services (completed) ==",
		"[info] services deployed",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "\nid\t") {
		t.Fatalf("output should not include raw build result table: %s", output)
	}
	wantRequests := []string{
		"POST /v1/app-builds",
		"GET /v1/tasks/84",
		"GET /v1/task-steps/step-build/logs",
		"GET /v1/app-deployments",
		"GET /v1/tasks/85",
		"GET /v1/task-steps/step-deploy/logs",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestDeploymentRunCommandsStreamTaskLogs(t *testing.T) {
	tests := []struct {
		name     string
		command  func() *cobra.Command
		args     []string
		postPath string
	}{
		{
			name:     "build deploy",
			command:  newBuildCommand,
			args:     []string{"deploy", "501"},
			postPath: "/v1/app-builds/501/deploy",
		},
		{
			name:     "deployment create",
			command:  newDeploymentCommand,
			args:     []string{"create", "--data", `{"services":[{"appServiceId":22}]}`},
			postPath: "/v1/app-deployments",
		},
		{
			name:     "deployment redeploy",
			command:  newDeploymentCommand,
			args:     []string{"redeploy", "601"},
			postPath: "/v1/app-deployments/601/redeploy",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var requests []string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.Method + " " + r.URL.Path {
				case http.MethodPost + " " + test.postPath:
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":            601,
						"number":        1,
						"status":        "running",
						"appInstanceId": 202,
						"taskId":        85,
					})
				case http.MethodGet + " /v1/tasks/85":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":     85,
						"title":  "Deploy app",
						"status": "completed",
						"jobs": []map[string]interface{}{
							{
								"id":     "job-deploy",
								"title":  "Deploy",
								"status": "completed",
								"steps": []map[string]interface{}{
									{"id": "step-deploy", "name": "Deploy services", "status": "completed"},
								},
							},
						},
					})
				case http.MethodGet + " /v1/task-steps/step-deploy/logs":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"lines": []map[string]interface{}{
							{"level": "info", "message": "services deployed"},
						},
					})
				default:
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := test.command()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			output := out.String()
			for _, expected := range []string{
				"Deployment started. Streaming task logs for deployment 601 (task 85).",
				"== Deploy services (completed) ==",
				"[info] services deployed",
				"Task completed.",
			} {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
			if strings.Contains(output, "\nid\t") {
				t.Fatalf("output should not include raw deployment result table: %s", output)
			}
			wantRequests := []string{
				"POST " + test.postPath,
				"GET /v1/tasks/85",
				"GET /v1/task-steps/step-deploy/logs",
			}
			if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
				t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
			}
		})
	}
}

func TestOperationTaskResultKeepsJSONOutputRaw(t *testing.T) {
	var out bytes.Buffer
	var taskRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/42" {
			taskRequests++
		}
		if r.URL.Path != "/v1/backups" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "taskId": 42})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBackupCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "--data", `{"integrationId":1,"bucket":"main"}`, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, `"taskId": 42`) {
		t.Fatalf("json output should include raw task id: %s", output)
	}
	if strings.Contains(output, "Streaming task logs") {
		t.Fatalf("json output should not stream logs: %s", output)
	}
	if taskRequests != 0 {
		t.Fatalf("task requests = %d, want 0", taskRequests)
	}
}

func TestLogLinesFormatsNewlineDelimitedJSON(t *testing.T) {
	lines := logLines(`{"level":"info","message":"first"}
{"stream":"stderr","log":"second\n"}`)

	want := []string{"[info] first", "[stderr] second"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("logLines() = %#v, want %#v", lines, want)
	}
}

func TestJSONOutputKeepsRawTaskData(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := printResult(cmd, outputOptions{output: outputJSON}, map[string]interface{}{
		"id":        1,
		"name":      "worker-task",
		"title":     "Deploy",
		"status":    "done",
		"progress":  87,
		"createdAt": "2026-01-02T03:00:00Z",
		"startedAt": "2026-01-02T03:04:05Z",
		"endedAt":   "2026-01-02T04:06:07Z",
	}, taskColumns)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{`"name": "worker-task"`, `"progress": 87`, `"startedAt": "2026-01-02T03:04:05Z"`, `"endedAt": "2026-01-02T04:06:07Z"`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("json output should include raw field %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{`"duration"`, "2026-01-02 03:04", "1h 2m"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("json output should not include display value %q: %s", unwanted, output)
		}
	}
}

func TestJSONOutputUsesStdoutByDefault(t *testing.T) {
	cmd := &cobra.Command{}
	var printErr error

	stdout, stderr := captureProcessOutput(t, func() {
		printErr = printJSON(cmd, map[string]interface{}{"id": 1})
	})
	if printErr != nil {
		t.Fatal(printErr)
	}
	if !strings.Contains(stdout, `"id": 1`) {
		t.Fatalf("stdout should include JSON output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestInheritedOutputFlagControlsCopiedOutputOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/stacks":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 1, "name": "drupal", "title": "Drupal"},
				},
			})
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       42,
				"title":    "Deploy",
				"progress": 87,
			})
		case "/v1/app-builds":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 101, "number": 1, "commitMessage": "Build PHP"},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	for _, test := range []struct {
		name       string
		cmd        *cobra.Command
		args       []string
		want       string
		unexpected string
	}{
		{
			name:       "catalog list",
			cmd:        newStackCommand(),
			args:       []string{"list", "-o", "json"},
			want:       `"items"`,
			unexpected: "id  name",
		},
		{
			name:       "generic get",
			cmd:        newTaskCommand(),
			args:       []string{"get", "42", "-o", "json"},
			want:       `"progress": 87`,
			unexpected: "id:",
		},
		{
			name:       "instance nested list",
			cmd:        newAppInstanceCommand("instance", "Manage app instances"),
			args:       []string{"build", "list", "21", "-o", "json"},
			want:       `"commitMessage": "Build PHP"`,
			unexpected: "commit message",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := test.cmd
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			output := out.String()
			if !strings.Contains(output, test.want) {
				t.Fatalf("json output should include %q: %s", test.want, output)
			}
			if strings.Contains(output, test.unexpected) {
				t.Fatalf("json output should not include table output %q: %s", test.unexpected, output)
			}
		})
	}
}

func TestTableColumnsShowAppInstanceRelations(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":         1,
			"name":       "prod",
			"title":      "Production",
			"status":     "running",
			"mainDomain": "example.com",
			"app": map[string]interface{}{
				"id":    11,
				"title": "Drupal",
			},
			"stack": map[string]interface{}{
				"id":    7,
				"title": "Drupal Stack",
			},
			"env": map[string]interface{}{
				"id":    22,
				"title": "Prod",
			},
			"cluster": map[string]interface{}{
				"id":    33,
				"title": "Primary",
			},
		},
	}, instanceColumns)

	output := out.String()
	for _, rawColumn := range []string{"appId", "stackId", "envId", "clusterId", "mainDomain"} {
		if strings.Contains(output, rawColumn) {
			t.Fatalf("output should not include %s column: %s", rawColumn, output)
		}
	}
	for _, expected := range []string{"app", "stack", "env", "cluster", "domain", "Drupal", "Drupal Stack", "Prod", "Primary", "example.com"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
}

func TestGetResultUsesVerticalTableWithRelationIDRows(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := printGetResult(cmd, outputOptions{output: outputTable}, map[string]interface{}{
		"id":         1,
		"name":       "prod",
		"title":      "Production",
		"status":     "running",
		"mainDomain": "example.com",
		"app": map[string]interface{}{
			"id":    11,
			"title": "Drupal",
		},
		"stack": map[string]interface{}{
			"id":    7,
			"title": "Drupal Stack",
		},
		"env": map[string]interface{}{
			"id":    22,
			"title": "Prod",
		},
	}, instanceColumns)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"id:", "name:", "app:", "Drupal", "app id:", "11", "stack:", "Drupal Stack", "stack id:", "7", "env:", "Prod", "env id:", "22", "domain:", "example.com"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("vertical output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"Drupal (id: 11)", "Drupal Stack (id: 7)", "Prod (id: 22)", "appId:", "stackId:", "envId:", "mainDomain:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("vertical output should not include %q: %s", unwanted, output)
		}
	}
}

func TestGetResultShowsTaskWithTaskIDRow(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := printGetResult(cmd, outputOptions{output: outputTable}, map[string]interface{}{
		"id":     1,
		"number": 12,
		"status": "done",
		"task": map[string]interface{}{
			"id":    99,
			"title": "Deploy app",
		},
	}, deploymentColumns)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"task:", "Deploy app", "task id:", "99"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("vertical output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "taskId:") {
		t.Fatalf("vertical output should use readable task id key: %s", output)
	}
}

func TestRelationColumnsDoNotFallbackToIDs(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":        1,
			"name":      "prod",
			"appId":     11,
			"envId":     22,
			"clusterId": 33,
			"taskId":    44,
		},
	}, append(instanceColumns, "task"))

	output := out.String()
	for _, unwanted := range []string{"11", "22", "33", "44"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("relation columns should not fallback to IDs: %s", output)
		}
	}
}

func TestListVerticalOutputDoesNotShowRelationIDRows(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := printResult(cmd, outputOptions{output: outputVertical}, []interface{}{
		map[string]interface{}{
			"id":     1,
			"number": 12,
			"task": map[string]interface{}{
				"id":    44,
				"title": "Deploy app",
			},
		},
	}, deploymentColumns)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "task:") || !strings.Contains(output, "Deploy app") {
		t.Fatalf("vertical list output should include readable task details: %s", output)
	}
	if strings.Contains(output, "task id:") || strings.Contains(output, "44") {
		t.Fatalf("vertical list output should not include task id rows: %s", output)
	}
}

func TestAppCommandExposesCanonicalNestedResources(t *testing.T) {
	app := newAppCommand()
	names := make(map[string]bool)
	for _, cmd := range app.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "status", "create", "instance"} {
		if !names[name] {
			t.Fatalf("missing app subcommand %q", name)
		}
	}
	for _, name := range []string{"service", "route", "port"} {
		if names[name] {
			t.Fatalf("app should not expose instance subcommand %q", name)
		}
	}
}

func TestInstanceCommandExposesCanonicalNestedResources(t *testing.T) {
	instance := newAppInstanceCommand("instance", "Manage app instances")
	names := make(map[string]bool)
	for _, cmd := range instance.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "get-by-name", "status", "create", "update", "delete", "settings", "upgrade-stack", "service", "route", "port", "cert", "build", "deployment", "backup", "import"} {
		if !names[name] {
			t.Fatalf("missing instance subcommand %q", name)
		}
	}
}

func TestSchemaAddedCommandSurfaceIsExposed(t *testing.T) {
	assertChildren := func(t *testing.T, cmd *cobra.Command, expected ...string) {
		t.Helper()
		names := make(map[string]bool)
		for _, child := range cmd.Commands() {
			names[child.Name()] = true
		}
		for _, name := range expected {
			if !names[name] {
				t.Fatalf("%s missing subcommand %q", cmd.Name(), name)
			}
		}
	}

	topLevel := make(map[string]bool)
	for _, cmd := range Commands() {
		topLevel[cmd.Name()] = true
	}
	if !topLevel["integration-kind"] {
		t.Fatal("missing top-level integration-kind command")
	}
	if !topLevel["helm"] {
		t.Fatal("missing top-level helm command")
	}

	assertChildren(t, newUserCommand(), "get", "update")
	assertChildren(t, newOrgCommand(), "get", "update")
	assertChildren(t, newProjectCommand(), "get-by-name", "create", "update", "delete")
	assertChildren(t, newDatabaseCommand(), "get-by-name", "options")
	assertChildren(t, newClusterCommand(), "get-by-name", "settings", "upgrade-infra", "upgrade-infra-apps", "delete")
	assertChildren(t, newIntegrationCommand(), "get-by-name", "options")
	assertChildren(t, newProviderCommand(), "get-by-name", "revision")
	assertChildren(t, newHelmCommand(), "inspect", "scaffold-service", "scaffold-stack")
	assertChildren(t, newStackCommand(), "get-by-name", "import", "validate-manifest", "create-from-manifest", "settings", "revision", "publish-draft", "update-from-git", "duplicate", "sync-origin")
	assertChildren(t, newServiceCommand(), "get-by-name", "import", "validate-manifest", "create-from-manifest", "settings", "revision", "options")
}

func TestStackActionCommandsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantPath   string
		assertBody func(*testing.T, map[string]interface{})
	}{
		{
			name:     "duplicate",
			args:     []string{"duplicate", "31", "--org", "10", "--project", "20"},
			wantPath: "/v1/stacks/31/actions/duplicate",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["orgId"] != float64(10) || body["projectId"] != float64(20) {
					t.Fatalf("body = %#v, want org/project", body)
				}
			},
		},
		{
			name:     "sync origin",
			args:     []string{"sync-origin", "31", "--delete-services", "--delete-service-config"},
			wantPath: "/v1/stacks/31/actions/sync-origin",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["deleteStackServices"] != true || body["deleteStackServicesConfiguration"] != true {
					t.Fatalf("body = %#v, want sync delete flags", body)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var gotMethod string
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				test.assertBody(t, body)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 99, "name": "copy", "title": "Copy"})
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newStackCommand()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if gotMethod != http.MethodPost {
				t.Fatalf("method = %q, want POST", gotMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			if !strings.Contains(out.String(), "Copy") {
				t.Fatalf("output should include duplicated/synced stack: %s", out.String())
			}
		})
	}
}

func TestManifestCommandsUseRESTEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "service.yml")
	includePath := filepath.Join(tempDir, "Dockerfile")
	if err := os.WriteFile(manifestPath, []byte("name: redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(includePath, []byte("FROM redis:7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotMethod string
	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":    true,
			"resource": map[string]interface{}{"name": "redis", "title": "Redis", "type": "database", "version": "1.2.3"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newServiceCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"validate-manifest",
		manifestPath,
		"--org", "10",
		"--project", "20",
		"--version", "1.2.3",
		"--include", "Dockerfile=" + includePath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/services/actions/validate-manifest" {
		t.Fatalf("path = %q, want /v1/services/actions/validate-manifest", gotPath)
	}
	if body["manifestYaml"] != "name: redis\n" || body["version"] != "1.2.3" {
		t.Fatalf("body = %#v, want manifest and version", body)
	}
	if body["orgId"] != float64(10) || body["projectId"] != float64(20) {
		t.Fatalf("body = %#v, want org/project", body)
	}
	files, ok := body["files"].(map[string]interface{})
	if !ok || files["Dockerfile"] != "FROM redis:7\n" {
		t.Fatalf("files = %#v, want included Dockerfile", body["files"])
	}
	output := out.String()
	for _, expected := range []string{"Service manifest is valid.", "Name: redis", "Title: Redis", "Type: database", "Version: 1.2.3"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, `{"`) {
		t.Fatalf("output should not include raw JSON: %s", output)
	}
}

func TestCreateFromManifestUsesStackEndpoint(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "stack.yml")
	if err := os.WriteFile(manifestPath, []byte("name: redis-stack\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotMethod string
	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 99, "name": "redis-stack", "title": "Redis Stack"})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create-from-manifest", "-f", manifestPath, "--org", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/stacks/actions/create-from-manifest" {
		t.Fatalf("path = %q, want /v1/stacks/actions/create-from-manifest", gotPath)
	}
	if body["manifestYaml"] != "name: redis-stack\n" || body["orgId"] != float64(10) {
		t.Fatalf("body = %#v, want manifest and org", body)
	}
	if !strings.Contains(out.String(), "Redis Stack") {
		t.Fatalf("output should include created stack: %s", out.String())
	}
}

func TestValidateManifestReturnsErrorForInvalidManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "service.yml")
	if err := os.WriteFile(manifestPath, []byte("name: broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "missing workloads",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newServiceCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"validate-manifest", "-f", manifestPath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid manifest error")
	}
	if !strings.Contains(err.Error(), "service manifest is invalid") {
		t.Fatalf("Execute() error = %q, want invalid manifest error", err)
	}
	if !strings.Contains(out.String(), "Service manifest is invalid.") || !strings.Contains(out.String(), "missing workloads") {
		t.Fatalf("output should include validation failure: %s", out.String())
	}
	if strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("stderr should not include command usage for validation failures: %s", errOut.String())
	}
}

func TestValidateManifestRejectsAmbiguousManifestInput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "service.yml")
	if err := os.WriteFile(manifestPath, []byte("name: redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newServiceCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"validate-manifest", manifestPath, "--manifest", manifestPath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want manifest input conflict")
	}
	if !strings.Contains(err.Error(), "use either MANIFEST or --manifest") {
		t.Fatalf("Execute() error = %q, want manifest input conflict", err)
	}
}

func TestHelmInspectUsesRESTEndpoint(t *testing.T) {
	tempDir := t.TempDir()
	valuesPath := filepath.Join(tempDir, "values.yml")
	if err := os.WriteFile(valuesPath, []byte("architecture: standalone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotMethod string
	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"chart": map[string]interface{}{
				"name":       "redis",
				"version":    "22.0.0",
				"appVersion": "7.4.0",
				"chart":      "bitnami/redis",
				"source":     "https://charts.bitnami.com/bitnami",
			},
			"release":       "redis",
			"namespace":     "default",
			"resourceCount": 4,
			"workloads": []map[string]interface{}{
				{
					"kind": "deployment",
					"name": "redis-master",
					"containers": []map[string]interface{}{
						{
							"name":  "redis",
							"image": "registry-1.docker.io/bitnami/redis:7.4.0",
							"ports": []map[string]interface{}{
								{"name": "redis", "number": 6379, "protocol": "tcp"},
							},
							"env": []string{"ALLOW_EMPTY_PASSWORD", "REDIS_PORT_NUMBER"},
						},
					},
				},
			},
			"services": []map[string]interface{}{
				{
					"name": "redis-master",
					"ports": []map[string]interface{}{
						{"name": "tcp-redis", "number": 6379, "targetPort": "redis", "protocol": "tcp"},
					},
				},
			},
			"warnings": []string{"uses persistent volume"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newHelmCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"inspect",
		"--chart", "redis",
		"--source", "https://charts.bitnami.com/bitnami",
		"--source-name", "bitnami",
		"--version", "22.0.0",
		"--release", "redis",
		"--namespace", "default",
		"--values-yaml", valuesPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/helm-charts/actions/inspect" {
		t.Fatalf("path = %q, want /v1/helm-charts/actions/inspect", gotPath)
	}
	for key, want := range map[string]interface{}{
		"chart":      "redis",
		"source":     "https://charts.bitnami.com/bitnami",
		"sourceName": "bitnami",
		"version":    "22.0.0",
		"release":    "redis",
		"namespace":  "default",
		"valuesYaml": "architecture: standalone\n",
	} {
		if body[key] != want {
			t.Fatalf("body[%s] = %#v, want %#v in %#v", key, body[key], want, body)
		}
	}
	output := out.String()
	for _, expected := range []string{
		"redis",
		"Version: 22.0.0 / app 7.4.0",
		"Chart: bitnami/redis",
		"Rendered resources: 4",
		"Workloads: 1",
		"deployment redis-master",
		"registry-1.docker.io/bitnami/redis:7.4.0",
		"redis:6379/tcp",
		"2 env vars",
		"Services: 1",
		"Warnings:",
		"uses persistent volume",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{`{"`, `"containers"`, `"workloads"`} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output should not include raw JSON %q: %s", unwanted, output)
		}
	}
}

func TestHelmInspectAcceptsPositionalChartReferences(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantChart  string
		wantSource string
	}{
		{
			name:      "oci chart reference",
			args:      []string{"inspect", "oci://registry-1.docker.io/bitnamicharts/memcached"},
			wantChart: "oci://registry-1.docker.io/bitnamicharts/memcached",
		},
		{
			name:       "repository url with chart path",
			args:       []string{"inspect", "https://charts.bitnami.com/bitnami/redis"},
			wantChart:  "redis",
			wantSource: "https://charts.bitnami.com/bitnami",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("method = %q, want POST", r.Method)
				}
				if r.URL.Path != "/v1/helm-charts/actions/inspect" {
					t.Fatalf("path = %q, want /v1/helm-charts/actions/inspect", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"chart":         test.wantChart,
					"release":       "app",
					"namespace":     "default",
					"resourceCount": 1,
				})
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newHelmCommand()
			cmd.SetOut(io.Discard)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if body["chart"] != test.wantChart {
				t.Fatalf("chart = %#v, want %q in %#v", body["chart"], test.wantChart, body)
			}
			if test.wantSource == "" {
				if _, ok := body["source"]; ok {
					t.Fatalf("source = %#v, want omitted in %#v", body["source"], body)
				}
			} else if body["source"] != test.wantSource {
				t.Fatalf("source = %#v, want %q in %#v", body["source"], test.wantSource, body)
			}
		})
	}
}

func TestHelmChartReferenceRejectsAmbiguousSource(t *testing.T) {
	cmd := newHelmCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"inspect", "https://charts.bitnami.com/bitnami/redis", "--source", "https://charts.example.com"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want source conflict error")
	}
	if !strings.Contains(err.Error(), "do not combine --source") {
		t.Fatalf("Execute() error = %q, want source conflict error", err)
	}
}

func TestHelmScaffoldServiceWritesManifest(t *testing.T) {
	tempDir := t.TempDir()
	valuesPath := filepath.Join(tempDir, "values.json")
	outPath := filepath.Join(tempDir, "service.yml")
	if err := os.WriteFile(valuesPath, []byte(`{"architecture":"standalone","replicaCount":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotMethod string
	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"manifestYaml": "name: redis\n",
			"manifest":     map[string]interface{}{"name": "redis"},
			"analysis": map[string]interface{}{
				"warnings": []string{"chart uses persistent storage"},
			},
			"warnings": []string{"review generated service", "chart uses persistent storage"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := newHelmCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"scaffold-service",
		"oci://registry-1.docker.io/bitnamicharts/redis",
		"--values", valuesPath,
		"--service-name", "redis",
		"--service-title", "Redis",
		"--service-type", "database",
		"--icon", "redis",
		"--out", outPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/services/actions/scaffold-from-helm-chart" {
		t.Fatalf("path = %q, want /v1/services/actions/scaffold-from-helm-chart", gotPath)
	}
	chart, ok := body["chart"].(map[string]interface{})
	if !ok {
		t.Fatalf("chart body = %#v, want object", body["chart"])
	}
	if chart["chart"] != "oci://registry-1.docker.io/bitnamicharts/redis" {
		t.Fatalf("chart = %#v, want OCI redis chart", chart)
	}
	values, ok := chart["values"].(map[string]interface{})
	if !ok || values["architecture"] != "standalone" || values["replicaCount"] != float64(1) {
		t.Fatalf("values = %#v, want JSON values", chart["values"])
	}
	for key, want := range map[string]interface{}{
		"serviceName":  "redis",
		"serviceTitle": "Redis",
		"serviceType":  "database",
		"icon":         "redis",
	} {
		if body[key] != want {
			t.Fatalf("body[%s] = %#v, want %#v in %#v", key, body[key], want, body)
		}
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "name: redis\n" {
		t.Fatalf("manifest file = %q, want generated manifest", content)
	}
	if !strings.Contains(out.String(), "Wrote service manifest") {
		t.Fatalf("output should report written manifest: %s", out.String())
	}
	for _, expected := range []string{"Warnings:", "review generated service", "chart uses persistent storage"} {
		if !strings.Contains(errOut.String(), expected) {
			t.Fatalf("stderr should include %q: %s", expected, errOut.String())
		}
	}
	if strings.Count(errOut.String(), "chart uses persistent storage") != 1 {
		t.Fatalf("stderr should deduplicate warnings: %s", errOut.String())
	}
}

func TestHelmScaffoldStackWritesManifests(t *testing.T) {
	tempDir := t.TempDir()
	serviceOut := filepath.Join(tempDir, "service.yml")
	stackOut := filepath.Join(tempDir, "stack.yml")

	var gotMethod string
	var gotPath string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"serviceManifestYaml": "name: redis\n",
			"stackManifestYaml":   "name: redis-stack\n",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newHelmCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"scaffold-stack",
		"--chart", "redis",
		"--service-name", "redis",
		"--stack-name", "redis-stack",
		"--stack-title", "Redis Stack",
		"--service-out", serviceOut,
		"--stack-out", stackOut,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/stacks/actions/scaffold-from-helm-chart" {
		t.Fatalf("path = %q, want /v1/stacks/actions/scaffold-from-helm-chart", gotPath)
	}
	chart, ok := body["chart"].(map[string]interface{})
	if !ok || chart["chart"] != "redis" {
		t.Fatalf("chart body = %#v, want redis chart", body["chart"])
	}
	if body["serviceName"] != "redis" || body["stackName"] != "redis-stack" || body["stackTitle"] != "Redis Stack" {
		t.Fatalf("body = %#v, want service and stack names", body)
	}
	serviceContent, err := os.ReadFile(serviceOut)
	if err != nil {
		t.Fatal(err)
	}
	stackContent, err := os.ReadFile(stackOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(serviceContent) != "name: redis\n" || string(stackContent) != "name: redis-stack\n" {
		t.Fatalf("generated files = %q / %q, want service and stack manifests", serviceContent, stackContent)
	}
	if !strings.Contains(out.String(), "Wrote service manifest") || !strings.Contains(out.String(), "Wrote stack manifest") {
		t.Fatalf("output should report written manifests: %s", out.String())
	}
}

func TestClusterActionAndDeleteCommandsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPath  string
		wantQuery string
	}{
		{name: "upgrade infra", args: []string{"upgrade-infra", "31"}, wantPath: "/v1/clusters/31/actions/upgrade-infra"},
		{name: "upgrade infra apps", args: []string{"upgrade-infra-apps", "31"}, wantPath: "/v1/clusters/31/actions/upgrade-infra-apps"},
		{name: "delete force", args: []string{"delete", "31", "--force", "-y"}, wantPath: "/v1/clusters/31", wantQuery: "force=true"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotMethod string
			var gotPath string
			var gotQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newClusterCommand()
			cmd.SetOut(io.Discard)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if test.name == "delete force" && gotMethod != http.MethodDelete {
				t.Fatalf("method = %q, want DELETE", gotMethod)
			}
			if test.name != "delete force" && gotMethod != http.MethodPost {
				t.Fatalf("method = %q, want POST", gotMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			if gotQuery != test.wantQuery {
				t.Fatalf("query = %q, want %s", gotQuery, test.wantQuery)
			}
		})
	}
}

func TestAppCreateUsesPublicAPIShape(t *testing.T) {
	var requestedMethod string
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" {
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("task appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		requestedMethod = r.Method
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    101,
			"name":  "drupal",
			"title": "Drupal",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--org", "10",
		"--project", "12",
		"--env", "22",
		"--cluster", "33",
		"--stack", "7",
		"--stack-rev", "70",
		"--name", "drupal",
		"--title", "Drupal",
		"--instance-name", "prod",
		"--instance-title", "Production",
		"--domain", "example.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/apps" {
		t.Fatalf("path = %q, want /v1/apps", requestedPath)
	}
	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["projectId"] != float64(12) {
		t.Fatalf("projectId = %#v, want 12", body["projectId"])
	}
	if body["envId"] != float64(22) {
		t.Fatalf("envId = %#v, want 22", body["envId"])
	}
	if body["clusterId"] != float64(33) {
		t.Fatalf("clusterId = %#v, want 33", body["clusterId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["name"] != "drupal" || body["title"] != "Drupal" {
		t.Fatalf("name/title body = %#v", body)
	}
	if body["instanceName"] != "prod" || body["instanceTitle"] != "Production" {
		t.Fatalf("instance name/title body = %#v", body)
	}
	if body["domain"] != "example.com" {
		t.Fatalf("domain = %#v, want example.com", body["domain"])
	}
	if _, ok := body["stackId"]; ok {
		t.Fatalf("body should not include stackId: %#v", body)
	}
	if _, ok := body["clusterApp"]; ok {
		t.Fatalf("body should not include clusterApp: %#v", body)
	}
}

func TestAppCreateRequiresCluster(t *testing.T) {
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatalf("unexpected request path %q", r.URL.Path)
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--name", "site",
		"--instance", "prod",
		"--env", "prod",
		"--stack", "drupal",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want --cluster required")
	}
	if !strings.Contains(err.Error(), "--cluster is required") {
		t.Fatalf("Execute() error = %q, want --cluster required", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestAppInstanceCreateUsesPublicAPIShape(t *testing.T) {
	var requestedMethod string
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" {
			if got := r.URL.Query().Get("appInstanceId"); got != "101" {
				t.Fatalf("task appInstanceId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		requestedMethod = r.Method
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    101,
			"name":  "prod",
			"title": "Production",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--app", "11",
		"--env", "22",
		"--cluster", "33",
		"--stack", "7",
		"--stack-rev", "70",
		"--instance-name", "prod",
		"--instance-title", "Production",
		"--domain", "example.com",
		"--region", "us",
		"--zone", "us-a",
		"--cluster-app",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/app-instances" {
		t.Fatalf("path = %q, want /v1/app-instances", requestedPath)
	}
	if body["appId"] != float64(11) {
		t.Fatalf("appId = %#v, want 11", body["appId"])
	}
	if body["envId"] != float64(22) {
		t.Fatalf("envId = %#v, want 22", body["envId"])
	}
	if body["clusterId"] != float64(33) {
		t.Fatalf("clusterId = %#v, want 33", body["clusterId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["instanceName"] != "prod" || body["instanceTitle"] != "Production" {
		t.Fatalf("instance name/title body = %#v", body)
	}
	if body["domain"] != "example.com" {
		t.Fatalf("domain = %#v, want example.com", body["domain"])
	}
	for _, key := range []string{"stackId", "name", "title", "mainDomain", "region", "zone", "clusterApp"} {
		if _, ok := body[key]; ok {
			t.Fatalf("body should not include %s: %#v", key, body)
		}
	}
}

func TestAppCreateResolvesEnvAndStackNames(t *testing.T) {
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/envs/by-name/prod":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("env orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   22,
				"name": "prod",
			})
		case "/v1/stacks/by-name/drupal":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("stack orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":           7,
				"name":         "drupal",
				"currentRevId": 70,
			})
		case "/v1/clusters/by-name/primary":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("cluster orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   33,
				"name": "primary",
			})
		case "/v1/apps":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %q, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    101,
				"name":  "site",
				"title": "Site",
			})
		case "/v1/tasks":
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("task appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--org", "10",
		"--project", "12",
		"--env", "prod",
		"--cluster", "primary",
		"--stack", "drupal",
		"--name", "site",
		"--instance", "prod",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if body["envId"] != float64(22) {
		t.Fatalf("envId = %#v, want 22", body["envId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["clusterId"] != float64(33) {
		t.Fatalf("clusterId = %#v, want 33", body["clusterId"])
	}
	if body["instanceName"] != "prod" {
		t.Fatalf("instanceName = %#v, want prod", body["instanceName"])
	}
}

func TestAppCreateResolvesBareStackNameWithCurrentOrgPrefix(t *testing.T) {
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 10, "name": "curorg"},
			})
		case "/v1/envs/by-name/prod":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("env orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   22,
				"name": "prod",
			})
		case "/v1/stacks/by-name/drupal":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("direct stack orgId = %q, want 10", got)
			}
			http.Error(w, "not found", http.StatusNotFound)
		case "/v1/orgs/10":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   10,
				"name": "curorg",
			})
		case "/v1/stacks/by-name/curorg/drupal":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("prefixed stack orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":           7,
				"name":         "curorg/drupal",
				"currentRevId": 70,
			})
		case "/v1/apps":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %q, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    101,
				"name":  "site",
				"title": "Site",
			})
		case "/v1/tasks":
			if got := r.URL.Query().Get("appId"); got != "101" {
				t.Fatalf("task appId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--env", "prod",
		"--cluster", "33",
		"--stack", "drupal",
		"--name", "site",
		"--instance", "prod",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
}

func TestAppCreateReportsPrefixedStackLookupError(t *testing.T) {
	var postRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/stacks/by-name/missing":
			http.Error(w, "not found", http.StatusNotFound)
		case "/v1/orgs/10":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   10,
				"name": "curorg",
			})
		case "/v1/stacks/by-name/curorg/missing":
			http.Error(w, "not found", http.StatusNotFound)
		case "/v1/apps":
			postRequests++
			t.Fatalf("app should not be created when stack lookup fails")
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--org", "10",
		"--env", "22",
		"--cluster", "33",
		"--stack", "missing",
		"--name", "site",
		"--instance", "prod",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want stack lookup error")
	}
	if !strings.Contains(err.Error(), `resolve --stack "curorg/missing"`) || !strings.Contains(err.Error(), "404") {
		t.Fatalf("Execute() error = %q, want prefixed stack 404 context", err)
	}
	if postRequests != 0 {
		t.Fatalf("postRequests = %d, want 0", postRequests)
	}
}

func TestAppInstanceCreateResolvesAppEnvAndStackNames(t *testing.T) {
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/by-name/drupal":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("app orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   11,
				"name": "drupal",
			})
		case "/v1/envs/by-name/prod":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("env orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   22,
				"name": "prod",
			})
		case "/v1/stacks/by-name/drupal":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("stack orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   7,
				"name": "drupal",
				"stackRev": map[string]interface{}{
					"id": 70,
				},
			})
		case "/v1/clusters/by-name/primary":
			if got := r.URL.Query().Get("orgId"); got != "10" {
				t.Fatalf("cluster orgId = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   33,
				"name": "primary",
			})
		case "/v1/app-instances":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %q, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    101,
				"name":  "prod",
				"title": "Production",
			})
		case "/v1/tasks":
			if got := r.URL.Query().Get("appInstanceId"); got != "101" {
				t.Fatalf("task appInstanceId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--org", "10",
		"--app", "drupal",
		"--env", "prod",
		"--cluster", "primary",
		"--stack", "drupal",
		"--instance", "prod",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if body["appId"] != float64(11) {
		t.Fatalf("appId = %#v, want 11", body["appId"])
	}
	if body["envId"] != float64(22) {
		t.Fatalf("envId = %#v, want 22", body["envId"])
	}
	if body["clusterId"] != float64(33) {
		t.Fatalf("clusterId = %#v, want 33", body["clusterId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["instanceName"] != "prod" {
		t.Fatalf("instanceName = %#v, want prod", body["instanceName"])
	}
}

func TestAppListEnrichesStackRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			if got := r.URL.Query().Get("orgId"); got != "123" {
				t.Fatalf("orgId = %q, want 123", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":      1,
					"name":    "drupal",
					"title":   "Drupal",
					"status":  "running",
					"stackId": 7,
				},
			})
		case "/v1/stacks/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7, "title": "Drupal Stack"})
		case "/v1/app-instances":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 21, "appId": 1},
				{"id": 22, "appId": 1},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack", "Drupal Stack", "instances", "2"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"stackId", "cluster app", "7"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("app list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestAppListShowsStackFromStackRevision(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     1,
					"name":   "drupal",
					"title":  "Drupal",
					"status": "running",
					"stackRev": map[string]interface{}{
						"stack": map[string]interface{}{
							"id":    7,
							"title": "Drupal Stack",
						},
					},
				},
			})
		case "/v1/app-instances":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"Drupal Stack", "instances", "0"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app list output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId") {
		t.Fatalf("app list output should not include raw stackId: %s", output)
	}
}

func TestAppListShowsStackFromInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			if got := r.URL.Query().Get("orgId"); got != "123" {
				t.Fatalf("orgId = %q, want 123", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     1,
					"name":   "drupal",
					"title":  "Drupal",
					"status": "running",
				},
			})
		case "/v1/app-instances":
			if got := r.URL.Query().Get("orgId"); got != "123" {
				t.Fatalf("orgId = %q, want 123", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":    21,
					"appId": 1,
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Drupal Stack",
					},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack", "Drupal Stack", "instances", "1"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app list output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId") || strings.Contains(output, "7") {
		t.Fatalf("app list output should not include raw stack id: %s", output)
	}
}

func TestAppListShowsStackFromEmbeddedInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     1,
					"name":   "drupal",
					"title":  "Drupal",
					"status": "running",
					"instances": []map[string]interface{}{
						{
							"id": 21,
							"stack": map[string]interface{}{
								"id":    7,
								"title": "Drupal Stack",
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack", "Drupal Stack", "instances", "1"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app list output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId") || strings.Contains(output, "7") {
		t.Fatalf("app list output should not include raw stack id: %s", output)
	}
}

func TestAppGetEnrichesStackRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      1,
				"name":    "drupal",
				"title":   "Drupal",
				"status":  "running",
				"stackId": 7,
			})
		case "/v1/stacks/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7, "title": "Drupal Stack"})
		case "/v1/app-instances":
			if got := r.URL.Query().Get("appId"); got != "1" {
				t.Fatalf("appId = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     21,
					"title":  "Production",
					"status": "running",
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack:", "Drupal Stack", "stack id:", "7", "instances:", "Production [21] (running)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId:") {
		t.Fatalf("app get output should not include raw stackId: %s", output)
	}
}

func TestAppGetShowsStackFromInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     1,
				"name":   "drupal",
				"title":  "Drupal",
				"status": "running",
			})
		case "/v1/app-instances":
			if got := r.URL.Query().Get("appId"); got != "1" {
				t.Fatalf("appId = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     21,
					"title":  "Production",
					"status": "running",
					"appId":  1,
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Drupal Stack",
					},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack:", "Drupal Stack", "stack id:", "7", "instances:", "Production [21] (running)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId:") {
		t.Fatalf("app get output should not include raw stackId: %s", output)
	}
}

func TestAppGetShowsStackFromEmbeddedInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     1,
				"name":   "drupal",
				"title":  "Drupal",
				"status": "running",
				"instances": []map[string]interface{}{
					{
						"id":     21,
						"title":  "Production",
						"status": "running",
						"stack": map[string]interface{}{
							"id":    7,
							"title": "Drupal Stack",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"stack:", "Drupal Stack", "stack id:", "7", "instances:", "Production [21] (running)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "stackId:") {
		t.Fatalf("app get output should not include raw stackId: %s", output)
	}
}

func TestAppStatusComposesInstanceOperationalSummary(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     1,
				"title":  "Drupal",
				"status": "running",
				"orgId":  123,
			})
		case "/v1/app-instances":
			if got := r.URL.Query().Get("appId"); got != "1" {
				t.Fatalf("appId = %q, want 1", got)
			}
			if got := r.URL.Query().Get("orgId"); got != "123" {
				t.Fatalf("orgId = %q, want 123", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 21, "title": "Production", "status": "running"},
			})
		case "/v1/app-services":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("app service appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 31, "title": "PHP", "status": "ok", "needsRebuild": true},
			})
		case "/v1/app-routes":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("route appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 41, "host": "example.com", "status": "active"},
			})
		case "/v1/app-ports":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("port appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 51, "number": 8080},
			})
		case "/v1/app-builds":
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("build pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 61, "number": 5, "status": "completed", "gitRef": "main", "commitHash": "abcdef123456", "createdAt": "2026-01-02T03:04:05Z"},
				},
			})
		case "/v1/app-deployments":
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 71, "number": 4, "status": "completed", "startedAt": "2026-01-02T03:04:00Z", "endedAt": "2026-01-02T03:06:00Z", "createdAt": "2026-01-02T03:03:00Z"},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"status", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"instances:", "Production [21] (running)", "service status:", "1 services ok", "route status:", "1 routes active", "latest build:", "#5 completed main abcdef12", "latest deployment:", "#4 completed 2m", "needs:", "1 services need rebuild"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app status output should include %q: %s", expected, output)
		}
	}
}

func TestInstanceStatusComposesOperationalSummary(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-instances/21":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     21,
				"title":  "Production",
				"status": "running",
			})
		case "/v1/app-services":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 31, "title": "PHP", "status": "ok", "needsRedeploy": true},
				{"id": 32, "title": "Cron", "status": "ok", "disabled": true},
			})
		case "/v1/app-routes":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 41, "host": "example.com", "status": "active", "disabled": true},
			})
		case "/v1/app-ports":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 51, "number": 8080},
				{"id": 52, "number": 8443},
			})
		case "/v1/app-builds":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 61, "number": 5, "status": "completed", "gitRef": "main", "commitHash": "abcdef123456", "createdAt": "2026-01-02T03:04:05Z"},
				},
			})
		case "/v1/app-deployments":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 71, "number": 4, "status": "completed", "startedAt": "2026-01-02T03:04:00Z", "endedAt": "2026-01-02T03:06:00Z", "createdAt": "2026-01-02T03:03:00Z"},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"status", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"service status:", "2 services (disabled=1, ok=1)", "route status:", "1 routes disabled", "port status:", "2 ports", "latest build:", "#5 completed main abcdef12", "latest deployment:", "#4 completed 2m", "needs:", "1 services need redeploy, 1 services disabled, 1 routes disabled"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("instance status output should include %q: %s", expected, output)
		}
	}
}

func TestRouteListShowsCompactRouteSummary(t *testing.T) {
	var out bytes.Buffer
	lastSyncedAt := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	createdAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	updatedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	certExpiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-routes":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":       1,
					"host":     "example.com",
					"path":     "/docs",
					"pathType": "prefix",
					"action":   "proxy",
					"status":   "active",
					"appService": map[string]interface{}{
						"title": "Nginx",
					},
					"cert": map[string]interface{}{
						"host":      "example.com",
						"status":    "ready",
						"issuer":    "Let's Encrypt",
						"expiresAt": certExpiresAt,
					},
					"portId":       55,
					"primary":      true,
					"private":      true,
					"lastSyncedAt": lastSyncedAt,
					"createdAt":    createdAt,
					"updatedAt":    updatedAt,
				},
			})
		case "/v1/app-ports/55":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 55, "number": 8080})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"route", "list", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"service", "route", "action", "cert", "primary", "private", "status", "updated at", "Nginx", "example.com/docs", "proxy", "example.com (Let's Encrypt, ready)", "true", "active", "ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("route list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"port", "portId", "55", "cert issuer", "cert status", "cert expires at", "last synced at", "created at", lastSyncedAt, createdAt, certExpiresAt, updatedAt} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("route list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestPortListShowsReadableRelations(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-ports":
			if got := r.URL.Query().Get("appInstanceId"); got != "21" {
				t.Fatalf("appInstanceId = %q, want 21", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":            55,
					"name":          "http",
					"number":        8080,
					"publicPort":    443,
					"protocol":      "http",
					"private":       false,
					"appServiceId":  22,
					"appInstanceId": 21,
					"createdAt":     "2026-01-02T03:04:05Z",
					"updatedAt":     time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
				},
			})
		case "/v1/app-services/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "Nginx"})
		case "/v1/app-instances/21":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 21, "title": "Production"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"port", "list", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"service", "name", "number", "public port", "private", "protocol", "updated at", "8080", "443", "http", "Nginx", "ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("port list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"instance", "Production", "created at", "appServiceId", "appInstanceId"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("port list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTopLevelRouteListUsesInstanceFlag(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppRouteCommand("route", []string{"routes"}, "Manage app routes", instanceFilterFlag)
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"list", "--instance", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-routes" {
		t.Fatalf("path = %q, want /v1/app-routes", requestedPath)
	}
	if requestedQuery != "appInstanceId=21" {
		t.Fatalf("query = %q, want appInstanceId=21", requestedQuery)
	}
}

func TestRouteGetShowsCertSummary(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/app-routes/1" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     1,
			"host":   "example.com",
			"status": "active",
			"cert": map[string]interface{}{
				"issuer": "Let's Encrypt",
				"status": "ready",
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppRouteCommand("route", []string{"routes"}, "Manage app routes", instanceFilterFlag)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"cert:", "Let's Encrypt, ready"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("route get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"cert issuer:", "cert status:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("route get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTopLevelPortListUsesInstanceFlag(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppPortCommand("port", []string{"ports"}, "Manage app ports", instanceFilterFlag)
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"list", "--instance", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-ports" {
		t.Fatalf("path = %q, want /v1/app-ports", requestedPath)
	}
	if requestedQuery != "appInstanceId=21" {
		t.Fatalf("query = %q, want appInstanceId=21", requestedQuery)
	}
}

func TestTopLevelApsListUsesInstanceFlag(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppServiceCommand("aps", nil, "Manage app services", instanceFilterFlag)
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"list", "--instance", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-services" {
		t.Fatalf("path = %q, want /v1/app-services", requestedPath)
	}
	if requestedQuery != "appInstanceId=21" {
		t.Fatalf("query = %q, want appInstanceId=21", requestedQuery)
	}
}

func TestAppServiceCommandExposesChildOperations(t *testing.T) {
	cmd := newAppServiceCommand("aps", nil, "Manage app services", instanceFilterFlag)
	names := make(map[string]bool)
	for _, child := range cmd.Commands() {
		names[child.Name()] = true
	}
	for _, name := range []string{
		"list",
		"get",
		"update",
		"action",
		"env-var",
		"helm-value",
		"token",
		"annotation",
		"integration",
		"setting",
		"config",
		"link",
		"container",
		"resources",
		"database",
		"cron-schedule",
		"cron-job",
		"log-stream",
	} {
		if !names[name] {
			t.Fatalf("missing app service subcommand %q", name)
		}
	}
}

func TestAppServiceChildOperationsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantQuery  string
		assertBody func(*testing.T, map[string]interface{})
		response   interface{}
		wantOutput []string
	}{
		{
			name:       "env create",
			args:       []string{"env-var", "create", "21", "--name", "APP_ENV", "--value", "prod", "--runtime"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/app-services/21/env-vars",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"name": "APP_ENV", "value": "prod", "secret": false, "runtime": true} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 61, "name": "APP_ENV", "value": "prod", "runtime": true},
			wantOutput: []string{"APP_ENV", "prod", "runtime"},
		},
		{
			name:       "setting set",
			args:       []string{"setting", "set", "21", "php_version", "--value", "8.3"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/app-services/21/settings/php_version",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["value"] != "8.3" {
					t.Fatalf("value = %#v, want 8.3; body=%#v", body["value"], body)
				}
			},
			response:   map[string]interface{}{"id": 62, "name": "php_version", "value": "8.3", "runtime": true, "build": false},
			wantOutput: []string{"php_version", "8.3"},
		},
		{
			name:       "resources set",
			args:       []string{"resources", "set", "21", "--workload", "web", "--container", "php", "--request-cpu", "1", "--limit-mem", "512"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/app-services/21/resources",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"workload": "web", "container": "php", "requestCPU": float64(1), "limitMem": float64(512)} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"success": true},
			wantOutput: []string{"success", "true"},
		},
		{
			name:       "database set",
			args:       []string{"database", "set", "21", "--database-db", "33", "--database-user", "44"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/app-services/21/database",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"databaseDbId": float64(33), "databaseUserId": float64(44)} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 21, "title": "PHP", "status": "running", "version": "8.3"},
			wantOutput: []string{"PHP", "running", "8.3"},
		},
		{
			name:       "cron run",
			args:       []string{"cron-schedule", "run", "81"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/app-service-cron-schedules/81/run",
			response:   map[string]interface{}{"id": 91, "title": "Run cron", "status": "running"},
			wantOutput: []string{"Run cron", "running"},
		},
		{
			name:       "cron job list",
			args:       []string{"cron-job", "list", "21", "--schedule", "81", "--page", "2", "--page-size", "10"},
			wantMethod: http.MethodGet,
			wantPath:   "/v1/app-service-cron-jobs",
			wantQuery:  "appServiceId=21&page=2&pageSize=10&scheduleId=81",
			response: map[string]interface{}{
				"items": []map[string]interface{}{{"id": 92, "title": "Run cron", "status": "succeeded", "appService": "PHP", "scheduleId": 81}},
			},
			wantOutput: []string{"Run cron", "succeeded", "schedule id"},
		},
		{
			name:       "log stream create",
			args:       []string{"log-stream", "create", "21", "--workload", "web", "--container", "php"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/app-services/21/log-streams",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"workload": "web", "container": "php"} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 101},
			wantOutput: []string{"101"},
		},
		{
			name:       "log stream start",
			args:       []string{"log-stream", "start", "101"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/log-streams/101/start",
			response:   map[string]interface{}{"success": true},
			wantOutput: []string{"success", "true"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var gotMethod string
			var gotPath string
			var gotQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				if test.assertBody != nil {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("Decode() error = %v", err)
					}
					test.assertBody(t, body)
				}
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newAppServiceCommand("aps", nil, "Manage app services", instanceFilterFlag)
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if gotMethod != test.wantMethod {
				t.Fatalf("method = %q, want %s", gotMethod, test.wantMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			if test.wantQuery != "" && gotQuery != test.wantQuery {
				t.Fatalf("query = %q, want %s", gotQuery, test.wantQuery)
			}
			output := out.String()
			for _, expected := range test.wantOutput {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
		})
	}
}

func TestInstanceFlagSupportsShortI(t *testing.T) {
	for _, test := range []struct {
		name      string
		cmd       *cobra.Command
		args      []string
		wantPath  string
		wantQuery string
	}{
		{name: "route", cmd: newAppRouteCommand("route", nil, "Manage app routes", instanceFilterFlag), args: []string{"list", "-i", "21"}, wantPath: "/v1/app-routes", wantQuery: "appInstanceId=21"},
		{name: "aps", cmd: newAppServiceCommand("aps", nil, "Manage app services", instanceFilterFlag), args: []string{"list", "-i", "21"}, wantPath: "/v1/app-services", wantQuery: "appInstanceId=21"},
		{name: "port", cmd: newAppPortCommand("port", nil, "Manage app ports", instanceFilterFlag), args: []string{"list", "-i", "21"}, wantPath: "/v1/app-ports", wantQuery: "appInstanceId=21"},
		{name: "cert", cmd: newAppCertCommand("cert", nil, "Manage app certificates", instanceFilterFlag), args: []string{"list", "-i", "21"}, wantPath: "/v1/certs", wantQuery: ""},
		{name: "build", cmd: newBuildCommand(), args: []string{"list", "-i", "21"}, wantPath: "/v1/app-builds", wantQuery: "appInstanceId=21"},
		{name: "task", cmd: newTaskCommand(), args: []string{"list", "-i", "21"}, wantPath: "/v1/tasks", wantQuery: "appInstanceId=21"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestedPath string
			var requestedQuery string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedPath = r.URL.Path
				requestedQuery = r.URL.RawQuery
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			test.cmd.SetOut(io.Discard)
			test.cmd.SetArgs(test.args)
			if err := test.cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if requestedPath != test.wantPath {
				t.Fatalf("path = %q, want %s", requestedPath, test.wantPath)
			}
			if requestedQuery != test.wantQuery {
				t.Fatalf("query = %q, want %s", requestedQuery, test.wantQuery)
			}
		})
	}
}

func TestCommandsDoNotDefaultToListSubcommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{
			name: "route",
			cmd: func() *cobra.Command {
				return newAppRouteCommand("route", nil, "Manage app routes", instanceFilterFlag)
			},
			args: []string{"-i", "21"},
		},
		{
			name: "aps",
			cmd: func() *cobra.Command {
				return newAppServiceCommand("aps", nil, "Manage app services", instanceFilterFlag)
			},
			args: []string{"-i", "21"},
		},
		{
			name: "cert",
			cmd: func() *cobra.Command {
				return newAppCertCommand("cert", nil, "Manage app certificates", instanceFilterFlag)
			},
			args: []string{"-i", "21"},
		},
		{
			name: "build",
			cmd:  newBuildCommand,
			args: []string{"-i", "21"},
		},
		{
			name: "deployment",
			cmd:  newDeploymentCommand,
			args: []string{"-i", "21"},
		},
		{
			name: "backup",
			cmd:  newBackupCommand,
			args: []string{"-i", "21"},
		},
		{
			name: "import",
			cmd:  newImportCommand,
			args: []string{"-i", "21"},
		},
		{
			name: "task",
			cmd:  newTaskCommand,
			args: []string{"-i", "21"},
		},
		{
			name: "database db",
			cmd:  newDatabaseCommand,
			args: []string{"db", "33"},
		},
		{
			name: "database user",
			cmd:  newDatabaseCommand,
			args: []string{"user", "33"},
		},
		{
			name: "cluster app",
			cmd:  newClusterCommand,
			args: []string{"app", "101"},
		},
		{
			name: "instance backup",
			cmd:  func() *cobra.Command { return newAppInstanceCommand("instance", "Manage app instances") },
			args: []string{"backup", "21"},
		},
		{
			name: "instance import",
			cmd:  func() *cobra.Command { return newAppInstanceCommand("instance", "Manage app instances") },
			args: []string{"import", "21"},
		},
		{
			name: "task job",
			cmd:  newTaskCommand,
			args: []string{"job", "42"},
		},
		{
			name: "task step",
			cmd:  newTaskCommand,
			args: []string{"step", "42"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests int

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				t.Fatalf("unexpected API request %s?%s", r.URL.Path, r.URL.RawQuery)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := test.cmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("Execute() error = nil, want explicit subcommand error")
			}

			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestInstanceBackupAndImportListUseInstanceArg(t *testing.T) {
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	backupCmd := newAppInstanceCommand("instance", "Manage app instances")
	backupCmd.SetOut(io.Discard)
	backupCmd.SetArgs([]string{"backup", "list", "21"})
	if err := backupCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	importCmd := newAppInstanceCommand("instance", "Manage app instances")
	importCmd.SetOut(io.Discard)
	importCmd.SetArgs([]string{"import", "list", "21"})
	if err := importCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	want := []string{"/v1/backups?appInstanceId=21", "/v1/imports?appInstanceId=21"}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestBackupAndImportListRequireScope(t *testing.T) {
	for _, test := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "backup", cmd: newBackupCommand()},
		{name: "import", cmd: newImportCommand()},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				t.Fatalf("unexpected API request %s?%s", r.URL.Path, r.URL.RawQuery)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			test.cmd.SetOut(io.Discard)
			test.cmd.SetErr(io.Discard)
			test.cmd.SetArgs([]string{"list"})
			err := test.cmd.Execute()
			if err == nil {
				t.Fatal("Execute() error = nil, want required scope error")
			}
			if !strings.Contains(err.Error(), "one of --instance, --service, --database, or --database-db is required") {
				t.Fatalf("Execute() error = %q, want required scope error", err)
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestNewCatalogCommandsExposeBasicReadOperations(t *testing.T) {
	for _, test := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "provider", cmd: newProviderCommand()},
		{name: "stack", cmd: newStackCommand()},
		{name: "service", cmd: newServiceCommand()},
	} {
		t.Run(test.name, func(t *testing.T) {
			names := make(map[string]bool)
			for _, cmd := range test.cmd.Commands() {
				names[cmd.Name()] = true
			}
			for _, name := range []string{"list", "get"} {
				if !names[name] {
					t.Fatalf("missing %s subcommand %q", test.name, name)
				}
			}
		})
	}
}

func TestStackCommandExposesServiceOperations(t *testing.T) {
	stack := newStackCommand()
	names := make(map[string]bool)
	var serviceCmd *cobra.Command
	for _, cmd := range stack.Commands() {
		names[cmd.Name()] = true
		if cmd.Name() == "service" {
			serviceCmd = cmd
		}
	}
	for _, name := range []string{"list", "get", "service"} {
		if !names[name] {
			t.Fatalf("missing stack subcommand %q", name)
		}
	}
	if serviceCmd == nil {
		t.Fatal("missing stack service command")
	}

	serviceNames := make(map[string]bool)
	for _, cmd := range serviceCmd.Commands() {
		serviceNames[cmd.Name()] = true
	}
	for _, name := range []string{
		"list",
		"create",
		"update",
		"delete",
		"env-var",
		"helm-value",
		"token",
		"annotation",
		"integration",
		"link",
		"volume",
		"setting",
		"config",
		"resources",
		"options",
		"cron-schedule",
	} {
		if !serviceNames[name] {
			t.Fatalf("missing stack service subcommand %q", name)
		}
	}

	aliases := strings.Join(serviceCmd.Aliases, ",")
	for _, expected := range []string{"services", "stack-service", "stack-services"} {
		if !strings.Contains(aliases, expected) {
			t.Fatalf("stack service aliases should include %q: %s", expected, aliases)
		}
	}
}

func TestStackServiceListUsesStackRevisionAndFormatsServiceRevision(t *testing.T) {
	var out bytes.Buffer
	updatedAt := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":                11,
				"name":              "php",
				"title":             "PHP",
				"type":              "SERVICE",
				"serviceRevId":      12,
				"serviceRevTitle":   "PHP",
				"serviceRevVersion": "8.3",
				"serviceRevNumber":  4,
				"replicas":          2,
				"required":          true,
				"disabled":          false,
				"main":              true,
				"updatedAt":         updatedAt,
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"services", "list", "--stack-rev", "31"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/stack-services" {
		t.Fatalf("path = %q, want /v1/stack-services", requestedPath)
	}
	if requestedQuery != "stackRevId=31" {
		t.Fatalf("query = %q, want stackRevId=31", requestedQuery)
	}
	output := out.String()
	for _, expected := range []string{"service rev", "PHP 8.3 #4", "replicas", "required", "disabled", "main", "updated at", "ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack service list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"serviceRevId", "12", updatedAt} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stack service list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestStackServiceListAcceptsPositionalStackIDAndUsesCurrentRevision(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/stacks/21":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    21,
				"title": "Drupal",
				"revId": 31,
			})
		case "/v1/stack-services":
			if got := r.URL.Query().Get("stackRevId"); got != "31" {
				t.Fatalf("stackRevId = %q, want 31", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":                11,
					"name":              "php",
					"title":             "PHP",
					"type":              "SERVICE",
					"serviceRevTitle":   "PHP",
					"serviceRevVersion": "8.3",
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"service", "list", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{"/v1/stacks/21?", "/v1/stack-services?stackRevId=31"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	output := out.String()
	for _, expected := range []string{"PHP", "PHP 8.3"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack service list output should include %q: %s", expected, output)
		}
	}
}

func TestStackServiceListAcceptsPositionalStackRevisionID(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/stacks/31":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "stack not found"})
		case "/v1/stack-services":
			if got := r.URL.Query().Get("stackRevId"); got != "31" {
				t.Fatalf("stackRevId = %q, want 31", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":                11,
					"name":              "php",
					"title":             "PHP",
					"type":              "SERVICE",
					"serviceRevTitle":   "PHP",
					"serviceRevVersion": "8.3",
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"service", "list", "31"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{"/v1/stacks/31?", "/v1/stack-services?stackRevId=31"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	if output := out.String(); !strings.Contains(output, "PHP 8.3") {
		t.Fatalf("stack service list output should include service revision: %s", output)
	}
}

func TestStackServiceListRejectsAmbiguousStackSelectors(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatalf("unexpected request path %q", r.URL.Path)
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"service", "list", "31", "--stack", "21"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want ambiguous selector error")
	}
	if !strings.Contains(err.Error(), "use only one of ID argument, --stack, or --stack-rev") {
		t.Fatalf("Execute() error = %q, want ambiguous selector error", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestStackServiceOperationsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		assertBody func(*testing.T, map[string]interface{})
		response   map[string]interface{}
		wantOutput []string
	}{
		{
			name:       "create",
			args:       []string{"service", "create", "--stack", "21", "--service", "22", "--name", "php", "--title", "PHP", "--required", "--replicas", "1"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/stack-services",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"stackId": float64(21), "serviceId": float64(22), "name": "php", "title": "PHP", "required": true, "replicas": float64(1)} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response: map[string]interface{}{
				"id":                11,
				"name":              "php",
				"title":             "PHP",
				"type":              "SERVICE",
				"serviceRevTitle":   "PHP",
				"serviceRevVersion": "8.3",
			},
			wantOutput: []string{"PHP 8.3"},
		},
		{
			name:       "update",
			args:       []string{"services", "update", "11", "--replicas", "2", "--disabled"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/stack-services/11",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"replicas": float64(2), "disabled": true} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
				if _, ok := body["required"]; ok {
					t.Fatalf("required should not be sent unless changed: %#v", body)
				}
			},
			response: map[string]interface{}{
				"id":                11,
				"name":              "php",
				"title":             "PHP",
				"type":              "SERVICE",
				"serviceRevTitle":   "PHP",
				"serviceRevVersion": "8.3",
				"replicas":          2,
				"disabled":          true,
			},
			wantOutput: []string{"PHP 8.3", "true"},
		},
		{
			name:       "delete",
			args:       []string{"stack-services", "delete", "11", "-y"},
			wantMethod: http.MethodDelete,
			wantPath:   "/v1/stack-services/11",
			response:   map[string]interface{}{"success": true},
			wantOutput: []string{"success", "true"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var gotMethod string
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				if test.assertBody != nil {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("Decode() error = %v", err)
					}
					test.assertBody(t, body)
				}
				status := http.StatusOK
				if r.Method == http.MethodPost {
					status = http.StatusCreated
				}
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newStackCommand()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if gotMethod != test.wantMethod {
				t.Fatalf("method = %q, want %s", gotMethod, test.wantMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			output := out.String()
			for _, expected := range test.wantOutput {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
		})
	}
}

func TestStackServiceChildOperationsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		assertBody func(*testing.T, map[string]interface{})
		response   interface{}
		wantOutput []string
	}{
		{
			name:       "env list",
			args:       []string{"service", "env-var", "list", "11"},
			wantMethod: http.MethodGet,
			wantPath:   "/v1/stack-services/11/env-vars",
			response: []map[string]interface{}{
				{"id": 31, "name": "DATABASE_URL", "valueSecretId": 7, "envType": "RUNTIME", "createdAt": time.Now().UTC().Format(time.RFC3339)},
			},
			wantOutput: []string{"DATABASE_URL", "secret", "true", "env type"},
		},
		{
			name:       "helm create",
			args:       []string{"service", "helm-value", "create", "11", "--name", "image.tag", "--value", "8.3", "--secret", "--env-type", "RUNTIME"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/stack-services/11/helm-values",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"name": "image.tag", "value": "8.3", "secret": true, "envType": "RUNTIME"} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 32, "name": "image.tag", "value": "8.3", "envType": "RUNTIME"},
			wantOutput: []string{"image.tag", "8.3"},
		},
		{
			name:       "token update",
			args:       []string{"service", "token", "update", "41", "--secret", "--regex", "[a-z0-9]+"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/stack-service-tokens/41",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"secret": true, "regex": "[a-z0-9]+"} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 41, "name": "TOKEN", "regex": "[a-z0-9]+", "valueSecretId": 9},
			wantOutput: []string{"TOKEN", "[a-z0-9]+", "true"},
		},
		{
			name:       "link set",
			args:       []string{"service", "link", "set", "11", "database", "--linked-service", "12"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/stack-services/11/links/database",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["linkedStackServiceId"] != float64(12) {
					t.Fatalf("linkedStackServiceId = %#v, want 12; body=%#v", body["linkedStackServiceId"], body)
				}
			},
			response:   map[string]interface{}{"success": true},
			wantOutput: []string{"success", "true"},
		},
		{
			name:       "options set",
			args:       []string{"service", "options", "set", "11", "--option", "8.3:true:false", "--option", "8.2:false:true"},
			wantMethod: http.MethodPut,
			wantPath:   "/v1/stack-services/11/options",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				options, ok := body["options"].([]interface{})
				if !ok || len(options) != 2 {
					t.Fatalf("options = %#v, want two options; body=%#v", body["options"], body)
				}
				first := options[0].(map[string]interface{})
				if first["version"] != "8.3" || first["default"] != true || first["disabled"] != false {
					t.Fatalf("first option = %#v", first)
				}
			},
			response:   map[string]interface{}{"success": true},
			wantOutput: []string{"success", "true"},
		},
		{
			name:       "cron create",
			args:       []string{"service", "cron-schedule", "create", "11", "--name", "drush", "--title", "Drush cron", "--crontab", "*/5 * * * *", "--command", "drush cron", "--env-type", "RUNTIME"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/stack-services/11/cron-schedules",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				for key, want := range map[string]interface{}{"name": "drush", "title": "Drush cron", "crontab": "*/5 * * * *", "command": "drush cron", "envType": "RUNTIME"} {
					if body[key] != want {
						t.Fatalf("%s = %#v, want %#v; body=%#v", key, body[key], want, body)
					}
				}
			},
			response:   map[string]interface{}{"id": 51, "name": "drush", "title": "Drush cron", "crontab": "*/5 * * * *", "command": "drush cron", "envType": "RUNTIME"},
			wantOutput: []string{"Drush cron", "drush cron"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			var gotMethod string
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				if test.assertBody != nil {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("Decode() error = %v", err)
					}
					test.assertBody(t, body)
				}
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := newStackCommand()
			cmd.SetOut(&out)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if gotMethod != test.wantMethod {
				t.Fatalf("method = %q, want %s", gotMethod, test.wantMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			output := out.String()
			for _, expected := range test.wantOutput {
				if !strings.Contains(output, expected) {
					t.Fatalf("output should include %q: %s", expected, output)
				}
			}
		})
	}
}

func TestStackListShowsCreatedAndUpdatedDatesWithoutServices(t *testing.T) {
	var out bytes.Buffer
	createdAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	updatedAt := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stacks" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":                    1,
					"name":                  "drupal",
					"title":                 "Drupal",
					"status":                "active",
					"public":                true,
					"revId":                 11,
					"originStackRevNumber":  2,
					"originStackRevVersion": "1.2.3",
					"latestRevNumber":       3,
					"outdated":              true,
					"createdAt":             createdAt,
					"updatedAt":             updatedAt,
					"services": []map[string]interface{}{
						{"title": "PHP"},
					},
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"revision", "version", "outdated", "created at", "updated at", "2", "1.2.3", "2h ago", "30m ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"public", "rev id", "current rev number", "current version", "latest rev number", "services", "PHP", createdAt, updatedAt} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stack list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestStackListEnrichesMissingRevisionSummary(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/stacks":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":       1,
						"name":     "custom",
						"title":    "Custom",
						"status":   "active",
						"revId":    11,
						"settings": map[string]interface{}{},
					},
				},
			})
		case "/v1/stack-revisions/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      11,
				"number":  2,
				"version": "1.2.3",
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{"/v1/stacks?", "/v1/stack-revisions/11?"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	output := out.String()
	for _, expected := range []string{"revision", "version", "2", "1.2.3"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack list output should include enriched %q: %s", expected, output)
		}
	}
}

func TestStackGetShowsServicesAndDates(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stacks/1" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                    1,
			"name":                  "drupal",
			"title":                 "Drupal",
			"status":                "active",
			"public":                true,
			"revId":                 11,
			"originStackRevNumber":  2,
			"originStackRevVersion": "1.2.3",
			"latestRevNumber":       3,
			"outdated":              false,
			"createdAt":             "2026-01-02T03:04:05Z",
			"updatedAt":             "2026-01-03T04:05:06Z",
			"services": []map[string]interface{}{
				{"title": "PHP"},
				{"service": map[string]interface{}{"title": "Nginx"}, "disabled": true},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"current rev number:", "2", "version:", "1.2.3", "outdated:", "no", "created at:", "updated at:", "services:", "2026-01-02 03:04", "2026-01-03 04:05", "2 services (1 disabled)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "current version:") {
		t.Fatalf("stack get output should use version label: %s", output)
	}
}

func TestStackGetFetchesServicesSummaryWhenMissingFromStack(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/stacks/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    1,
				"name":  "drupal",
				"title": "Drupal",
				"revId": 11,
			})
		case "/v1/stack-revisions/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 11, "number": 2})
		case "/v1/stack-services":
			if got := r.URL.Query().Get("stackRevId"); got != "11" {
				t.Fatalf("stackRevId = %q, want 11", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 21, "title": "PHP", "disabled": false},
				{"id": 22, "title": "Nginx", "disabled": false},
				{"id": 23, "title": "Solr", "disabled": true},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newStackCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{"/v1/stacks/1?", "/v1/stack-revisions/11?", "/v1/stack-services?stackRevId=11"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	if output := out.String(); !strings.Contains(output, "3 services (1 disabled)") {
		t.Fatalf("stack get output should include services summary: %s", output)
	}
}

func TestDatabaseCommandExposesBasicOperations(t *testing.T) {
	database := newDatabaseCommand()
	names := make(map[string]bool)
	for _, cmd := range database.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "create", "update", "delete", "db", "user"} {
		if !names[name] {
			t.Fatalf("missing database subcommand %q", name)
		}
	}
}

func TestDatabaseListShowsAppServiceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/databases":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":           1,
					"name":         "postgres",
					"title":        "Postgres",
					"status":       "running",
					"type":         "postgres",
					"appServiceId": 22,
				},
			})
		case "/v1/app-services/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "Postgres Service"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"service", "Postgres Service"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("database list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appServiceId", "22"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("database list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestDatabaseNestedCommandsExposeBasicOperations(t *testing.T) {
	for _, test := range []struct {
		name     string
		cmd      *cobra.Command
		expected []string
	}{
		{name: "db", cmd: newDatabaseDbCommand(), expected: []string{"list", "get", "create", "delete"}},
		{name: "user", cmd: newDatabaseUserCommand(), expected: []string{"list", "get", "create", "dbs", "delete"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			names := make(map[string]bool)
			for _, cmd := range test.cmd.Commands() {
				names[cmd.Name()] = true
			}
			for _, name := range test.expected {
				if !names[name] {
					t.Fatalf("missing database %s subcommand %q", test.name, name)
				}
			}
		})
	}
}

func TestBuildCommandExposesSupportedOperations(t *testing.T) {
	build := newBuildCommand()
	names := make(map[string]bool)
	for _, cmd := range build.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "create", "deploy"} {
		if !names[name] {
			t.Fatalf("missing build subcommand %q", name)
		}
	}
	for _, name := range []string{"void", "registry-login"} {
		if names[name] {
			t.Fatalf("unexpected build subcommand %q", name)
		}
	}
}

func TestSchemaAddedCommandsUseRESTEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		cmd        func() *cobra.Command
		args       []string
		wantMethod string
		wantPath   string
		wantQuery  string
		assertBody func(*testing.T, map[string]interface{})
		response   interface{}
	}{
		{
			name:       "build create",
			cmd:        newBuildCommand,
			args:       []string{"create", "--service", "22", "--service", "33,44"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/app-builds",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if got := fmt.Sprint(body["appServiceIds"]); got != "[22 33 44]" {
					t.Fatalf("appServiceIds = %#v, want [22 33 44]", body["appServiceIds"])
				}
			},
			response: []map[string]interface{}{{"id": 101, "status": "created"}},
		},
		{
			name:       "instance delete force",
			cmd:        func() *cobra.Command { return newAppInstanceCommand("instance", "Manage app instances") },
			args:       []string{"delete", "21", "--force", "-y"},
			wantMethod: http.MethodDelete,
			wantPath:   "/v1/app-instances/21",
			wantQuery:  "force=true",
			response:   map[string]interface{}{"success": true},
		},
		{
			name:       "instance upgrade stack",
			cmd:        func() *cobra.Command { return newAppInstanceCommand("instance", "Manage app instances") },
			args:       []string{"upgrade-stack", "21", "--tokens=false"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/app-instances/21/actions/upgrade-stack",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["versions"] != true || body["tokens"] != false {
					t.Fatalf("upgrade body = %#v", body)
				}
			},
			response: map[string]interface{}{"success": true, "taskId": 55},
		},
		{
			name:       "stack update from git",
			cmd:        newStackCommand,
			args:       []string{"update-from-git", "7", "--git-ref", "main", "--git-ref-type", "branch"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/stacks/7/actions/update-from-git",
			assertBody: func(t *testing.T, body map[string]interface{}) {
				if body["gitRef"] != "main" || body["gitRefType"] != "branch" {
					t.Fatalf("update-from-git body = %#v", body)
				}
			},
			response: map[string]interface{}{"success": true},
		},
		{
			name:       "integration option branches",
			cmd:        newIntegrationCommand,
			args:       []string{"options", "remote-git-repo-branches", "9", "--remote-git-repo", "repo-1"},
			wantMethod: http.MethodGet,
			wantPath:   "/v1/integrations/9/options/remote-git-repo-branches",
			wantQuery:  "remoteGitRepoId=repo-1",
			response:   []interface{}{"main"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/tasks/55" {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":     55,
						"title":  "Upgrade stack",
						"status": "completed",
					})
					return
				}
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				if test.assertBody != nil {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("Decode() error = %v", err)
					}
					test.assertBody(t, body)
				}
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			defer server.Close()
			configureTestAPI(t, server.URL+"/v1")

			cmd := test.cmd()
			cmd.SetOut(io.Discard)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			if gotMethod != test.wantMethod {
				t.Fatalf("method = %q, want %s", gotMethod, test.wantMethod)
			}
			if gotPath != test.wantPath {
				t.Fatalf("path = %q, want %s", gotPath, test.wantPath)
			}
			if test.wantQuery != "" && gotQuery != test.wantQuery {
				t.Fatalf("query = %q, want %s", gotQuery, test.wantQuery)
			}
		})
	}
}

func TestLongRunningResourceColumnsIncludeTask(t *testing.T) {
	for name, columns := range map[string][]string{
		"build":      buildColumns,
		"deployment": deploymentColumns,
		"backup":     backupColumns,
		"import":     importColumns,
	} {
		found := false
		for _, column := range columns {
			if column == "task" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s columns should include task details", name)
		}
	}

	buildColumnList := strings.Join(buildColumns, ",")
	for _, expected := range []string{"services", "images", "commitMessage", "startedAt", "endedAt", "duration"} {
		if !strings.Contains(buildColumnList, expected) {
			t.Fatalf("buildColumns should include %q: %s", expected, buildColumnList)
		}
	}
	buildListColumnList := strings.Join(buildListColumns, ",")
	for _, expected := range []string{"imageCount", "startedAt", "duration"} {
		if !strings.Contains(buildListColumnList, expected) {
			t.Fatalf("buildListColumns should include %q: %s", expected, buildListColumnList)
		}
	}
	for _, unwanted := range []string{"images", "createdAt", "endedAt", "commitMessage"} {
		if strings.Contains(buildListColumnList, unwanted) {
			t.Fatalf("buildListColumns should not include %q: %s", unwanted, buildListColumnList)
		}
	}

	deploymentColumnList := strings.Join(deploymentColumns, ",")
	for _, expected := range []string{"services", "images", "rollbackStatus", "duration"} {
		if !strings.Contains(deploymentColumnList, expected) {
			t.Fatalf("deploymentColumns should include %q: %s", expected, deploymentColumnList)
		}
	}
	deploymentListColumnList := strings.Join(deploymentListColumns, ",")
	for _, expected := range []string{"services", "builds", "startedAt", "duration", "rollbackStatus"} {
		if !strings.Contains(deploymentListColumnList, expected) {
			t.Fatalf("deploymentListColumns should include %q: %s", expected, deploymentListColumnList)
		}
	}
	for _, unwanted := range []string{"images", "createdAt", "endedAt", "skipRollback"} {
		if strings.Contains(deploymentListColumnList, unwanted) {
			t.Fatalf("deploymentListColumns should not include %q: %s", unwanted, deploymentListColumnList)
		}
	}
}

func TestBuildListShowsServiceAndImageCounts(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/app-builds" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("appInstanceId"); got != "21" {
			t.Fatalf("appInstanceId = %q, want 21", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":        101,
					"number":    7,
					"status":    "completed",
					"gitRef":    "main",
					"startedAt": "2026-01-02T03:00:00Z",
					"endedAt":   "2026-01-02T03:02:00Z",
					"appServiceBuilds": []map[string]interface{}{
						{"appServiceId": 22, "image": "registry.example.com/app:php-7"},
						{"appServiceId": 33, "image": "registry.example.com/app:nginx-7"},
					},
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBuildCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "-i", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"services", "2 services", "images", "2 images", "started at", "duration", "2m", "completed"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("build list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"commit message", "created at", "ended at", "registry.example.com/app:php-7", "registry.example.com/app:nginx-7"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("build list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestBuildAndDeploymentColumnsShowServicesAndImages(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	row := map[string]interface{}{
		"id":     1,
		"number": 7,
		"status": "completed",
		"services": []interface{}{
			map[string]interface{}{
				"appService": map[string]interface{}{"title": "PHP"},
				"image":      "registry.example.com/app:php-7",
			},
			map[string]interface{}{
				"name":  "nginx",
				"image": "registry.example.com/app:nginx-7",
			},
		},
	}

	printTable(cmd, []interface{}{row}, buildColumns)
	output := out.String()
	for _, expected := range []string{"services", "images", "PHP", "nginx", "PHP=registry.example.com/app:php-7", "nginx=registry.example.com/app:nginx-7"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("build output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, `{"appService"`) {
		t.Fatalf("build output should not include raw service JSON: %s", output)
	}
}

func TestBuildGetShowsServicesAndImages(t *testing.T) {
	var out bytes.Buffer
	requests := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/v1/app-builds/101":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     101,
				"number": 7,
				"status": "completed",
				"task": map[string]interface{}{
					"id":    44,
					"title": "Build",
				},
				"appServiceBuilds": []map[string]interface{}{
					{
						"appServiceId": 22,
						"image":        "registry.example.com/app:php-7",
						"status":       "completed",
					},
					{
						"appServiceId": 33,
						"image":        "registry.example.com/app:nginx-7",
						"status":       "completed",
					},
				},
			})
		case "/v1/app-services/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "PHP"})
		case "/v1/app-services/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Nginx"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newBuildCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "101"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requests["/v1/app-builds/101"] != 1 {
		t.Fatalf("build get requests = %d, want 1", requests["/v1/app-builds/101"])
	}
	for _, path := range []string{"/v1/app-services/22", "/v1/app-services/33"} {
		if requests[path] != 1 {
			t.Fatalf("%s requests = %d, want 1", path, requests[path])
		}
	}
	output := out.String()
	for _, expected := range []string{"services:", "PHP, Nginx", "images:", "PHP=registry.example.com/app:php-7", "Nginx=registry.example.com/app:nginx-7", "task:", "Build"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("build get output should include %q: %s", expected, output)
		}
	}
}

func TestDeploymentGetShowsServicesAndImages(t *testing.T) {
	var out bytes.Buffer
	requests := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/v1/app-deployments/303":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             303,
				"number":         7,
				"status":         "completed",
				"rollbackStatus": "rolled_back",
				"appServiceDeployments": []map[string]interface{}{
					{
						"appServiceId":      22,
						"appServiceBuildId": 2201,
						"status":            "completed",
					},
					{
						"appServiceId":      33,
						"appServiceBuildId": 3301,
						"status":            "completed",
					},
				},
				"builds": []map[string]interface{}{
					{
						"id": 101,
						"appServiceBuilds": []map[string]interface{}{
							{
								"id":           2201,
								"appServiceId": 22,
								"image":        "registry.example.com/app:php-7",
							},
							{
								"id":           3301,
								"appServiceId": 33,
								"image":        "registry.example.com/app:nginx-7",
							},
						},
					},
				},
			})
		case "/v1/app-services/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "PHP"})
		case "/v1/app-services/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Nginx"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDeploymentCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "303"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requests["/v1/app-deployments/303"] != 1 {
		t.Fatalf("deployment get requests = %d, want 1", requests["/v1/app-deployments/303"])
	}
	for _, path := range []string{"/v1/app-services/22", "/v1/app-services/33"} {
		if requests[path] != 1 {
			t.Fatalf("%s requests = %d, want 1", path, requests[path])
		}
	}
	output := out.String()
	for _, expected := range []string{"services:", "PHP, Nginx", "images:", "PHP=registry.example.com/app:php-7", "Nginx=registry.example.com/app:nginx-7", "rollback status:", "rolled back"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("deployment get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, `{"appService"`) || strings.Contains(output, `"appServiceId"`) {
		t.Fatalf("deployment get output should not include raw service JSON: %s", output)
	}
}

func TestDeploymentListShowsServiceBuildCountsAndDurationWithoutImages(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/app-deployments" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("appInstanceId"); got != "21" {
			t.Fatalf("appInstanceId = %q, want 21", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":             303,
					"number":         7,
					"status":         "completed",
					"rollbackStatus": "not_attempted",
					"startedAt":      "2026-01-02T03:00:00Z",
					"endedAt":        "2026-01-02T03:02:00Z",
					"appServiceDeployments": []map[string]interface{}{
						{"appServiceId": 22, "appServiceBuildId": 2201},
						{"appServiceId": 33, "appServiceBuildId": 3301},
					},
					"builds": []map[string]interface{}{
						{
							"appServiceBuilds": []map[string]interface{}{
								{"id": 2201, "appServiceId": 22, "image": "registry.example.com/app:php-7"},
								{"id": 3301, "appServiceId": 33, "image": "registry.example.com/app:nginx-7"},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDeploymentCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "-i", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"services", "2 services", "builds", "1 build", "started at", "duration", "2m", "rollback status", "not attempted"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("deployment list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"images", "created at", "ended at", "registry.example.com/app:php-7", "registry.example.com/app:nginx-7"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("deployment list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestImportListUsesStartedAndDuration(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/imports" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("appInstanceId"); got != "21" {
			t.Fatalf("appInstanceId = %q, want 21", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":        55,
					"name":      "db-import",
					"source":    "url",
					"status":    "completed",
					"createdAt": "2026-01-02T02:00:00Z",
					"startedAt": "2026-01-02T03:00:00Z",
					"endedAt":   "2026-01-02T03:04:00Z",
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newImportCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "-i", "21"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"started at", "duration", "4m", "db-import", "url", "completed"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("import list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"created at", "ended at", "2026-01-02T02:00:00Z"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("import list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestDatabaseCreateUsesPublicAPIShape(t *testing.T) {
	var requestedMethod string
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedMethod = r.Method
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    101,
			"name":  "main",
			"title": "Main",
			"type":  "postgres",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--env", "3",
		"--integration-kind", "7",
		"--name", "main",
		"--title", "Main",
		"--type", "postgres",
		"--version", "16",
		"--machine-type", "db-s",
		"--org", "10",
		"--project", "12",
		"--region", "us",
		"--zone", "us-a",
		"--resided-cluster", "99",
		"--high-availability",
		"--storage-autoscaling",
		"--storage-size", "50",
		"--iops", "3000",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/databases" {
		t.Fatalf("path = %q, want /v1/databases", requestedPath)
	}
	if body["envId"] != float64(3) {
		t.Fatalf("envId = %#v, want 3", body["envId"])
	}
	if body["integrationKindId"] != float64(7) {
		t.Fatalf("integrationKindId = %#v, want 7", body["integrationKindId"])
	}
	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["projectId"] != float64(12) {
		t.Fatalf("projectId = %#v, want 12", body["projectId"])
	}
	if body["residedClusterId"] != float64(99) {
		t.Fatalf("residedClusterId = %#v, want 99", body["residedClusterId"])
	}
	if body["type"] != "postgres" {
		t.Fatalf("type = %#v, want postgres", body["type"])
	}
	if body["version"] != "16" {
		t.Fatalf("version = %#v, want 16", body["version"])
	}
	if body["machineType"] != "db-s" {
		t.Fatalf("machineType = %#v, want db-s", body["machineType"])
	}
	if _, ok := body["username"]; ok {
		t.Fatalf("body should not include username: %#v", body)
	}
	if body["highAvailability"] != true {
		t.Fatalf("highAvailability = %#v, want true", body["highAvailability"])
	}
	if body["storageAutoscaling"] != true {
		t.Fatalf("storageAutoscaling = %#v, want true", body["storageAutoscaling"])
	}
	if body["storageSize"] != float64(50) {
		t.Fatalf("storageSize = %#v, want 50", body["storageSize"])
	}
	if body["iops"] != float64(3000) {
		t.Fatalf("iops = %#v, want 3000", body["iops"])
	}
}

func TestDatabaseDbListUsesDatabaseFilter(t *testing.T) {
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/database-dbs":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 44, "name": "main", "databaseId": 33},
			})
		case "/v1/databases/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Postgres"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"db", "list", "33"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(requests) == 0 || requests[0] != "/v1/database-dbs?databaseId=33" {
		t.Fatalf("first request = %#v, want /v1/database-dbs?databaseId=33", requests)
	}
}

func TestDatabaseDbCreateUsesPublicAPIShape(t *testing.T) {
	var requests []struct {
		method string
		path   string
		body   map[string]interface{}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests = append(requests, struct {
			method string
			path   string
			body   map[string]interface{}
		}{method: r.Method, path: r.URL.Path, body: body})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 44, "name": "main"})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	createCmd := newDatabaseCommand()
	createCmd.SetOut(io.Discard)
	createCmd.SetArgs([]string{"db", "create", "33", "--name", "main", "--charset", "utf8mb4", "--collation", "utf8mb4_unicode_ci"})
	if err := createCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/v1/database-dbs" {
		t.Fatalf("create request = %s %s", requests[0].method, requests[0].path)
	}
	if requests[0].body["databaseId"] != float64(33) || requests[0].body["name"] != "main" || requests[0].body["charset"] != "utf8mb4" || requests[0].body["collation"] != "utf8mb4_unicode_ci" {
		t.Fatalf("create body = %#v", requests[0].body)
	}
	if _, ok := requests[0].body["title"]; ok {
		t.Fatalf("create body should not include title: %#v", requests[0].body)
	}
}

func TestDatabaseUserListUsesDatabaseFilter(t *testing.T) {
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/database-users":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 55, "name": "editor", "databaseId": 33},
			})
		case "/v1/databases/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Postgres"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"user", "list", "33"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(requests) == 0 || requests[0] != "/v1/database-users?databaseId=33" {
		t.Fatalf("first request = %#v, want /v1/database-users?databaseId=33", requests)
	}
}

func TestDatabaseUserGetSelectsUserFromDatabaseList(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/database-users":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 54, "name": "reader", "databaseId": 33},
					{
						"id":         55,
						"name":       "editor",
						"hostname":   "%",
						"status":     "active",
						"databaseId": 33,
						"databaseDbs": []map[string]interface{}{
							{"id": 44, "name": "main"},
						},
					},
				},
			})
		case "/v1/databases/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Postgres"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"user", "get", "33", "55"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(requests) == 0 || requests[0] != "/v1/database-users?databaseId=33" {
		t.Fatalf("first request = %#v, want /v1/database-users?databaseId=33", requests)
	}

	output := out.String()
	for _, expected := range []string{"id:", "55", "username:", "editor", "hostname:", "%", "status:", "active", "database:", "Postgres", "database id:", "33", "dbs:", "main"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("database user get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"reader", "databaseId:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("database user get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestDatabaseUserGetReturnsNotFoundForMissingUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/database-users":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 54, "name": "reader", "databaseId": 33},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"user", "get", "33", "55"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing database user error")
	}
	if !strings.Contains(err.Error(), `database user "55" not found in database "33"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDatabaseUserListShowsUsernameFromNameFallback(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/database-users":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 55, "name": "editor", "databaseId": 33},
			})
		case "/v1/databases/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Postgres"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"user", "list", "33"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"username", "editor"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("database user output should include %q: %s", expected, output)
		}
	}
}

func TestDatabaseUserCreateAndGrantDBsUsePublicAPIShape(t *testing.T) {
	var requests []struct {
		method string
		path   string
		body   map[string]interface{}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests = append(requests, struct {
			method string
			path   string
			body   map[string]interface{}
		}{method: r.Method, path: r.URL.Path, body: body})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 55, "name": "editor"})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	createCmd := newDatabaseCommand()
	createCmd.SetOut(io.Discard)
	createCmd.SetArgs([]string{"user", "create", "33", "--username", "editor", "--password", "secret", "--hostname", "%", "--database-db", "44", "--database-db", "45,46"})
	if err := createCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	dbsCmd := newDatabaseCommand()
	dbsCmd.SetOut(io.Discard)
	dbsCmd.SetArgs([]string{"user", "dbs", "55", "--database-db", "44,45"})
	if err := dbsCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/v1/database-users" {
		t.Fatalf("create request = %s %s", requests[0].method, requests[0].path)
	}
	if requests[0].body["databaseId"] != float64(33) || requests[0].body["name"] != "editor" || requests[0].body["password"] != "secret" || requests[0].body["hostname"] != "%" {
		t.Fatalf("create body = %#v", requests[0].body)
	}
	if got := requests[0].body["databaseDbIds"]; fmt.Sprint(got) != "[44 45 46]" {
		t.Fatalf("databaseDbIds = %#v, want [44 45 46]", got)
	}
	if _, ok := requests[0].body["username"]; ok {
		t.Fatalf("create body should use name, not username: %#v", requests[0].body)
	}
	if requests[1].method != http.MethodPut || requests[1].path != "/v1/database-users/55/dbs" {
		t.Fatalf("grant request = %s %s", requests[1].method, requests[1].path)
	}
	if got := requests[1].body["databaseDbIds"]; fmt.Sprint(got) != "[44 45]" {
		t.Fatalf("databaseDbIds = %#v, want [44 45]", got)
	}
}

func TestClusterCreateUsesPublicAPIShape(t *testing.T) {
	var requestedMethod string
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" {
			if got := r.URL.Query().Get("clusterId"); got != "101" {
				t.Fatalf("task clusterId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "10" {
				t.Fatalf("task pageSize = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		requestedMethod = r.Method
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":            101,
			"name":          "prod",
			"title":         "Production",
			"integrationId": 7,
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--integration", "7",
		"--name", "prod",
		"--title", "Production",
		"--org", "10",
		"--project", "12",
		"--serverless",
		"--disable-monitoring",
		"--single-node",
		"--region", "us",
		"--min-node-count", "1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/clusters" {
		t.Fatalf("path = %q, want /v1/clusters", requestedPath)
	}
	if body["integrationId"] != float64(7) {
		t.Fatalf("integrationId = %#v, want 7", body["integrationId"])
	}
	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["projectId"] != float64(12) {
		t.Fatalf("projectId = %#v, want 12", body["projectId"])
	}
	if body["serverless"] != true {
		t.Fatalf("serverless = %#v, want true", body["serverless"])
	}
	if body["singleNode"] != true {
		t.Fatalf("singleNode = %#v, want true", body["singleNode"])
	}
	if body["minNodeCount"] != float64(1) {
		t.Fatalf("minNodeCount = %#v, want 1", body["minNodeCount"])
	}
}

func TestClusterListDoesNotFailWhenIntegrationEnrichmentFails(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":            101,
					"name":          "prod",
					"title":         "Production",
					"status":        "running",
					"integrationId": 7,
				},
			})
		case "/v1/integrations/7":
			http.NotFound(w, r)
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"id", "name", "title", "integration", "101", "prod", "Production"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "integrationId") {
		t.Fatalf("output should not include integrationId: %s", output)
	}
}

func TestClusterListEnrichesIntegrationTitleWithProviderTitle(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":            101,
						"name":          "prod",
						"title":         "Production",
						"status":        "running",
						"integrationId": 7,
					},
				},
			})
		case "/v1/integrations/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"item": map[string]interface{}{
					"id":            7,
					"title":         "Production AWS",
					"providerRevId": 9,
				},
			})
		case "/v1/providers":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":      3,
						"title":   "Amazon Web Services",
						"version": "2.1.0",
						"revId":   9,
					},
				},
			})
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "Production AWS (Amazon Web Services)") {
		t.Fatalf("output should include integration and provider titles: %s", output)
	}
	if strings.Contains(output, "integrationId") || strings.Contains(output, "providerRevId") {
		t.Fatalf("output should not include raw integration/provider ID columns: %s", output)
	}
}

func TestClusterListShowsRealNodeMetrics(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":               101,
					"name":             "prod",
					"title":            "Production",
					"status":           "running",
					"currentNodeCount": 2,
					"maxNodeCount":     5,
				},
				{
					"id":     102,
					"name":   "stage",
					"title":  "Staging",
					"status": "running",
				},
			})
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {3, 4}, 102: {0, 2}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"nodes", "3 / 4", "0 / 2"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster list output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "2 / 5") {
		t.Fatalf("cluster list output should use real metrics instead of REST fallback counts: %s", output)
	}
}

func TestClusterCommandExposesAppSubcommand(t *testing.T) {
	cluster := newClusterCommand()
	names := make(map[string]bool)
	for _, cmd := range cluster.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "app", "create", "update", "delete"} {
		if !names[name] {
			t.Fatalf("missing cluster subcommand %q", name)
		}
	}
}

func TestClusterGetShowsVersionInfraIPsAndNodes(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters/101":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":                    101,
				"name":                  "prod",
				"title":                 "Production",
				"status":                "running",
				"integration":           map[string]interface{}{"title": "AWS"},
				"region":                "us-east",
				"zone":                  "a",
				"version":               "1.31",
				"infrastructureVersion": "2026.06",
				"ips":                   []string{"203.0.113.10"},
				"currentNodeCount":      2,
				"maxNodeCount":          5,
				"singleNode":            false,
				"serverless":            false,
			})
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {3, 4}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "101"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"kubernetes version:", "1.31", "infra version:", "2026.06", "ips:", "203.0.113.10", "nodes:", "3 / 4", "single node:", "false"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"instances:", "scalable:", "serverless:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cluster get output should not include %q: %s", unwanted, output)
		}
	}
	if strings.Contains(output, "version:") && !strings.Contains(output, "kubernetes version:") {
		t.Fatalf("cluster get output should use kubernetes version title: %s", output)
	}
}

func TestClusterOutputHidesRegionWhenMissing(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 101, "name": "prod", "title": "Production", "status": "running", "zone": "a"},
			})
		case "/v1/clusters/101":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 101, "name": "prod", "title": "Production", "status": "running", "zone": "a"})
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	listCmd := newClusterCommand()
	listCmd.SetOut(&out)
	listCmd.SetArgs([]string{"list", "--org", "123"})
	if err := listCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	listOutput := out.String()
	if strings.Contains(listOutput, "region") {
		t.Fatalf("cluster list output should hide empty region column: %s", listOutput)
	}
	if !strings.Contains(listOutput, "zone") || !strings.Contains(listOutput, "a") {
		t.Fatalf("cluster list output should keep zone column: %s", listOutput)
	}

	out.Reset()
	getCmd := newClusterCommand()
	getCmd.SetOut(&out)
	getCmd.SetArgs([]string{"get", "101"})
	if err := getCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	getOutput := out.String()
	if strings.Contains(getOutput, "region:") {
		t.Fatalf("cluster get output should hide empty region row: %s", getOutput)
	}
	if !strings.Contains(getOutput, "zone:") || !strings.Contains(getOutput, "a") {
		t.Fatalf("cluster get output should keep zone row: %s", getOutput)
	}
}

func TestClusterOutputKeepsRegionWhenPresent(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/query" {
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}, 102: {2, 2}})
			return
		}
		if r.URL.Path != "/v1/clusters" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 101, "name": "prod", "title": "Production", "status": "running", "region": "us-east", "zone": "a"},
			{"id": 102, "name": "dev", "title": "Development", "status": "running", "zone": "b"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "region") || !strings.Contains(output, "us-east") {
		t.Fatalf("cluster list output should keep region column when any row has region: %s", output)
	}
}

func TestClusterAppListShowsInstanceIDAndStackFromInstance(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps":
			if got := r.URL.Query().Get("clusterId"); got != "101" {
				t.Fatalf("clusterId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("clusterApp"); got != "true" {
				t.Fatalf("clusterApp = %q, want true", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":     11,
					"name":   "varnish",
					"title":  "Varnish",
					"status": "running",
				},
				{
					"id":     12,
					"name":   "solr",
					"title":  "Solr",
					"status": "running",
				},
			})
		case "/v1/app-instances":
			if got := r.URL.Query().Get("clusterId"); got != "101" {
				t.Fatalf("clusterId = %q, want 101", got)
			}
			if got := r.URL.Query().Get("clusterApp"); got != "true" {
				t.Fatalf("clusterApp = %q, want true", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":      21,
					"appId":   11,
					"status":  "running",
					"stackId": 7,
				},
			})
		case "/v1/stacks/7":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7, "title": "Varnish Stack"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"app", "list", "101"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"id", "21", "Varnish", "Varnish Stack"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster infra app output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"instance id", "appId", "instances", "appInstances", " 11 ", "Solr"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cluster infra app output should not include %q: %s", unwanted, output)
		}
	}
}

func TestClusterListShowsSpecialIntegrationLabelsWithoutEnrichment(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/query" {
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}, 102: {1, 1}, 103: {1, 1}})
			return
		}
		if r.URL.Path != "/v1/clusters" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":            101,
					"name":          "cloud",
					"title":         "Cloud",
					"status":        "running",
					"wodby":         true,
					"integrationId": 701,
				},
				{
					"id":            102,
					"name":          "local",
					"title":         "Local",
					"status":        "running",
					"serverless":    false,
					"type":          "k3s",
					"integrationId": 802,
				},
				{
					"id":            103,
					"name":          "sample",
					"title":         "Sample",
					"status":        "running",
					"serverless":    false,
					"kind":          "demo",
					"integrationId": 903,
				},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"Wodby Cloud", "k3s", "Demo"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"integrationId", "701", "802", "903"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cluster list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestClusterGetOmitsSpecialIntegrationIDRows(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/clusters/101":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":            101,
				"name":          "cloud",
				"title":         "Cloud",
				"status":        "running",
				"wodby":         true,
				"integrationId": 701,
			})
		case "/v1/app-instances":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
		case "/query":
			writeClusterMetricsResponse(t, w, r, map[int][2]int{101: {1, 1}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "101"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "integration:") || !strings.Contains(output, "Wodby Cloud") {
		t.Fatalf("cluster get output should include Wodby Cloud integration: %s", output)
	}
	for _, unwanted := range []string{"integration id:", "701"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cluster get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestProviderListSendsSupportedFilters(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{"id": 1, "name": "aws", "title": "AWS"},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newProviderCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"list",
		"--org", "10",
		"--project", "20,21",
		"--search", "aws",
		"--page", "2",
		"--page-size", "50",
		"--exclude-public",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/providers" {
		t.Fatalf("path = %q, want /v1/providers", requestedPath)
	}
	want := "excludePublic=true&orgId=10&page=2&pageSize=50&projectIds=20%2C21&search=aws"
	if requestedQuery != want {
		t.Fatalf("query = %q, want %q", requestedQuery, want)
	}
}

func TestProviderListShowsVersionRevisionSummary(t *testing.T) {
	var out bytes.Buffer
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/v1/providers":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":     1,
						"name":   "aws",
						"title":  "AWS",
						"status": "ok",
						"public": true,
						"revId":  11,
					},
				},
			})
		case "/v1/provider-revisions/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      11,
				"number":  4,
				"version": "1.2.3",
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newProviderCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	wantRequests := []string{"/v1/providers?orgId=123", "/v1/provider-revisions/11?"}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	output := out.String()
	for _, expected := range []string{"version", "1.2.3 (#4)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("provider list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"public", "rev id", " true ", " 11 "} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("provider list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestIntegrationListEnrichesProviderFromRevisionID(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/integrations":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":            1,
					"name":          "aws-prod",
					"title":         "AWS Prod",
					"scope":         "project",
					"status":        "ready",
					"providerRevId": 7,
				},
			})
		case "/v1/providers":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":      3,
						"title":   "Amazon Web Services",
						"version": "2.1.0",
						"revId":   7,
					},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newIntegrationCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	if !strings.Contains(output, "Amazon Web Services (2.1.0 #7)") {
		t.Fatalf("output should include provider title, version, and revision: %s", output)
	}
	if strings.Contains(output, "providerRevId") || strings.Contains(output, "providerId") {
		t.Fatalf("output should not include provider ID columns: %s", output)
	}
}

func TestInstanceListExcludesClusterAppsByDefault(t *testing.T) {
	query := executeInstanceListQuery(t, "list", "--org", "123")

	if got := query.Get("clusterApp"); got != "false" {
		t.Fatalf("clusterApp = %q, want false", got)
	}
}

func TestInstanceListCanFilterClusterApps(t *testing.T) {
	query := executeInstanceListQuery(t, "list", "--org", "123", "--cluster-app")

	if got := query.Get("clusterApp"); got != "true" {
		t.Fatalf("clusterApp = %q, want true", got)
	}
}

func TestInstanceListEnrichesReadableRelations(t *testing.T) {
	var out bytes.Buffer
	deployedAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-instances":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":         1,
					"name":       "prod",
					"title":      "Production",
					"status":     "running",
					"outdated":   true,
					"appId":      11,
					"envId":      22,
					"clusterId":  33,
					"mainDomain": "example.com",
				},
			})
		case "/v1/apps/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    11,
				"title": "Drupal",
				"stackRev": map[string]interface{}{
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Drupal Stack",
					},
				},
			})
		case "/v1/envs/22":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 22, "title": "Prod"})
		case "/v1/clusters/33":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 33, "title": "Primary"})
		case "/v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "1" {
				t.Fatalf("deployment appInstanceId = %q, want 1", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"items": []map[string]interface{}{
					{"id": 71, "number": 4, "status": "completed", "startedAt": "2026-01-02T03:04:00Z", "endedAt": deployedAt, "createdAt": "2026-01-02T03:03:00Z"},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"outdated", "app", "stack", "env", "cluster", "domain", "last deployed at", "Drupal", "Drupal Stack", "Prod", "Primary", "example.com", "2h ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appId", "stackId", "envId", "clusterId", "mainDomain", "11", "7", "22", "33"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output should not include %q: %s", unwanted, output)
		}
	}
}

func TestInstanceListUsesStackFromInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-instances":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":         1,
					"name":       "prod",
					"title":      "Production",
					"status":     "running",
					"mainDomain": "example.com",
					"app": map[string]interface{}{
						"id":    11,
						"title": "Drupal",
					},
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Drupal Stack",
					},
					"env": map[string]interface{}{
						"id":    22,
						"title": "Prod",
					},
					"cluster": map[string]interface{}{
						"id":    33,
						"title": "Primary",
					},
				},
			})
		case "/v1/app-deployments":
			if got := r.URL.Query().Get("appInstanceId"); got != "1" {
				t.Fatalf("deployment appInstanceId = %q, want 1", got)
			}
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Fatalf("deployment pageSize = %q, want 1", got)
			}
			encodeEmptyItems(w)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--org", "123"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"app", "stack", "env", "cluster", "domain", "Drupal", "Drupal Stack", "Prod", "Primary", "example.com"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("instance list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appId", "stackId", "envId", "clusterId", "mainDomain", "7", "22", "33"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("instance list output should not include %q: %s", unwanted, output)
		}
	}
}

func TestInstanceGetEnrichesStackThroughAppRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-instances/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     1,
				"name":   "prod",
				"title":  "Production",
				"status": "running",
				"appId":  11,
			})
		case "/v1/apps/11":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    11,
				"title": "Drupal",
				"stackRev": map[string]interface{}{
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Drupal Stack",
					},
				},
			})
		case "/v1/app-services", "/v1/app-routes", "/v1/app-ports":
			encodeEmptyList(w)
		case "/v1/app-builds", "/v1/app-deployments":
			encodeEmptyItems(w)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"app:", "Drupal", "app id:", "11", "stack:", "Drupal Stack", "stack id:", "7", "service status:", "no services", "route status:", "no routes", "port status:", "no ports"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("instance get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appId:", "stackId:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("instance get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestInstanceGetUsesStackFromInstanceRelation(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-instances/1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     1,
				"name":   "prod",
				"title":  "Production",
				"status": "running",
				"app": map[string]interface{}{
					"id":    11,
					"title": "Drupal",
				},
				"stack": map[string]interface{}{
					"id":    7,
					"title": "Drupal Stack",
				},
			})
		case "/v1/app-services", "/v1/app-routes", "/v1/app-ports":
			encodeEmptyList(w)
		case "/v1/app-builds", "/v1/app-deployments":
			encodeEmptyItems(w)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"app:", "Drupal", "app id:", "11", "stack:", "Drupal Stack", "stack id:", "7", "service status:", "no services", "route status:", "no routes", "port status:", "no ports"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("instance get output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appId:", "stackId:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("instance get output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTaskLogsShowStepNamesDurationsAndNoLogs(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 42,
				"jobs": []map[string]interface{}{
					{
						"steps": []map[string]interface{}{
							{
								"id":        "step-1",
								"name":      "Prepare",
								"status":    "running",
								"startedAt": "2026-01-02T03:00:00Z",
								"endedAt":   "2026-01-02T03:01:30Z",
							},
							{
								"id":        "step-2",
								"title":     "Deploy",
								"status":    "failed",
								"startedAt": "2026-01-02T03:02:00Z",
								"endedAt":   "2026-01-02T03:04:00Z",
							},
						},
					},
				},
			})
		case "/v1/task-steps/step-1/logs":
			if got := r.URL.Query().Get("delivery"); got != "auto" {
				t.Fatalf("delivery = %q, want auto", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "pending",
				"items": []map[string]interface{}{
					{"level": "info", "message": "ready"},
				},
			})
		case "/v1/task-steps/step-2/logs":
			if got := r.URL.Query().Get("delivery"); got != "auto" {
				t.Fatalf("delivery = %q, want auto", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "empty",
				"lines":  []map[string]interface{}{},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"logs", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"== Prepare (running, 1m 30s) ==", "[info] ready", "== Deploy (failed, 2m) ==", "no logs"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task logs output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"== step step-1 ==", "== step step-2 =="} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("task logs output should not include %q: %s", unwanted, output)
		}
	}
}

func TestTaskLogsWithMultipleJobsShowsSummaryWithoutFetchingLogs(t *testing.T) {
	var out bytes.Buffer
	var logRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 42,
				"jobs": []map[string]interface{}{
					{
						"id":        "job-1",
						"title":     "Build",
						"status":    "done",
						"startedAt": "2026-01-02T03:00:00Z",
						"endedAt":   "2026-01-02T03:02:00Z",
						"steps": []map[string]interface{}{
							{"id": "step-1", "name": "Prepare"},
						},
					},
					{
						"id":        "job-2",
						"title":     "Deploy",
						"status":    "pending",
						"startedAt": "2026-01-02T03:03:00Z",
						"endedAt":   "2026-01-02T03:05:00Z",
						"steps": []map[string]interface{}{
							{"id": "step-2", "name": "Apply"},
						},
					},
				},
			})
		case "/v1/task-steps/step-1/logs", "/v1/task-steps/step-2/logs":
			logRequests++
			t.Fatalf("logs should not be fetched without --job or --all-jobs")
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"logs", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"task has 2 jobs; pass --job to show logs, or --all-jobs to show everything", "id", "name", "status", "duration", "steps", "job-1", "Build", "done", "2m", "job-2", "Deploy", "pending"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task logs summary should include %q: %s", expected, output)
		}
	}
	if logRequests != 0 {
		t.Fatalf("logRequests = %d, want 0", logRequests)
	}
}

func TestTaskLogsWithJobFilterFetchesSelectedJobOnly(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 42,
				"jobs": []map[string]interface{}{
					{
						"id":        "job-1",
						"title":     "Build",
						"startedAt": "2026-01-02T03:00:00Z",
						"endedAt":   "2026-01-02T03:02:00Z",
						"steps": []map[string]interface{}{
							{"id": "step-1", "name": "Prepare"},
						},
					},
					{
						"id":        "job-2",
						"title":     "Deploy",
						"status":    "running",
						"startedAt": "2026-01-02T03:03:00Z",
						"endedAt":   "2026-01-02T03:05:00Z",
						"steps": []map[string]interface{}{
							{
								"id":        "step-2",
								"name":      "Apply",
								"status":    "done",
								"startedAt": "2026-01-02T03:03:00Z",
								"endedAt":   "2026-01-02T03:03:30Z",
							},
						},
					},
				},
			})
		case "/v1/task-steps/step-2/logs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lines": []map[string]interface{}{
					{"message": "applied"},
				},
			})
		case "/v1/task-steps/step-1/logs":
			t.Fatalf("unselected job logs should not be fetched")
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"logs", "42", "--job", "Deploy"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"== job Deploy [job-2] (running, 2m) ==", "== Apply (done, 30s) ==", "applied"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("task logs output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "Prepare") || strings.Contains(output, "Build") {
		t.Fatalf("task logs output should not include unselected job details: %s", output)
	}
}

func TestTaskLogsWithoutStepsPrintsNoLogs(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/42" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   42,
			"jobs": []map[string]interface{}{},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newTaskCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"logs", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if output := strings.TrimSpace(out.String()); output != "no logs" {
		t.Fatalf("task logs output = %q, want no logs", output)
	}
}

func executeInstanceListQuery(t *testing.T, args ...string) url.Values {
	t.Helper()

	var requestedPath string
	var requestedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/app-deployments" {
			encodeEmptyItems(w)
			return
		}
		requestedPath = r.URL.Path
		requestedQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 1, "name": "demo"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(io.Discard)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-instances" {
		t.Fatalf("path = %q, want /v1/app-instances", requestedPath)
	}
	return requestedQuery
}

func encodeEmptyList(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
}

func encodeEmptyItems(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": []map[string]interface{}{}})
}

func captureProcessOutput(t *testing.T, run func()) (string, string) {
	t.Helper()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	run()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	return string(stdout), string(stderr)
}

func configureTestAPI(t *testing.T, endpoint string) {
	t.Helper()
	viper.Set("api_key", "secret")
	viper.Set("access_token", "")
	viper.Set("api_endpoint", "")
	viper.Set("api_base_url", endpoint)
	t.Cleanup(func() {
		viper.Set("api_key", "")
		viper.Set("access_token", "")
		viper.Set("api_endpoint", "")
		viper.Set("api_base_url", "")
	})
}

func writeClusterMetricsResponse(t *testing.T, w http.ResponseWriter, r *http.Request, metrics map[int][2]int) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Fatalf("cluster metrics method = %q, want POST", r.Method)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	variables, _ := body["variables"].(map[string]interface{})
	rawIDs, _ := variables["ids"].([]interface{})

	items := make([]map[string]interface{}, 0, len(rawIDs))
	for _, rawID := range rawIDs {
		id, err := strconv.Atoi(fmt.Sprint(rawID))
		if err != nil {
			t.Fatalf("cluster metrics id = %#v: %v", rawID, err)
		}
		counts, ok := metrics[id]
		if !ok {
			continue
		}
		items = append(items, map[string]interface{}{
			"id":         id,
			"nodesReady": counts[0],
			"nodesTotal": counts[1],
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{"clustersMetrics": items},
	})
}
