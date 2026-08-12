package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestCreatedWithinOperationUsesTimestampBoundary(t *testing.T) {
	intent := time.Date(2026, 7, 26, 12, 0, 0, 900_000_000, time.UTC)
	operation := MigrationOperationState{IntentAt: intent}
	if !createdWithinOperation(intent.Truncate(time.Second), operation) {
		t.Fatal("same-second API timestamp should be recoverable")
	}
	if createdWithinOperation(intent.Add(-time.Second), operation) {
		t.Fatal("resource created before intent second must not be recoverable")
	}
	if createdWithinOperation(intent.Add(migrationRecoveryWindow+time.Second), operation) {
		t.Fatal("resource created after the bounded recovery window must not be recoverable")
	}
	if createdWithinOperation(time.Time{}, operation) {
		t.Fatal("missing creation timestamp must fail closed")
	}
}

func TestLoadStateUsesInstanceSourceIdentity(t *testing.T) {
	export, prepared := refreshDataImportFixture()
	export.Source = &ExportSource{Kind: "instance", UUID: "instance-1"}
	configDigest, err := export.MigrationConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan := reviewedPlanFixture(t)
	plan.Source.Kind = "instance"
	plan.Source.ID = "instance-1"
	plan.Source.ConfigDigest = configDigest
	digest, err := plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = digest

	executor := &MigrationExecutor{statePath: filepath.Join(t.TempDir(), "instance-state.json")}
	state, initialized, err := executor.loadState(export, plan, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized || state.Source.Kind != "instance" || state.Source.ID != "instance-1" {
		t.Fatalf("initialized=%v source=%#v", initialized, state.Source)
	}
}

func TestAmbiguousMutationIsNotRetriedByDefault(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	const operation = "route.0123456789abcdef"
	if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationAmbiguous("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	before := state.Instances["instance-1"].Operations[operation]

	executor := &MigrationExecutor{statePath: statePath}
	run, err := executor.beginInstanceMutation(
		state,
		"instance-1",
		operation,
		false,
	)
	if err == nil || run {
		t.Fatalf("ambiguous operation retry = %v, %v; want blocked", run, err)
	}
	wantRetry := `--retry-ambiguous "instance:instance-1:route.0123456789abcdef"`
	if !strings.Contains(err.Error(), wantRetry) {
		t.Fatalf("error = %q, want exact acknowledgement %q", err, wantRetry)
	}
	after := state.Instances["instance-1"].Operations[operation]
	if after.Attempts != before.Attempts || after.Status != MigrationOperationAmbiguous {
		t.Fatalf("ambiguous operation was modified: before=%+v after=%+v", before, after)
	}
}

func TestExplicitAmbiguousRetryRecordsNewIntent(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	const operation = "route.0123456789abcdef"
	if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationAmbiguous("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}

	executor := &MigrationExecutor{
		statePath:               statePath,
		retryAmbiguousOperation: instanceAmbiguousRetryOperation("instance-1", operation),
	}
	run, err := executor.beginInstanceMutation(state, "instance-1", operation, false)
	if err != nil || !run {
		t.Fatalf("explicit retry = %v, %v; want new intent", run, err)
	}
	got := state.Instances["instance-1"].Operations[operation]
	if got.Status != MigrationOperationIntent || got.Attempts != 2 {
		t.Fatalf("new intent = %+v", got)
	}
}

func TestAmbiguousRetryAcknowledgementCannotAuthorizeAnotherOperation(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	const (
		sourceID       = "instance-1"
		acknowledgedOp = "route.0123456789abcdef"
		otherOp        = "import.fedcba9876543210"
		otherSourceID  = "instance-2"
	)
	state.Instances[otherSourceID] = newMigrationResourceState()
	for _, operation := range []string{acknowledgedOp, otherOp} {
		if err := state.MarkInstanceOperationIntent(sourceID, operation); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkInstanceOperationAmbiguous(sourceID, operation); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.MarkInstanceOperationIntent(otherSourceID, acknowledgedOp); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationAmbiguous(otherSourceID, acknowledgedOp); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}

	executor := &MigrationExecutor{
		statePath: statePath,
		retryAmbiguousOperation: instanceAmbiguousRetryOperation(
			sourceID,
			acknowledgedOp,
		),
	}
	run, err := executor.beginInstanceMutation(state, sourceID, acknowledgedOp, false)
	if err != nil || !run {
		t.Fatalf("acknowledged retry = %v, %v; want allowed", run, err)
	}

	run, err = executor.beginInstanceMutation(state, sourceID, otherOp, false)
	if err == nil || run {
		t.Fatalf("second ambiguous retry = %v, %v; want blocked", run, err)
	}
	wantOther := `--retry-ambiguous "instance:instance-1:import.fedcba9876543210"`
	if !strings.Contains(err.Error(), wantOther) {
		t.Fatalf("error = %q, want exact second acknowledgement %q", err, wantOther)
	}

	run, err = executor.beginInstanceMutation(state, otherSourceID, acknowledgedOp, false)
	if err == nil || run {
		t.Fatalf("other resource ambiguous retry = %v, %v; want blocked", run, err)
	}
	wantOtherResource := `--retry-ambiguous "instance:instance-2:route.0123456789abcdef"`
	if !strings.Contains(err.Error(), wantOtherResource) {
		t.Fatalf("error = %q, want exact other-resource acknowledgement %q", err, wantOtherResource)
	}
}

func TestAmbiguousRetryOperationIDsScopeAppAndInstances(t *testing.T) {
	if got := appCreateAmbiguousRetryOperation("app-1"); got != "app:app-1:create" {
		t.Fatalf("combined app and initial instance create acknowledgement = %q", got)
	}
	if got := appCreateAmbiguousRetryOperation("app-2"); got == appCreateAmbiguousRetryOperation("app-1") {
		t.Fatalf("app acknowledgement is not source-scoped: %q", got)
	}
	if got := instanceAmbiguousRetryOperation("later-uuid", "create"); got != "instance:later-uuid:create" {
		t.Fatalf("later instance acknowledgement = %q", got)
	}
}

func TestMutationErrorsDoNotExposeOrPersistSignedURL(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{
			name: "transport",
			err:  errors.New("Post \"https://signed.example/backup?token=secret\": connection reset"),
		},
		{
			name: "server",
			err: &rest.APIError{
				StatusCode: 503,
				Status:     "503 Service Unavailable",
				Body:       "https://signed.example/backup?token=secret",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, statePath := newExecutorTestState(t)
			const operation = "import.0123456789abcdef"
			if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
				t.Fatal(err)
			}
			if err := SaveMigrationState(statePath, state); err != nil {
				t.Fatal(err)
			}
			executor := &MigrationExecutor{statePath: statePath}
			gotErr := executor.recordInstanceMutationError(
				state,
				"instance-1",
				operation,
				"data import start",
				test.err,
			)
			if gotErr == nil {
				t.Fatal("expected ambiguous mutation error")
			}
			if strings.Contains(gotErr.Error(), "signed.example") ||
				strings.Contains(gotErr.Error(), "secret") {
				t.Fatalf("returned error exposed signed URL: %v", gotErr)
			}
			wantRetry := `--retry-ambiguous "instance:instance-1:import.0123456789abcdef"`
			if !strings.Contains(gotErr.Error(), wantRetry) {
				t.Fatalf("error = %q, want exact acknowledgement %q", gotErr, wantRetry)
			}
			data, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "signed.example") ||
				strings.Contains(string(data), "secret") {
				t.Fatalf("state exposed signed URL: %s", data)
			}
			op := state.Instances["instance-1"].Operations[operation]
			if op.Status != MigrationOperationAmbiguous {
				t.Fatalf("5xx/transport status = %q, want ambiguous", op.Status)
			}
		})
	}
}

