package wodby1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wodby/wodby-cli/pkg/api/rest"
)

func TestBuildTerminalFailureIsRetriedAsNewAttempt(t *testing.T) {
	var posts atomic.Int32
	var created sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-builds":
			attempt := int(posts.Add(1))
			id := 100 + attempt
			taskID := 200 + attempt
			createdAt := time.Now().UTC()
			created.Store(id, createdAt)
			writeTargetExecutionJSON(t, w, TargetAppBuildsCreateResponse{
				Items:  []TargetAppBuild{asyncTestBuild(id, 10, taskID, "QUEUED", createdAt)},
				TaskID: &taskID,
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/app-builds/"):
			id := asyncTestPathID(t, r.URL.Path)
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("GET for unknown build ID %d", id)
			}
			status := "COMPLETED"
			if id == 101 {
				status = "ERRORED"
			}
			writeTargetExecutionJSON(
				t,
				w,
				asyncTestBuild(id, 10, 100+id, status, createdAt.(time.Time)),
			)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, statePath, 100*time.Millisecond)
	operation := operationKey("build", strconv.Itoa(10))

	if _, err := executor.ensureBuild(
		context.Background(),
		state,
		"instance-1",
		20,
		10,
		TargetBuildSourceInput{},
	); err == nil {
		t.Fatal("expected terminal build failure")
	}
	failed := state.Instances["instance-1"].Operations[operation]
	if failed.Status != MigrationOperationFailed ||
		failed.FailureCode != "target_terminal" ||
		failed.Attempts != 1 {
		t.Fatalf("failed build operation = %+v", failed)
	}

	build, err := executor.ensureBuild(
		context.Background(),
		state,
		"instance-1",
		20,
		10,
		TargetBuildSourceInput{},
	)
	if err != nil {
		t.Fatalf("retry build: %v", err)
	}
	if build.ID != 102 || posts.Load() != 2 {
		t.Fatalf("retried build ID=%d POSTs=%d, want ID 102 and two POSTs", build.ID, posts.Load())
	}
	completed := state.Instances["instance-1"].Operations[operation]
	if completed.Status != MigrationOperationSucceeded || completed.Attempts != 2 ||
		completed.TargetID != 102 {
		t.Fatalf("completed build operation = %+v", completed)
	}
}

