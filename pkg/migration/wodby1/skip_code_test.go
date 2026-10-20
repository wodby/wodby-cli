package wodby1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// skipCodeTestInstance is a --skip-code WordPress instance: php and nginx have
// builds and are left to the operator.
func skipCodeTestInstance() PreparedInstance {
	code := func(name string) TargetStackServiceInspection {
		return TargetStackServiceInspection{
			StackService: TargetStackService{Name: name},
			ServiceRevision: TargetServiceRevision{
				Manifest: &TargetServiceManifest{Build: &TargetServiceBuildCapability{Connect: true}},
			},
		}
	}
	return PreparedInstance{SkipCode: true, StackServices: []TargetStackServiceInspection{code("php"), code("nginx")}}
}

func skipCodeTestServices(codeStatus string) []TargetAppService {
	php := 20
	return []TargetAppService{
		{ID: 10, Name: "mariadb", Status: "OK"},
		{ID: 20, Name: "php", Status: codeStatus},
		{ID: 21, Name: "crond", Status: codeStatus, ParentAppServiceID: &php},
		{ID: 30, Name: "nginx", Status: codeStatus},
		{ID: 40, Name: "vinyl"},
		{ID: 50, Name: "wp-content-nfs", Type: "storage", Status: "OK"},
		{ID: 60, Name: "phpmyadmin"},
	}
}

// skipCodeTestTarget serves the links of skipCodeTestServices: php mounts the
// NFS storage, nginx proxies to php, Vinyl proxies to nginx, and phpMyAdmin
// uses the database.
func skipCodeTestTarget(t *testing.T) *TargetClient {
	t.Helper()
	links := map[int][]TargetAppServiceLink{
		20: {{ID: 1, AppServiceID: 20, LinkedAppServiceID: 10, Name: "db"}, {ID: 2, AppServiceID: 20, LinkedAppServiceID: 50, Name: "wp-content"}},
		30: {{ID: 3, AppServiceID: 30, LinkedAppServiceID: 20, Name: "backend"}},
		40: {{ID: 4, AppServiceID: 40, LinkedAppServiceID: 30, Name: "backend"}},
		60: {{ID: 5, AppServiceID: 60, LinkedAppServiceID: 10, Name: "db"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/app-services/"), "/links"))
		if r.Method != http.MethodGet || err != nil || !strings.HasSuffix(r.URL.Path, "/links") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		items := links[id]
		if items == nil {
			items = []TargetAppServiceLink{}
		}
		writeTargetExecutionJSON(t, w, items)
	}))
	t.Cleanup(server.Close)
	return mustTargetExecutionClient(t, server.URL)
}

// Vinyl cannot start without the nginx it proxies to, and cron runs php's
// code, so they wait with the code services until the operator deploys them.
func TestSkipCodeHoldsBackServicesThatNeedTheCode(t *testing.T) {
	executor := &MigrationExecutor{target: skipCodeTestTarget(t)}
	prepared := skipCodeTestInstance()
	for _, tc := range []struct {
		codeStatus string
		want       []int
	}{
		{codeStatus: "", want: []int{10, 50, 60}},
		{codeStatus: "OK", want: []int{10, 21, 40, 50, 60}},
	} {
		services := skipCodeTestServices(tc.codeStatus)
		waiting, _, err := executor.servicesWaitingForManualCode(context.Background(), prepared, services)
		if err != nil {
			t.Fatal(err)
		}
		input, err := technicalDeploymentInput(prepared, services, nil, waiting)
		if err != nil {
			t.Fatal(err)
		}
		got := []int{}
		for _, item := range input.Services {
			got = append(got, item.AppServiceID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("code status %q: deployed %v, want %v", tc.codeStatus, got, tc.want)
		}
	}
}

// The files import writes into the NFS volume php mounts, which Wodby 2
// creates only when php is deployed, so it waits for php. The database import
// does not wait.
func TestSkipCodeFilesImportWaitsForTheCodeThatMountsTheStorage(t *testing.T) {
	executor := &MigrationExecutor{target: skipCodeTestTarget(t)}
	prepared := skipCodeTestInstance()
	services := skipCodeTestServices("")
	code, blocked, err := executor.manualCodeImportBlocker(context.Background(), prepared, services, services[5])
	if err != nil || !blocked || code.ID != 20 {
		t.Fatalf("files import blocker = %+v, %v, %v; want php", code, blocked, err)
	}
	if _, blocked, err := executor.manualCodeImportBlocker(context.Background(), prepared, services, services[0]); err != nil || blocked {
		t.Fatalf("database import blocked = %v, %v", blocked, err)
	}
	deployed := skipCodeTestServices("OK")
	if _, blocked, err := executor.manualCodeImportBlocker(context.Background(), prepared, deployed, deployed[5]); err != nil || blocked {
		t.Fatalf("files import after the code deployment blocked = %v, %v", blocked, err)
	}
	withCode := PreparedInstance{}
	if _, blocked, err := executor.manualCodeImportBlocker(context.Background(), withCode, services, services[5]); err != nil || blocked {
		t.Fatalf("files import without --skip-code blocked = %v, %v", blocked, err)
	}
}

func TestManualCodeDeployPauseExplainsTheNextStep(t *testing.T) {
	action := &ExternalActionRequiredError{
		Instance: "prod", TargetInstanceID: 4100, ServiceName: "php", TargetServiceID: 4200,
		ManualCodeDeploy: true, ImportComponent: "files",
	}
	steps := action.NextSteps()
	for _, want := range []string{"--skip-code", `service "php" (app service ID 4200)`, "4100", "Rerun the same --apply command"} {
		if !strings.Contains(steps, want) {
			t.Fatalf("next steps missing %q:\n%s", want, steps)
		}
	}
	if strings.Contains(steps, "WODBY_API_KEY") || strings.Contains(action.Error(), "Custom CI") {
		t.Fatalf("manual code deployment pause talks about Custom CI:\n%s\n%v", steps, action)
	}
	paused := &MigrationPausedError{Actions: []*ExternalActionRequiredError{action}}
	if !paused.ManualCodeDeployOnly() {
		t.Fatal("pause should be a manual code deployment only")
	}
	paused.add(&ExternalActionRequiredError{Instance: "dev", ServiceName: "php", TargetServiceID: 4300})
	if paused.ManualCodeDeployOnly() {
		t.Fatal("pause with a Custom CI build is not a manual code deployment only")
	}
}
