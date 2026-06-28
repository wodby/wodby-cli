package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		"org",
		"member",
		"project",
		"env",
		"database",
		"cluster",
		"integration",
		"provider",
		"stack",
		"service",
		"app",
		"instance",
		"aps",
		"route",
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
		"build":        buildColumns,
		"deployment":   deploymentColumns,
		"backup":       backupColumns,
		"import":       importColumns,
		"task":         taskColumns,
		"operation":    operationColumns,
	} {
		for _, column := range columns {
			switch column {
			case "envId", "appId", "stackId", "clusterId", "mainDomain", "appServiceId", "portId", "appInstanceId", "databaseId", "databaseDbId", "taskId", "authorId":
				t.Fatalf("%s columns should use readable relation names, got %q", name, column)
			}
		}
	}
}

func TestDefaultTaskColumnsUseCompactListShape(t *testing.T) {
	got := strings.Join(taskColumns, ",")
	want := "id,title,status,author,createdAt,duration"
	if got != want {
		t.Fatalf("taskColumns = %q, want %q", got, want)
	}

	getColumns := strings.Join(taskGetColumns, ",")
	for _, expected := range []string{"author", "app", "instance", "service", "database", "databaseDb"} {
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
	for _, expected := range []string{"createdAt", "updatedAt"} {
		if !strings.Contains(listColumns, expected) {
			t.Fatalf("stackColumns should include %q: %s", expected, listColumns)
		}
	}
	if strings.Contains(listColumns, "services") {
		t.Fatalf("stackColumns should not include services on list: %s", listColumns)
	}

	getColumns := strings.Join(stackGetColumns, ",")
	for _, expected := range []string{"createdAt", "updatedAt", "services"} {
		if !strings.Contains(getColumns, expected) {
			t.Fatalf("stackGetColumns should include %q: %s", expected, getColumns)
		}
	}
}

func TestClusterGetColumnsShowAdditionalDetails(t *testing.T) {
	listColumns := strings.Join(clusterColumns, ",")
	for _, unwanted := range []string{"kubernetesVersion", "infraVersion", "publicIp"} {
		if strings.Contains(listColumns, unwanted) {
			t.Fatalf("clusterColumns should not include %q on list: %s", unwanted, listColumns)
		}
	}
	for _, expected := range []string{"nodes", "singleNode", "scalable"} {
		if !strings.Contains(listColumns, expected) {
			t.Fatalf("clusterColumns should include %q: %s", expected, listColumns)
		}
	}

	getColumns := strings.Join(clusterGetColumns, ",")
	for _, expected := range []string{"kubernetesVersion", "infraVersion", "ips", "nodes", "singleNode", "scalable"} {
		if !strings.Contains(getColumns, expected) {
			t.Fatalf("clusterGetColumns should include %q: %s", expected, getColumns)
		}
	}
	if strings.Contains(getColumns, "instances") {
		t.Fatalf("clusterGetColumns should not include instances: %s", getColumns)
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
				"id":     8,
				"role":   "owner",
				"status": "active",
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
	for _, expected := range []string{"member", "email", "role", "status", "Jane Doe <jane@example.com>", "jane@example.com", "owner", "active"} {
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
	} {
		if got := tableColumnTitle(column); got != expected {
			t.Fatalf("tableColumnTitle(%q) = %q, want %q", column, got, expected)
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
			want: "false",
		},
		{
			name: "stack flag",
			row:  map[string]interface{}{"stackOutdated": true},
			want: "true",
		},
		{
			name: "revision behind",
			row: map[string]interface{}{
				"stackRev": map[string]interface{}{"number": 2},
				"stack":    map[string]interface{}{"latestRevNumber": 3},
			},
			want: "true",
		},
		{
			name: "revision current",
			row: map[string]interface{}{
				"stackRevision": map[string]interface{}{"revNumber": 3},
				"stack":         map[string]interface{}{"latestRevNumber": 3},
			},
			want: "false",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatColumnValue(test.row, "outdated"); got != test.want {
				t.Fatalf("formatColumnValue(outdated) = %q, want %q", got, test.want)
			}
		})
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

func TestTaskColumnsShowDurationInsteadOfStartAndEndTimes(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	createdAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":          1,
			"name":        "worker-task",
			"title":       "Deploy",
			"status":      "done",
			"progress":    87,
			"createdAt":   createdAt,
			"startedAt":   "2026-01-02T03:04:05Z",
			"endedAt":     "2026-01-02T04:06:07Z",
			"appInstance": map[string]interface{}{"title": "Prod"},
			"app":         map[string]interface{}{"title": "Drupal"},
		},
	}, taskColumns)

	output := out.String()
	for _, expected := range []string{"id", "title", "status", "created at", "duration", "2h ago", "1h 2m"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"name", "worker-task", "progress", "app", "Drupal", "instance", "Prod", "started at", "ended at", createdAt, "2026-01-02T03:04:05Z", "2026-01-02T04:06:07Z"} {
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
				"id":        42,
				"title":     "Deploy",
				"status":    "done",
				"authorId":  8,
				"createdAt": "2026-01-02T03:00:00Z",
				"startedAt": "2026-01-02T03:04:00Z",
				"endedAt":   "2026-01-02T03:06:00Z",
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
	for _, expected := range []string{"author:", "Jane Doe <jane@example.com>", "author id:", "8"} {
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
					"startedAt": "2026-01-02T03:00:00Z",
					"endedAt":   "2026-01-02T03:02:00Z",
					"steps": []map[string]interface{}{
						{"id": "step-1", "name": "Prepare", "status": "done"},
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
							"status":    "running",
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
		{name: "job list", args: []string{"job", "list", "42"}, expected: []string{"id", "name", "status", "duration", "steps", "job-1", "Build", "done", "2m", "job-2", "Deploy", "running"}},
		{name: "job get", args: []string{"job", "get", "42", "Deploy"}, expected: []string{"id:", "job-2", "name:", "Deploy", "status:", "running", "duration:", "2m", "steps:", "1"}},
		{name: "step list", args: []string{"step", "list", "42"}, expected: []string{"id", "name", "status", "duration", "job", "step-1", "Prepare", "Build [job-1]", "step-2", "Apply", "30s", "Deploy [job-2]"}},
		{name: "step get", args: []string{"step", "get", "42", "step-2"}, expected: []string{"id:", "step-2", "name:", "Apply", "status:", "running", "duration:", "30s", "job:", "Deploy [job-2]"}},
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
	for _, name := range []string{"service", "route"} {
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

	for _, name := range []string{"list", "get", "status", "create", "service", "route", "build", "deployment", "backup", "import"} {
		if !names[name] {
			t.Fatalf("missing instance subcommand %q", name)
		}
	}
}

func TestAppCreateUsesPublicAPIShape(t *testing.T) {
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
		"--stack", "7",
		"--stack-rev", "70",
		"--name", "drupal",
		"--title", "Drupal",
		"--cluster-app",
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
	if body["stackId"] != float64(7) {
		t.Fatalf("stackId = %#v, want 7", body["stackId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["name"] != "drupal" || body["title"] != "Drupal" {
		t.Fatalf("name/title body = %#v", body)
	}
	if body["clusterApp"] != true {
		t.Fatalf("clusterApp = %#v, want true", body["clusterApp"])
	}
}

func TestAppInstanceCreateUsesPublicAPIShape(t *testing.T) {
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
		"--name", "prod",
		"--title", "Production",
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
	if body["stackId"] != float64(7) {
		t.Fatalf("stackId = %#v, want 7", body["stackId"])
	}
	if body["stackRevId"] != float64(70) {
		t.Fatalf("stackRevId = %#v, want 70", body["stackRevId"])
	}
	if body["name"] != "prod" || body["title"] != "Production" {
		t.Fatalf("name/title body = %#v", body)
	}
	if body["mainDomain"] != "example.com" {
		t.Fatalf("mainDomain = %#v, want example.com", body["mainDomain"])
	}
	if body["region"] != "us" || body["zone"] != "us-a" {
		t.Fatalf("region/zone body = %#v", body)
	}
	if body["clusterApp"] != true {
		t.Fatalf("clusterApp = %#v, want true", body["clusterApp"])
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
	for _, expected := range []string{"stack", "Drupal Stack"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("app list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"stackId", "7"} {
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
	if !strings.Contains(output, "Drupal Stack") {
		t.Fatalf("app list output should include stack from stack revision: %s", output)
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
	for _, expected := range []string{"stack", "Drupal Stack"} {
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
	for _, expected := range []string{"stack", "Drupal Stack"} {
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

func TestRouteListEnrichesPortNumber(t *testing.T) {
	var out bytes.Buffer

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
					"path":     "/",
					"pathType": "prefix",
					"action":   "proxy",
					"status":   "active",
					"appService": map[string]interface{}{
						"title": "Nginx",
					},
					"portId": 55,
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
	for _, expected := range []string{"port", "8080"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("route list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"portId", "55"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("route list output should not include %q: %s", unwanted, output)
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
					"id":              1,
					"name":            "drupal",
					"title":           "Drupal",
					"status":          "active",
					"public":          true,
					"revId":           11,
					"latestRevNumber": 3,
					"outdated":        true,
					"createdAt":       createdAt,
					"updatedAt":       updatedAt,
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
	for _, expected := range []string{"outdated", "created at", "updated at", "2h ago", "30m ago"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack list output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"services", "PHP", createdAt, updatedAt} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("stack list output should not include %q: %s", unwanted, output)
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
			"id":              1,
			"name":            "drupal",
			"title":           "Drupal",
			"status":          "active",
			"public":          true,
			"revId":           11,
			"latestRevNumber": 3,
			"outdated":        false,
			"createdAt":       "2026-01-02T03:04:05Z",
			"updatedAt":       "2026-01-03T04:05:06Z",
			"services": []map[string]interface{}{
				{"title": "PHP"},
				{"service": map[string]interface{}{"title": "Nginx"}},
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
	for _, expected := range []string{"outdated:", "false", "created at:", "updated at:", "services:", "2026-01-02 03:04", "2026-01-03 04:05", "PHP, Nginx"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("stack get output should include %q: %s", expected, output)
		}
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
		{name: "user", cmd: newDatabaseUserCommand(), expected: []string{"list", "create", "dbs", "delete"}},
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

	for _, name := range []string{"list", "get", "deploy"} {
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
	for _, expected := range []string{"kubernetes version:", "1.31", "infra version:", "2026.06", "ips:", "203.0.113.10", "nodes:", "2/5", "single node:", "false", "scalable:", "true"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster get output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "instances:") {
		t.Fatalf("cluster get output should not include instances: %s", output)
	}
	if strings.Contains(output, "version:") && !strings.Contains(output, "kubernetes version:") {
		t.Fatalf("cluster get output should use kubernetes version title: %s", output)
	}
}

func TestClusterAppListShowsSingleInstanceID(t *testing.T) {
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
					"stack": map[string]interface{}{
						"id":    7,
						"title": "Varnish Stack",
					},
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
					"id":     21,
					"appId":  11,
					"status": "running",
				},
			})
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
	for _, expected := range []string{"instance id", "21", "Varnish", "Varnish Stack"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("cluster infra app output should include %q: %s", expected, output)
		}
	}
	for _, unwanted := range []string{"appId", "instances", "appInstances"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cluster infra app output should not include %q: %s", unwanted, output)
		}
	}
}

func TestClusterListShowsSpecialIntegrationLabelsWithoutEnrichment(t *testing.T) {
	var out bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	for _, expected := range []string{"outdated", "app", "stack", "env", "cluster", "domain", "Drupal", "Drupal Stack", "Prod", "Primary", "example.com"} {
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
	for _, expected := range []string{"app:", "Drupal", "app id:", "11", "stack:", "Drupal Stack", "stack id:", "7"} {
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
	for _, expected := range []string{"app:", "Drupal", "app id:", "11", "stack:", "Drupal Stack", "stack id:", "7"} {
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