func TestDeploymentTerminalFailureIsRetriedAsNewAttempt(t *testing.T) {
	var posts atomic.Int32
	var created sync.Map
	input := TargetCreateAppDeploymentInput{
		Services: []TargetAppServiceDeploymentInput{{
			AppServiceID: 10,
			Force:        true,
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-deployments":
			attempt := int(posts.Add(1))
			id := 300 + attempt
			taskID := 400 + attempt
			createdAt := time.Now().UTC()
			created.Store(id, createdAt)
			writeTargetExecutionJSON(
				t,
				w,
				asyncTestDeployment(id, 20, taskID, "QUEUED", createdAt),
			)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/app-deployments/"):
			id := asyncTestPathID(t, r.URL.Path)
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("GET for unknown deployment ID %d", id)
			}
			status := "COMPLETED"
			if id == 301 {
				status = "CANCELED"
			}
			writeTargetExecutionJSON(
				t,
				w,
				asyncTestDeployment(id, 20, 100+id, status, createdAt.(time.Time)),
			)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, statePath, 100*time.Millisecond)
	operation := operationKey("deploy", "instance-1")

	if err := executor.ensureDeployment(
		context.Background(),
		state,
		"instance-1",
		20,
		operation,
		input,
	); err == nil {
		t.Fatal("expected terminal deployment failure")
	}
	failed := state.Instances["instance-1"].Operations[operation]
	if failed.Status != MigrationOperationFailed ||
		failed.FailureCode != "target_terminal" ||
		failed.Attempts != 1 {
		t.Fatalf("failed deployment operation = %+v", failed)
	}

	if err := executor.ensureDeployment(
		context.Background(),
		state,
		"instance-1",
		20,
		operation,
		input,
	); err != nil {
		t.Fatalf("retry deployment: %v", err)
	}
	completed := state.Instances["instance-1"].Operations[operation]
	if posts.Load() != 2 ||
		completed.Status != MigrationOperationSucceeded ||
		completed.Attempts != 2 ||
		completed.TargetID != 302 {
		t.Fatalf("retried deployment POSTs=%d operation=%+v", posts.Load(), completed)
	}
}

func TestImportTerminalFailureIsRetriedAsNewAttempt(t *testing.T) {
	var posts atomic.Int32
	var created sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/imports":
			attempt := int(posts.Add(1))
			id := 500 + attempt
			created.Store(id, time.Now().UTC())
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/imports":
			attempt := int(posts.Load())
			id := 500 + attempt
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("list before import attempt was recorded")
			}
			writeTargetExecutionJSON(
				t,
				w,
				[]TargetImport{asyncTestImport(id, 20, 10, "QUEUED", createdAt.(time.Time))},
			)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/imports/"):
			id := asyncTestPathID(t, r.URL.Path)
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("GET for unknown import ID %d", id)
			}
			status := "COMPLETED"
			if id == 501 {
				status = "ERRORED"
			}
			writeTargetExecutionJSON(
				t,
				w,
				asyncTestImport(id, 20, 10, status, createdAt.(time.Time)),
			)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item := PreparedDataImport{
		SourceInstanceUUID: "instance-1",
		Backup: Backup{
			Component: "database",
			URL:       "https://objects.example.test/database.sql.gz?signature=redacted",
		},
		Destination: PreparedImport{ImportName: "database"},
	}
	state, statePath := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, statePath, 100*time.Millisecond)
	operation := operationKey("import", "instance-1", "database")

	if err := executor.ensureImport(context.Background(), state, item, 20, 10); err == nil {
		t.Fatal("expected terminal import failure")
	}
	failed := state.Instances["instance-1"].Operations[operation]
	if failed.Status != MigrationOperationFailed ||
		failed.FailureCode != "target_terminal" ||
		failed.Attempts != 1 {
		t.Fatalf("failed import operation = %+v", failed)
	}

	if err := executor.ensureImport(context.Background(), state, item, 20, 10); err != nil {
		t.Fatalf("retry import: %v", err)
	}
	completed := state.Instances["instance-1"].Operations[operation]
	if posts.Load() != 2 ||
		completed.Status != MigrationOperationSucceeded ||
		completed.Attempts != 2 ||
		completed.TargetID != 502 {
		t.Fatalf("retried import POSTs=%d operation=%+v", posts.Load(), completed)
	}
}