func TestAmbiguousAppCreateErrorPrintsCombinedOperationAcknowledgement(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationIntent("instance-1", "create"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}

	executor := &MigrationExecutor{statePath: statePath}
	err := executor.recordPairMutationError(
		state,
		"instance-1",
		"create",
		"app creation",
		errors.New("connection reset"),
	)
	wantRetry := `--retry-ambiguous "app:app-1:create"`
	if err == nil || !strings.Contains(err.Error(), wantRetry) {
		t.Fatalf("error = %q, want exact acknowledgement %q", err, wantRetry)
	}
}

func TestClientRejectionIsDefinitiveFailure(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	const operation = "setting.0123456789abcdef"
	if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	executor := &MigrationExecutor{statePath: statePath}
	err := executor.recordInstanceMutationError(
		state,
		"instance-1",
		operation,
		"service setting update",
		&rest.APIError{
			StatusCode: 422,
			Status:     "422 Unprocessable Entity",
			Message:    "setting value is invalid",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "HTTP 422") ||
		!strings.Contains(err.Error(), "setting value is invalid") {
		t.Fatalf("error = %v", err)
	}
	op := state.Instances["instance-1"].Operations[operation]
	if op.Status != MigrationOperationFailed || op.FailureCode != "api_rejected" {
		t.Fatalf("operation = %+v", op)
	}
}

func TestDefinitiveAppCreateRejectionIncludesSafeStructuredDetail(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationIntent("instance-1", "create"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}

	executor := &MigrationExecutor{statePath: statePath}
	err := executor.recordPairMutationError(
		state,
		"instance-1",
		"create",
		"app creation",
		&rest.APIError{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Message:    "volume is invalid; details at https://signed.example/backup?token=secret",
			Body:       "raw response containing another-secret",
		},
	)
	if err == nil {
		t.Fatal("expected app creation rejection")
	}
	if !strings.Contains(err.Error(), "volume is invalid") {
		t.Fatalf("error omitted structured validation detail: %v", err)
	}
	for _, sensitive := range []string{"signed.example", "token=secret", "another-secret"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error exposed %q: %v", sensitive, err)
		}
	}
	if !strings.Contains(err.Error(), "[redacted URL]") {
		t.Fatalf("error did not mark redacted URL: %v", err)
	}
	if state.App.Operations["create"].Status != MigrationOperationFailed ||
		state.Instances["instance-1"].Operations["create"].Status != MigrationOperationFailed {
		t.Fatalf("app/instance operations were not marked failed: app=%+v instance=%+v",
			state.App.Operations["create"],
			state.Instances["instance-1"].Operations["create"],
		)
	}
}

func TestPotentiallyPostMutationHTTPResponsesRemainAmbiguous(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusTooEarly,
		499,
	} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			state, statePath := newExecutorTestState(t)
			const operation = "setting.0123456789abcdef"
			if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
				t.Fatal(err)
			}
			if err := SaveMigrationState(statePath, state); err != nil {
				t.Fatal(err)
			}
			executor := &MigrationExecutor{statePath: statePath}
			err := executor.recordInstanceMutationError(
				state,
				"instance-1",
				operation,
				"service setting update",
				&rest.APIError{StatusCode: statusCode},
			)
			if err == nil || !strings.Contains(err.Error(), "ambiguous") {
				t.Fatalf("error = %v", err)
			}
			if got := state.Instances["instance-1"].Operations[operation].Status; got != MigrationOperationAmbiguous {
				t.Fatalf("operation status = %q", got)
			}
		})
	}
}

func TestEnsureServiceEnvironmentOverridesCompiledDefaultsAndWritesProtectedValue(t *testing.T) {
	var updateBody map[string]interface{}
	secretMarker := -1
	persistedSecretID := 31
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/10/env-vars":
			writeTargetExecutionJSON(t, w, []TargetAppServiceEnvVar{
				{
					ID: -1, AppServiceID: 10, Name: "APP_SECRET",
					ValueSecretID: &secretMarker, Runtime: true,
					Source: &TargetAppServiceEnvVarSource{FromStack: true},
				},
				{
					ID: 20, AppServiceID: 10, Name: "APP_SECRET",
					ValueSecretID: &persistedSecretID, Runtime: true,
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-service-env-vars/20":
			updateBody = decodeTargetExecutionObject(t, r)
			writeTargetExecutionJSON(t, w, TargetAppServiceEnvVar{
				ID: 20, AppServiceID: 10, Name: "APP_SECRET",
				ValueSecretID: &persistedSecretID, Runtime: true,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	client := mustTargetExecutionClient(t, server.URL)
	executor := &MigrationExecutor{target: client, statePath: statePath}
	err := executor.ensureServiceEnvironment(
		context.Background(),
		state,
		"instance-1",
		TargetAppService{ID: 10},
		Service{EnvVars: []EnvVar{{
			Name: "APP_SECRET", Value: "source-secret", Enabled: true,
			Origin: "custom", Protected: true,
		}}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if updateBody["value"] != "source-secret" || updateBody["secret"] != true {
		t.Fatalf("protected override body = %#v", updateBody)
	}
	if updateBody["runtime"] != true || updateBody["build"] != false {
		t.Fatalf("protected override scope = %#v", updateBody)
	}
}

func TestEnsureServiceEnvironmentCreatesGlobalOverrideForInheritedAndScopedEntries(t *testing.T) {
	var createBody map[string]interface{}
	secretMarker := -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/10/env-vars":
			writeTargetExecutionJSON(t, w, []TargetAppServiceEnvVar{
				{
					ID: -1, AppServiceID: 10, Name: "APP_SECRET",
					ValueSecretID: &secretMarker, Runtime: true,
					Source: &TargetAppServiceEnvVarSource{FromStack: true},
				},
				{
					ID: 22, AppServiceID: 10, Name: "APP_SECRET",
					Workload: "worker", Container: "php", Runtime: true,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/10/env-vars":
			createBody = decodeTargetExecutionObject(t, r)
			persistedSecretID := 31
			writeTargetExecutionJSON(t, w, TargetAppServiceEnvVar{
				ID: 20, AppServiceID: 10, Name: "APP_SECRET",
				ValueSecretID: &persistedSecretID, Runtime: true,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{
		target:    mustTargetExecutionClient(t, server.URL),
		statePath: statePath,
	}
	err := executor.ensureServiceEnvironment(
		context.Background(),
		state,
		"instance-1",
		TargetAppService{ID: 10},
		Service{EnvVars: []EnvVar{{
			Name: "APP_SECRET", Value: "source-secret", Enabled: true,
			Origin: "custom", Protected: true,
		}}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if createBody["name"] != "APP_SECRET" ||
		createBody["value"] != "source-secret" ||
		createBody["secret"] != true {
		t.Fatalf("protected override body = %#v", createBody)
	}
}

func TestPrepareInstanceDoesNotApplyPayloadFromDisabledSourceService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			r.URL.Path != "/v1/app-services" ||
			r.URL.Query().Get("appInstanceId") != "20" {
			t.Fatalf("disabled service payload triggered unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeTargetExecutionJSON(t, w, []TargetAppService{{
			ID: 10, Name: "worker", AppInstanceID: 20, ServiceRevID: 101, Disabled: true,
		}})
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{
		target:    mustTargetExecutionClient(t, server.URL),
		statePath: statePath,
	}
	source := Service{
		Name:    "worker",
		Enabled: false,
		Configuration: map[string]interface{}{
			"token": "must-not-be-applied",
		},
		EnvVars: []EnvVar{{
			Name: "APP_SECRET", Value: "must-not-be-applied", Enabled: true, Origin: "custom",
		}},
		CronJobs: []CronJob{{
			Crontab: "@hourly", Command: "must-not-run", Enabled: true,
		}},
	}
	inspection := TargetStackServiceInspection{
		StackService: TargetStackService{
			ID: 11, Name: "worker", ServiceRevID: 101, Disabled: true,
		},
	}
	err := executor.prepareInstance(
		context.Background(),
		state,
		PreparedInstance{
			Source:        Instance{UUID: "instance-1", Services: []Service{source}},
			StackServices: []TargetStackServiceInspection{inspection},
			Services: map[string]PreparedService{
				"worker": {Source: source, Target: inspection},
			},
			EffectiveState: map[string]bool{"worker": false},
		},
		TargetAppInstance{ID: 20},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnsureServiceVersionIsResumable(t *testing.T) {
	updates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/app-services/10" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		body := decodeTargetExecutionObject(t, r)
		if body["version"] != "8.4" || len(body) != 1 {
			t.Fatalf("service version update body = %#v", body)
		}
		updates++
		writeTargetExecutionJSON(t, w, TargetAppService{
			ID: 10, Name: "php", AppInstanceID: 20, ServiceRevID: 101, Version: "8.4",
		})
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), statePath: statePath}
	for attempt := 0; attempt < 2; attempt++ {
		if err := executor.ensureServiceVersion(
			context.Background(), state, "instance-1",
			TargetAppService{ID: 10, Name: "php", Version: "8.3"}, "8.4",
		); err != nil {
			t.Fatal(err)
		}
	}
	if updates != 1 {
		t.Fatalf("service version updates = %d, want 1", updates)
	}
}

func TestEnsureServiceCronsDisablesDrupalPHPDefaults(t *testing.T) {
	defaultDisabled := false
	updates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/10/cron-schedules":
			writeTargetExecutionJSON(t, w, []TargetAppServiceCronSchedule{
				{
					ID: 31, AppServiceID: 10, Name: "drush", Title: "drush cron",
					Crontab: "0 0 * * *", Command: "drush cron", Disabled: defaultDisabled,
				},
				{
					ID: 32, AppServiceID: 10, Name: "customer", Title: "Customer schedule",
					Crontab: "@hourly", Command: "bin/customer",
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-service-cron-schedules/31":
			body := decodeTargetExecutionObject(t, r)
			if body["disabled"] != true || len(body) != 1 {
				t.Fatalf("default cron update body = %#v", body)
			}
			updates++
			defaultDisabled = true
			writeTargetExecutionJSON(t, w, TargetAppServiceCronSchedule{
				ID: 31, AppServiceID: 10, Name: "drush", Title: "drush cron",
				Crontab: "0 0 * * *", Command: "drush cron", Disabled: true,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{
		target:    mustTargetExecutionClient(t, server.URL),
		statePath: statePath,
	}
	inspection := TargetStackServiceInspection{
		ServiceRevision: TargetServiceRevision{
			Name: "drupal11-php",
			Manifest: &TargetServiceManifest{
				Name: "drupal11-php",
				CronSchedules: []TargetServiceCronSchedule{{
					Name: "drush", Title: "drush cron",
				}},
			},
		},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := executor.ensureServiceCrons(
			context.Background(),
			state,
			"instance-1",
			TargetAppService{ID: 10, Name: "php"},
			Service{Name: "php"},
			inspection,
			false,
		); err != nil {
			t.Fatal(err)
		}
	}
	if updates != 1 {
		t.Fatalf("default cron updates = %d, want 1", updates)
	}
	operation := operationKey("cron_default_disable", "10", "drush")
	if !operationSucceeded(state.Instances["instance-1"], operation) {
		t.Fatalf("default cron disable operation was not recorded: %#v", state.Instances["instance-1"].Operations)
	}
}

func TestEnsureServiceCronsCreatesMigratedScheduleDisabledWithoutFeature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/10/cron-schedules":
			writeTargetExecutionJSON(t, w, []TargetAppServiceCronSchedule{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/10/cron-schedules":
			body := decodeTargetExecutionObject(t, r)
			if body["disabled"] != true {
				t.Fatalf("disabled cron create body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceCronSchedule{
				ID: 41, AppServiceID: 10, Name: body["name"].(string), Title: "Source cron",
				Crontab: "@daily", Command: "drush cron", Disabled: true,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), statePath: statePath}
	err := executor.ensureServiceCrons(
		context.Background(),
		state,
		"instance-1",
		TargetAppService{ID: 10, Name: "php"},
		Service{Name: "php", CronJobs: []CronJob{{
			Title: "Source cron", Crontab: "@daily", Command: "drush cron", Enabled: true,
		}}},
		TargetStackServiceInspection{},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultPHPCronScheduleNamesOnlySelectsDrupalAndWordPress(t *testing.T) {
	tests := []struct {
		service string
		want    bool
	}{
		{service: "drupal-php", want: true},
		{service: "drupal10-php", want: true},
		{service: "drupal11-php", want: true},
		{service: "wordpress-php", want: true},
		{service: "php", want: false},
		{service: "drupal11-php-sshd", want: false},
		{service: "laravel-php", want: false},
	}
	for _, test := range tests {
		t.Run(test.service, func(t *testing.T) {
			inspection := TargetStackServiceInspection{
				ServiceRevision: TargetServiceRevision{
					Name: test.service,
					Manifest: &TargetServiceManifest{
						Name: test.service,
						CronSchedules: []TargetServiceCronSchedule{{
							Name: "default",
						}},
					},
				},
			}
			got := defaultPHPCronScheduleNames(inspection)["default"]
			if got != test.want {
				t.Fatalf("selected = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVerifyServiceCronsRequiresDrupalPHPDefaultDisabled(t *testing.T) {
	disabled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/app-services/10/cron-schedules" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeTargetExecutionJSON(t, w, []TargetAppServiceCronSchedule{{
			ID: 31, AppServiceID: 10, Name: "drush", Title: "drush cron",
			Crontab: "0 0 * * *", Command: "drush cron", Disabled: disabled,
		}})
	}))
	defer server.Close()

	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL)}
	inspection := TargetStackServiceInspection{
		ServiceRevision: TargetServiceRevision{
			Manifest: &TargetServiceManifest{
				Name:          "drupal11-php",
				CronSchedules: []TargetServiceCronSchedule{{Name: "drush"}},
			},
		},
	}
	if err := executor.verifyServiceCrons(
		context.Background(), "instance-1", 10, Service{Name: "php"}, inspection, false,
	); err == nil || !strings.Contains(err.Error(), "is not disabled") {
		t.Fatalf("enabled default cron verification error = %v", err)
	}
	disabled = true
	if err := executor.verifyServiceCrons(
		context.Background(), "instance-1", 10, Service{Name: "php"}, inspection, false,
	); err != nil {
		t.Fatalf("disabled default cron rejected: %v", err)
	}
}

func TestTechnicalDeploymentSkipsPostDeployOnlyForBuiltService(t *testing.T) {
	parentServiceID := 20
	build := TargetAppBuild{
		ID: 30, AppServiceID: 20, Status: "COMPLETED",
		AppServiceBuilds: []TargetAppServiceBuild{{
			ID: 31, AppServiceID: 20, Status: "COMPLETED",
		}},
	}
	input, err := technicalDeploymentInput(
		PreparedInstance{Source: Instance{
			Properties: map[string]interface{}{"post_deploy": false},
		}},
		[]TargetAppService{
			{ID: 10, Name: "database"},
			{ID: 20, Name: "php"},
			{ID: 25, Name: "sshd", ParentAppServiceID: &parentServiceID},
			{ID: 30, Name: "cache"},
		},
		&build,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Services) != 4 {
		t.Fatalf("deployment services = %#v", input.Services)
	}
	for _, service := range input.Services {
		switch service.AppServiceID {
		case 20:
			if service.AppServiceBuildID == nil || *service.AppServiceBuildID != 31 ||
				service.SkipPostDeployment == nil || !*service.SkipPostDeployment {
				t.Fatalf("built service deployment = %#v", service)
			}
		case 25:
			if service.AppServiceBuildID == nil || *service.AppServiceBuildID != 31 ||
				service.SkipPostDeployment != nil {
				t.Fatalf("derivative service deployment = %#v", service)
			}
		case 10, 30:
			if service.AppServiceBuildID != nil || service.SkipPostDeployment != nil {
				t.Fatalf("non-build service received build-only fields: %#v", service)
			}
		default:
			t.Fatalf("unexpected deployment service: %#v", service)
		}
	}
	actual := TargetAppDeployment{}
	for index, service := range input.Services {
		actual.AppServiceDeployments = append(actual.AppServiceDeployments, TargetAppServiceDeployment{
			ID:                 100 + index,
			AppServiceID:       service.AppServiceID,
			AppServiceBuildID:  service.AppServiceBuildID,
			SkipPostDeployment: service.SkipPostDeployment != nil && *service.SkipPostDeployment,
			Force:              service.Force,
		})
	}
	if !deploymentMatchesInput(actual, input) {
		t.Fatalf("deployment with inherited derivative build did not match input: %#v", actual)
	}
}

func TestTechnicalDeploymentOmitsBuildCapableServicesWhenCodeIsSkipped(t *testing.T) {
	prepared := PreparedInstance{
		SkipCode: true,
		StackServices: []TargetStackServiceInspection{
			{
				StackService: TargetStackService{Name: "php"},
				ServiceRevision: TargetServiceRevision{
					Manifest: &TargetServiceManifest{Build: &TargetServiceBuildCapability{Connect: true}},
				},
			},
			{
				StackService: TargetStackService{Name: "worker"},
				ServiceRevision: TargetServiceRevision{
					Manifest: &TargetServiceManifest{Build: &TargetServiceBuildCapability{}},
				},
			},
			{StackService: TargetStackService{Name: "database"}},
			{StackService: TargetStackService{Name: "cache"}},
		},
	}
	input, err := technicalDeploymentInput(
		prepared,
		[]TargetAppService{
			{ID: 10, Name: "database"},
			{ID: 20, Name: "php"},
			{ID: 30, Name: "cache"},
			{ID: 40, Name: "worker"},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Services) != 2 ||
		input.Services[0].AppServiceID != 10 ||
		input.Services[1].AppServiceID != 30 {
		t.Fatalf("deployment services = %#v, want database and cache only", input.Services)
	}
}

func TestTechnicalDeploymentAllowsAllServicesToBeManualWhenCodeIsSkipped(t *testing.T) {
	input, err := technicalDeploymentInput(
		PreparedInstance{
			SkipCode: true,
			StackServices: []TargetStackServiceInspection{{
				StackService: TargetStackService{Name: "php"},
				ServiceRevision: TargetServiceRevision{
					Manifest: &TargetServiceManifest{Build: &TargetServiceBuildCapability{Connect: true}},
				},
			}},
		},
		[]TargetAppService{{ID: 20, Name: "php"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Services) != 0 {
		t.Fatalf("deployment services = %#v, want no automatic deployment", input.Services)
	}
}

func TestSkippedCodeRequiresHealthyManualDeployment(t *testing.T) {
	prepared := PreparedInstance{
		SkipCode:       true,
		EffectiveState: map[string]bool{"php": true},
		StackServices: []TargetStackServiceInspection{{
			StackService: TargetStackService{Name: "php"},
			ServiceRevision: TargetServiceRevision{
				Manifest: &TargetServiceManifest{Build: &TargetServiceBuildCapability{Connect: true}},
			},
		}},
	}
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Status: "CREATING", NeedsRedeploy: true}},
	); err == nil {
		t.Fatal("unhealthy manually deployed code service was accepted")
	}
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Status: "OK"}},
	); err != nil {
		t.Fatalf("healthy manually deployed code service rejected: %v", err)
	}
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Disabled: true, Status: "OK"}},
	); err == nil || !strings.Contains(err.Error(), "enabled state") {
		t.Fatalf("disabled enabled-code service error = %v", err)
	}

	prepared.EffectiveState["php"] = false
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Disabled: true, Status: "CREATING", NeedsRedeploy: true}},
	); err != nil {
		t.Fatalf("intentionally disabled code service rejected: %v", err)
	}
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Status: "OK"}},
	); err == nil || !strings.Contains(err.Error(), "enabled state") {
		t.Fatalf("enabled disabled-code service error = %v", err)
	}

	delete(prepared.EffectiveState, "php")
	if err := validateManuallyDeployedCodeServices(
		prepared,
		[]TargetAppService{{Name: "php", Disabled: true}},
	); err == nil || !strings.Contains(err.Error(), "enabled state") {
		t.Fatalf("missing approved enabled state error = %v", err)
	}
}

func TestEnsureBuildRejectsRecordedBuildForWrongGitRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/app-builds/50" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeTargetExecutionJSON(t, w, TargetAppBuild{
			ID: 50, Status: "COMPLETED", AppInstanceID: 20, AppServiceID: 10,
			GitRef: "unexpected", GitRefType: TargetGitRefBranch,
		})
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	operation := operationKey("build", strconv.Itoa(10))
	if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationSuccessWithIDs("instance-1", operation, 50, 0); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	expectedRef := "main"
	expectedType := TargetGitRefBranch
	executor := &MigrationExecutor{
		target:           mustTargetExecutionClient(t, server.URL),
		statePath:        statePath,
		pollInterval:     time.Millisecond,
		operationTimeout: time.Second,
	}
	_, err := executor.ensureBuild(
		context.Background(),
		state,
		"instance-1",
		20,
		10,
		TargetBuildSourceInput{GitRef: &expectedRef, GitRefType: &expectedType},
	)
	if err == nil || !strings.Contains(err.Error(), "approved service and Git ref") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCustomDNSRequiresSelectedClusterAddress(t *testing.T) {
	lookups := map[string][]string{
		"app.example.com": {"203.0.113.10"},
		"cluster.example": {"203.0.113.10"},
	}
	executor := &MigrationExecutor{
		lookupHost: func(_ context.Context, host string) ([]string, error) {
			items, ok := lookups[host]
			if !ok {
				return nil, errors.New("not found")
			}
			return items, nil
		},
	}
	plan := Plan{Apps: []AppPlan{{Instances: []InstancePlan{{
		Routes: []RoutePlan{{Host: "app.example.com", Action: "create_backend"}},
	}}}}}
	hostname := "cluster.example"
	cluster := TargetCluster{IPs: []string{"203.0.113.10"}, Hostname: &hostname}
	if err := executor.checkCustomDNS(context.Background(), plan, cluster); err != nil {
		t.Fatalf("matching DNS rejected: %v", err)
	}
	lookups["app.example.com"] = []string{"203.0.113.10", "198.51.100.20"}
	if err := executor.checkCustomDNS(context.Background(), plan, cluster); err == nil {
		t.Fatal("DNS split between old and target addresses should be rejected")
	}
	lookups["app.example.com"] = []string{"198.51.100.20"}
	if err := executor.checkCustomDNS(context.Background(), plan, cluster); err == nil {
		t.Fatal("DNS pointing away from target cluster should be rejected")
	}
}

func TestRouteRedirectTarget(t *testing.T) {
	scheme, host, path, err := routeRedirectTarget(RoutePlan{
		Host:           "example.com",
		SSL:            true,
		RedirectTarget: "https://www.example.com/docs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if scheme != "https" || host != "www.example.com" || path != "/docs" {
		t.Fatalf("redirect = %s %s %s", scheme, host, path)
	}
}

func TestBackedOffTaskRemainsActive(t *testing.T) {
	done, err := taskStatusOutcome(42, "BACKED_OFF")
	if err != nil || done {
		t.Fatalf("BACKED_OFF outcome = %v, %v; want active", done, err)
	}
	done, err = taskStatusOutcome(42, "DONE_WITH_WARNINGS")
	if err != nil || !done {
		t.Fatalf("DONE_WITH_WARNINGS outcome = %v, %v; want complete", done, err)
	}
}

func TestTargetRouteCertificateMustBeLetsEncryptAndReady(t *testing.T) {
	route := TargetAppRoute{
		ID: 1, Host: "app.example.com",
		Cert: &TargetCert{ID: 2, Issuer: "letsencrypt", Status: "CREATING"},
	}
	ready, err := targetRouteCertificateReady(route, true)
	if err != nil || ready {
		t.Fatalf("creating certificate = %v, %v; want pending", ready, err)
	}
	route.Cert.Status = "OK"
	ready, err = targetRouteCertificateReady(route, true)
	if err != nil || !ready {
		t.Fatalf("ready certificate = %v, %v", ready, err)
	}
	route.Cert.Issuer = "custom"
	if _, err := targetRouteCertificateReady(route, true); err == nil {
		t.Fatal("custom certificate issuer should not satisfy planned Let's Encrypt route")
	}
	instanceID, serviceID := 8, 9
	fullRoute := TargetAppRoute{
		ID: 3, Host: "app.example.com", AppInstanceID: instanceID,
		AppServiceID: serviceID, PortID: 10,
		Cert: &TargetCert{
			ID: 4, Issuer: "letsencrypt", Status: "OK",
			AppInstanceID: &instanceID, AppServiceID: &serviceID,
		},
	}
	if err := validateTargetAppRoute(fullRoute, instanceID); err != nil {
		t.Fatalf("valid route certificate relationship rejected: %v", err)
	}
	wrongServiceID := 99
	fullRoute.Cert.AppServiceID = &wrongServiceID
	if err := validateTargetAppRoute(fullRoute, instanceID); err == nil {
		t.Fatal("certificate attached to another service should be rejected")
	}
}

func TestMigrationExecutorRunsMinimalCustomerLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	appCreated := false
	deploymentID := 30
	app := TargetApp{
		ID: 10, Name: "demo", Title: "Demo", OrgID: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	instance := TargetAppInstance{
		ID: 20, Name: "prod", Title: "Production", Status: "OK",
		AppID: 10, ClusterID: 3, EnvID: 4, StackID: 5, StackRevID: 12,
		CreatedAt: now, UpdatedAt: now,
	}
	service := TargetAppService{
		ID: 21, Name: "nginx", Status: "OK", AppInstanceID: 20,
		ServiceRevID: 101, CreatedAt: now, UpdatedAt: now,
	}
	deployments := map[int]TargetAppDeployment{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			if appCreated {
				writeTargetExecutionJSON(t, w, []TargetApp{app})
			} else {
				writeTargetExecutionJSON(t, w, []TargetApp{})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, found := body["projectId"]; found {
				t.Fatalf("organization-owned app body contains projectId: %#v", body)
			}
			if body["ciIntegrationId"] != float64(0) {
				t.Fatalf("migration app must default to Wodby CI: %#v", body)
			}
			if body["deferInitialDeployment"] != true {
				t.Fatalf("migration app must defer the automatic initial deployment: %#v", body)
			}
			appCreated = true
			writeTargetExecutionJSON(t, w, app)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-instances":
			writeTargetExecutionJSON(t, w, []TargetAppInstance{instance})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-instances/20":
			writeTargetExecutionJSON(t, w, instance)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services":
			writeTargetExecutionJSON(t, w, []TargetAppService{service})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-ports":
			writeTargetExecutionJSON(t, w, []TargetAppPort{})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-routes":
			writeTargetExecutionJSON(t, w, []TargetAppRoute{})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-auths":
			writeTargetExecutionJSON(t, w, []TargetAppAuth{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-deployments":
			var body TargetCreateAppDeploymentInput
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Services) != 1 || body.Services[0].AppServiceID != service.ID ||
				body.Services[0].SkipPostDeployment != nil {
				t.Fatalf("deployment body = %#v", body)
			}
			deploymentID++
			item := TargetAppDeployment{
				ID: deploymentID, Status: "COMPLETED", AppInstanceID: instance.ID,
				CreatedAt: now, UpdatedAt: now,
				AppServiceDeployments: []TargetAppServiceDeployment{{
					ID: deploymentID + 100, Status: "COMPLETED",
					AppServiceID: service.ID, Force: true,
					CreatedAt: now, UpdatedAt: now,
				}},
			}
			deployments[item.ID] = item
			writeTargetExecutionJSON(t, w, item)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/app-deployments/"):
			idText := strings.TrimPrefix(r.URL.Path, "/v1/app-deployments/")
			id, err := strconv.Atoi(idText)
			if err != nil {
				t.Fatal(err)
			}
			item, ok := deployments[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			writeTargetExecutionJSON(t, w, item)
		default:
			t.Fatalf("unexpected target request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	sourceInstance := Instance{
		UUID: "instance-1", Name: "prod", Title: "Production", Type: "prod", Status: "ok",
		Stack:      Stack{UUID: "stack-1", Name: "drupal", Version: "1"},
		Properties: map[string]interface{}{"post_deploy": false},
	}
	export := Export{
		Schema:          ExportSchemaV2,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		SecretsIncluded: true,
		Apps: []AppExport{{
			App:       App{UUID: "app-1", Name: "demo", Title: "Demo", Type: "app", Status: "ok"},
			Instances: []Instance{sourceInstance},
		}},
	}
	configDigest, err := export.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{
			Kind: "app", ID: "app-1", Schema: ExportSchemaV2, ConfigDigest: configDigest,
		},
		Target: PlanTarget{
			OrgID: 1, ClusterID: 3,
			OrgOwnerOrAdminVerified: true, DiscoveryVerified: true,
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1", Name: "demo",
			Instances: []InstancePlan{{
				SourceUUID: "instance-1", Name: "prod", TargetEnvID: 4,
				Stack: StackPlan{Target: "drupal", TargetID: 5, TargetRevID: 12},
			}},
		}},
		Status: "target_scope_validated",
	}
	plan.PlanHash, err = plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedMigration{
		App: export.Apps[0],
		Instances: []PreparedInstance{{
			Source: sourceInstance,
			Stack:  TargetStack{ID: 5, Name: "drupal", RevID: 12, OrgID: 1},
			StackServices: []TargetStackServiceInspection{{
				StackService: TargetStackService{
					ID: 11, Name: "nginx", ServiceRevID: 101,
				},
			}},
			Services:          map[string]PreparedService{},
			Imports:           map[string]PreparedImport{},
			ImportByComponent: map[string]PreparedImport{},
			EffectiveState:    map[string]bool{"nginx": true},
		}},
	}
	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	progress := []string{}
	executor, err := NewMigrationExecutor(client, MigrationExecutorOptions{
		StatePath:        filepath.Join(t.TempDir(), "state.json"),
		PollInterval:     time.Millisecond,
		OperationTimeout: time.Second,
		Progress: func(message string) {
			progress = append(progress, message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := executor.Apply(ctx, export, plan, prepared); err != nil {
		t.Fatalf("apply: %v", err)
	}
	cluster := TargetCluster{
		ID: 3, OrgID: 1, Status: "OK", IPs: []string{"203.0.113.10"},
	}
	result, err := executor.Verify(ctx, export, plan, prepared, cluster)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.State.Status != MigrationStatusComplete || deploymentID != 31 {
		t.Fatalf("result status=%q deployments=%d", result.State.Status, deploymentID-30)
	}
	output := strings.Join(progress, "\n")
	for _, expected := range []string{
		`Creating target app "demo" with initial instance "prod"`,
		`Target app "demo" created (ID 10).`,
		`Initial target instance "prod" created (ID 20).`,
		`Service "nginx" (ID 21) is already enabled.`,
		`Launching target deployment for app instance ID 20...`,
		`Target deployment ID 31 completed.`,
		`skipping the second deployment`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("progress output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestApplyChecksDataReadinessBeforeTargetRequests(t *testing.T) {
	now := time.Unix(10_000, 0)
	export, prepared := refreshDataImportFixture()
	export.Source = &ExportSource{Kind: "instance", UUID: "instance-1"}
	export.Apps[0].Instances[0].Properties["maintenance_mode"] = false
	configDigest, err := export.MigrationConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{
			Kind: "instance", ID: "instance-1", Schema: ExportSchemaV2,
			ConfigDigest: configDigest,
		},
		Target: PlanTarget{
			OrgID: 1, ClusterID: 3,
			OrgOwnerOrAdminVerified: true, DiscoveryVerified: true,
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1", Name: "demo",
			Instances: []InstancePlan{{
				SourceUUID: "instance-1", Name: "prod",
			}},
		}},
		Status: "target_scope_validated",
	}
	plan.PlanHash, err = plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}

	targetRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		t.Fatalf("apply made target request before data preflight: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()
	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(client, MigrationExecutorOptions{
		StatePath:        filepath.Join(t.TempDir(), "state.json"),
		PollInterval:     time.Millisecond,
		OperationTimeout: time.Second,
		MaxBackupAge:     time.Hour,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Apply(context.Background(), export, plan, prepared)
	if err == nil || !strings.Contains(err.Error(), "apply preflight failed before target changes") ||
		!strings.Contains(err.Error(), "not in maintenance mode") {
		t.Fatalf("apply error = %v", err)
	}
	if targetRequests != 0 {
		t.Fatalf("target requests = %d, want 0", targetRequests)
	}
}

func TestWaitAppInstanceOKWaitsForImportFinalization(t *testing.T) {
	statuses := []string{"IMPORTING", "DEPLOYING", "OK"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/app-instances/20" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		status := statuses[requests]
		requests++
		writeTargetExecutionJSON(t, w, TargetAppInstance{
			ID: 20, Name: "dev", Status: status, AppID: 10,
			ClusterID: 3, EnvID: 4, StackID: 5, StackRevID: 6,
		})
	}))
	defer server.Close()

	progress := []string{}
	executor := &MigrationExecutor{
		target:           mustTargetExecutionClient(t, server.URL),
		pollInterval:     time.Millisecond,
		operationTimeout: time.Second,
		progress: func(message string) {
			progress = append(progress, message)
		},
	}
	if err := executor.waitAppInstanceOK(context.Background(), 20, "start the files data import"); err != nil {
		t.Fatal(err)
	}
	if requests != len(statuses) {
		t.Fatalf("status requests = %d, want %d", requests, len(statuses))
	}
	output := strings.Join(progress, "\n")
	for _, expected := range []string{
		`is "IMPORTING"; waiting until it is OK to start the files data import`,
		`is "DEPLOYING"; waiting until it is OK to start the files data import`,
		`is OK; continuing`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("progress output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestPlanHasCustomRoutes(t *testing.T) {
	technical := InstancePlan{Routes: []RoutePlan{{Action: "skip_technical"}}}
	if planHasCustomRoutes(&technical) {
		t.Fatal("technical routes should not trigger a second deployment")
	}
	custom := InstancePlan{Routes: []RoutePlan{{Action: "create_backend"}}}
	if !planHasCustomRoutes(&custom) {
		t.Fatal("custom routes should trigger a second deployment")
	}
}

func TestMatchingRoutesAccountsForDisabledStaging(t *testing.T) {
	plan := RoutePlan{Host: "example.com", Action: "create_backend", Primary: true}
	routes := []TargetAppRoute{
		{ID: 1, Host: "example.com", Path: "/", Action: TargetRouteActionBackend, AppServiceID: 10, PortID: 20, Main: true, Primary: true},
		{ID: 2, Host: "example.com", Path: "/", Action: TargetRouteActionBackend, AppServiceID: 10, PortID: 20, Disabled: true},
	}

	active := matchingRoutes(routes, 10, 20, plan, false)
	if len(active) != 1 || active[0].ID != 1 {
		t.Fatalf("active matches = %#v", active)
	}
	disabled := matchingRoutes(routes, 10, 20, plan, true)
	if len(disabled) != 1 || disabled[0].ID != 2 {
		t.Fatalf("disabled matches = %#v", disabled)
	}
}

func newExecutorTestState(t *testing.T) (*MigrationState, string) {
	t.Helper()
	identity := MigrationStateIdentity{
		Source: MigrationStateSourceIdentity{
			Kind:         "app",
			ID:           "app-1",
			ConfigDigest: strings.Repeat("a", 64),
		},
		PlanHash: strings.Repeat("b", 64),
		Target: MigrationStateTarget{
			OrgID:     1,
			ProjectID: 2,
			ClusterID: 3,
		},
	}
	state, err := NewMigrationState(identity, []string{"instance-1"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "migration-state.json")
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
	return state, path
}