func TestImportRecoveryQueriesByTargetService(t *testing.T) {
	const (
		instanceID = 20
		serviceID  = 10
		importID   = 501
		taskID     = 601
	)
	createdAt := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/imports":
			writeTargetExecutionJSON(t, w, TargetOperationResult{
				Success: true,
				TaskID:  asyncTestIntPointer(taskID),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/601":
			writeTargetExecutionJSON(t, w, TargetTask{ID: taskID, UserID: 1, Status: "DONE"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/imports":
			if got := r.URL.Query(); got.Get("appServiceId") != "10" {
				t.Fatalf("import recovery query = %q", r.URL.RawQuery)
			}
			filesImport := TargetImport{
				ID: importID, Name: "files", Status: "COMPLETED",
				AppInstanceID: asyncTestIntPointer(instanceID),
				AppServiceID:  asyncTestIntPointer(serviceID),
				TaskID:        asyncTestIntPointer(taskID),
				CreatedAt:     createdAt,
			}
			if r.URL.Query().Get("appInstanceId") == "" {
				writeTargetExecutionJSON(t, w, []TargetImport{filesImport})
				return
			}
			// Wodby 2 currently treats appInstanceId as the primary import
			// filter and returns all imports for it, including another service.
			writeTargetExecutionJSON(t, w, []TargetImport{
				{
					ID: 500, Name: "database", Status: "COMPLETED",
					AppInstanceID: asyncTestIntPointer(instanceID),
					AppServiceID:  asyncTestIntPointer(11),
					TaskID:        asyncTestIntPointer(600),
					CreatedAt:     createdAt.Add(-time.Minute),
				},
				filesImport,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/imports/501":
			writeTargetExecutionJSON(t, w, TargetImport{
				ID: importID, Name: "files", Status: "COMPLETED",
				AppInstanceID: asyncTestIntPointer(instanceID),
				AppServiceID:  asyncTestIntPointer(serviceID),
				TaskID:        asyncTestIntPointer(taskID),
				CreatedAt:     createdAt,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item := PreparedDataImport{
		SourceInstanceUUID: "instance-1",
		Backup: Backup{
			Component: "files",
			URL:       "https://objects.example.test/files.tar.gz?signature=redacted",
		},
		Destination: PreparedImport{ImportName: "files"},
	}
	state, statePath := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, statePath, 100*time.Millisecond)

	if err := executor.ensureImport(context.Background(), state, item, instanceID, serviceID); err != nil {
		t.Fatalf("files import recovery: %v", err)
	}
	operation := state.Instances["instance-1"].Operations[importOperationKey("instance-1", "files", false)]
	if operation.Status != MigrationOperationSucceeded || operation.TargetID != importID {
		t.Fatalf("files import operation = %+v", operation)
	}
}

func TestImportPrefersMirrorAndFallsBackToServerAfterDefinitiveFailure(t *testing.T) {
	var posts atomic.Int32
	var created sync.Map
	var requestedURLs sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/imports":
			attempt := int(posts.Add(1))
			body := decodeTargetExecutionObject(t, r)
			input, ok := body["import"].(map[string]interface{})
			if !ok {
				t.Fatalf("import body = %#v", body)
			}
			requestedURLs.Store(attempt, input["url"])
			id := 500 + attempt
			created.Store(id, time.Now().UTC())
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/imports":
			attempt := int(posts.Load())
			id := 500 + attempt
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("list before import attempt was recorded")
			}
			writeTargetExecutionJSON(
				t,
				w,
				[]TargetImport{asyncTestImport(id, 20, 10, "QUEUED", createdAt.(time.Time))},
			)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/imports/"):
			id := asyncTestPathID(t, r.URL.Path)
			createdAt, ok := created.Load(id)
			if !ok {
				t.Fatalf("GET for unknown import ID %d", id)
			}
			status := "COMPLETED"
			if id == 501 {
				status = "ERRORED"
			}
			writeTargetExecutionJSON(t, w, asyncTestImport(id, 20, 10, status, createdAt.(time.Time)))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-instances/20":
			writeTargetExecutionJSON(t, w, TargetAppInstance{
				ID: 20, Name: "dev", Status: "OK", AppID: 1,
				ClusterID: 2, EnvID: 3, StackID: 4, StackRevID: 5,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	item := PreparedDataImport{
		SourceInstanceUUID: "instance-1",
		Backup: Backup{
			Component:   "database",
			URL:         "https://node.example.test/database.sql.gz",
			MirroredURL: "https://mirror.example.test/database.sql.gz",
		},
		Destination: PreparedImport{ImportName: "database"},
	}
	state, statePath := newExecutorTestState(t)
	progress := []string{}
	executor := newAsyncTestExecutor(t, server.URL, statePath, 100*time.Millisecond)
	executor.progress = func(message string) {
		progress = append(progress, message)
	}

	if err := executor.ensureImport(context.Background(), state, item, 20, 10); err != nil {
		t.Fatalf("mirror fallback import: %v", err)
	}
	if posts.Load() != 2 {
		t.Fatalf("import POSTs = %d, want 2", posts.Load())
	}
	wantURLs := []string{item.Backup.MirroredURL, item.Backup.URL}
	for attempt, want := range wantURLs {
		got, ok := requestedURLs.Load(attempt + 1)
		if !ok || got != want {
			t.Fatalf("attempt %d URL = %#v, want %q", attempt+1, got, want)
		}
	}
	mirrorOperation := state.Instances["instance-1"].Operations[importOperationKey("instance-1", "database", true)]
	serverOperation := state.Instances["instance-1"].Operations[importOperationKey("instance-1", "database", false)]
	if mirrorOperation.Status != MigrationOperationFailed ||
		serverOperation.Status != MigrationOperationSucceeded ||
		serverOperation.TargetID != 502 {
		t.Fatalf("mirror operation=%+v server operation=%+v", mirrorOperation, serverOperation)
	}
	if output := strings.Join(progress, "\n"); !strings.Contains(output, "retrying from the Wodby 1 server") {
		t.Fatalf("fallback progress not reported:\n%s", output)
	}
}

func TestBuildPollingTimeoutResumesAcceptedTargetWithoutDuplicatePost(t *testing.T) {
	var posts atomic.Int32
	var complete atomic.Bool
	var createdAt time.Time
	const (
		buildID   = 701
		serviceID = 10
		taskID    = 801
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-builds":
			posts.Add(1)
			createdAt = time.Now().UTC()
			writeTargetExecutionJSON(t, w, TargetAppBuildsCreateResponse{
				Items:  []TargetAppBuild{asyncTestBuild(buildID, serviceID, taskID, "QUEUED", createdAt)},
				TaskID: asyncTestIntPointer(taskID),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-builds/701":
			status := "QUEUED"
			if complete.Load() {
				status = "COMPLETED"
			}
			writeTargetExecutionJSON(
				t,
				w,
				asyncTestBuild(buildID, serviceID, taskID, status, createdAt),
			)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, statePath, 8*time.Millisecond)
	operation := operationKey("build", strconv.Itoa(serviceID))

	if _, err := executor.ensureBuild(
		context.Background(),
		state,
		"instance-1",
		20,
		serviceID,
		TargetBuildSourceInput{},
	); err == nil {
		t.Fatalf("polling timeout error = %v", err)
	}
	accepted := state.Instances["instance-1"].Operations[operation]
	if accepted.Status != MigrationOperationAccepted ||
		accepted.Attempts != 1 ||
		accepted.TargetID != buildID ||
		accepted.TaskID != taskID {
		t.Fatalf("accepted build operation = %+v", accepted)
	}

	reloaded, err := LoadMigrationState(statePath, state.Identity())
	if err != nil {
		t.Fatalf("reload accepted state: %v", err)
	}
	persisted := reloaded.Instances["instance-1"].Operations[operation]
	if persisted.Status != MigrationOperationAccepted ||
		persisted.TargetID != buildID ||
		persisted.TaskID != taskID {
		t.Fatalf("persisted accepted build operation = %+v", persisted)
	}

	complete.Store(true)
	build, err := executor.ensureBuild(
		context.Background(),
		reloaded,
		"instance-1",
		20,
		serviceID,
		TargetBuildSourceInput{},
	)
	if err != nil {
		t.Fatalf("resume accepted build: %v", err)
	}
	if build.ID != buildID || posts.Load() != 1 {
		t.Fatalf("resumed build ID=%d POSTs=%d, want same build and one POST", build.ID, posts.Load())
	}
	completed := reloaded.Instances["instance-1"].Operations[operation]
	if completed.Status != MigrationOperationSucceeded ||
		completed.Attempts != 1 ||
		completed.TargetID != buildID ||
		completed.TaskID != taskID {
		t.Fatalf("completed resumed build operation = %+v", completed)
	}
}

func TestChildMutationErrorsPreserveReadyInstanceResource(t *testing.T) {
	tests := []struct {
		name        string
		mutationErr error
		wantStatus  MigrationOperationStatus
	}{
		{
			name:        "client rejection",
			mutationErr: &rest.APIError{StatusCode: http.StatusUnprocessableEntity},
			wantStatus:  MigrationOperationFailed,
		},
		{
			name:        "transport uncertainty",
			mutationErr: errors.New("connection reset"),
			wantStatus:  MigrationOperationAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, statePath := newExecutorTestState(t)
			if err := state.SetInstanceTarget("instance-1", 20, MigrationResourceReady); err != nil {
				t.Fatal(err)
			}
			const operation = "setting.deadbeef"
			if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
				t.Fatal(err)
			}
			if err := SaveMigrationState(statePath, state); err != nil {
				t.Fatal(err)
			}

			executor := &MigrationExecutor{statePath: statePath}
			if err := executor.recordInstanceMutationError(
				state,
				"instance-1",
				operation,
				"child resource mutation",
				test.mutationErr,
			); err == nil {
				t.Fatal("expected mutation error")
			}
			resource := state.Instances["instance-1"]
			if resource.Status != MigrationResourceReady || resource.TargetID != 20 {
				t.Fatalf("ready instance resource changed after child error: %+v", resource)
			}
			if got := resource.Operations[operation].Status; got != test.wantStatus {
				t.Fatalf("operation status = %q, want %q", got, test.wantStatus)
			}
		})
	}
}

func TestInstanceCreationErrorsChangeZeroIDResourceStatus(t *testing.T) {
	tests := []struct {
		name        string
		mutationErr error
		wantStatus  MigrationResourceStatus
	}{
		{
			name:        "client rejection",
			mutationErr: &rest.APIError{StatusCode: http.StatusUnprocessableEntity},
			wantStatus:  MigrationResourceFailed,
		},
		{
			name:        "transport uncertainty",
			mutationErr: errors.New("connection reset"),
			wantStatus:  MigrationResourceAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, statePath := newExecutorTestState(t)
			if err := state.SetInstanceTarget("instance-1", 0, MigrationResourceCreating); err != nil {
				t.Fatal(err)
			}
			const operation = "create"
			if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
				t.Fatal(err)
			}
			if err := SaveMigrationState(statePath, state); err != nil {
				t.Fatal(err)
			}

			executor := &MigrationExecutor{statePath: statePath}
			if err := executor.recordInstanceCreationMutationError(
				state,
				"instance-1",
				operation,
				"app instance creation",
				test.mutationErr,
			); err == nil {
				t.Fatal("expected creation error")
			}
			resource := state.Instances["instance-1"]
			if resource.Status != test.wantStatus || resource.TargetID != 0 {
				t.Fatalf("zero-ID instance resource = %+v, want status %q", resource, test.wantStatus)
			}
		})
	}
}

func newAsyncTestExecutor(
	t *testing.T,
	serverURL string,
	statePath string,
	timeout time.Duration,
) *MigrationExecutor {
	t.Helper()
	executor, err := NewMigrationExecutor(
		mustTargetExecutionClient(t, serverURL),
		MigrationExecutorOptions{
			StatePath:        statePath,
			PollInterval:     time.Millisecond,
			OperationTimeout: timeout,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func asyncTestBuild(
	id int,
	serviceID int,
	taskID int,
	status string,
	createdAt time.Time,
) TargetAppBuild {
	return TargetAppBuild{
		ID:            id,
		Status:        status,
		AppInstanceID: 20,
		AppServiceID:  serviceID,
		TaskID:        asyncTestIntPointer(taskID),
		CreatedAt:     createdAt,
	}
}

func asyncTestDeployment(
	id int,
	instanceID int,
	taskID int,
	status string,
	createdAt time.Time,
) TargetAppDeployment {
	return TargetAppDeployment{
		ID:            id,
		Status:        status,
		AppInstanceID: instanceID,
		TaskID:        asyncTestIntPointer(taskID),
		AppServiceDeployments: []TargetAppServiceDeployment{{
			ID:           1000 + id,
			AppServiceID: 10,
			Force:        true,
		}},
		CreatedAt: createdAt,
	}
}

func asyncTestImport(
	id int,
	instanceID int,
	serviceID int,
	status string,
	createdAt time.Time,
) TargetImport {
	return TargetImport{
		ID:            id,
		Name:          "database",
		Status:        status,
		AppInstanceID: asyncTestIntPointer(instanceID),
		AppServiceID:  asyncTestIntPointer(serviceID),
		CreatedAt:     createdAt,
	}
}

func asyncTestIntPointer(value int) *int {
	return &value
}

func asyncTestPathID(t *testing.T, path string) int {
	t.Helper()
	id, err := strconv.Atoi(path[strings.LastIndex(path, "/")+1:])
	if err != nil {
		t.Fatalf("parse target ID from %q: %v", path, err)
	}
	return id
}
