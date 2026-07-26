package wodby1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestTargetClientResolvesAndInspectsStackRevision(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/stacks":
			if r.URL.Query().Get("orgId") != "8" ||
				r.URL.Query().Get("projectIds") != "9" ||
				r.URL.Query().Get("search") != "acme/drupal11" {
				t.Fatalf("stack query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, TargetStacksResponse{
				Items: []TargetStack{{
					ID: 7, Name: "acme/drupal11", Status: "OK", RevID: 71, OrgID: 8,
				}},
				TotalCount: 1,
			})
		case "/v1/stack-revisions/71/services":
			writeTargetExecutionJSON(t, w, []TargetStackService{
				{ID: 12, Name: "mariadb", ServiceRevID: 102},
				{ID: 11, Name: "php", ServiceRevID: 101},
			})
		case "/v1/service-revisions/101":
			writeTargetExecutionJSON(t, w, TargetServiceRevision{
				ID: 101, ServiceID: 201, Name: "drupal11-php",
				Manifest: &TargetServiceManifest{
					Name:  "drupal11-php",
					Build: &TargetServiceBuildCapability{Connect: true},
				},
			})
		case "/v1/service-revisions/102":
			writeTargetExecutionJSON(t, w, TargetServiceRevision{
				ID: 102, ServiceID: 202, Name: "mariadb",
				Manifest: &TargetServiceManifest{
					Name: "mariadb",
					Imports: []TargetServiceImportCapability{{
						Name: "database", Extensions: []string{"gz", "tar.gz"},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	stack, err := client.ResolveStackRevisionByName(context.Background(), 8, 9, "acme/drupal11")
	if err != nil {
		t.Fatal(err)
	}
	if stack.RevID != 71 {
		t.Fatalf("stack = %#v", stack)
	}

	inspections, err := client.InspectStackRevision(context.Background(), stack.RevID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspections) != 2 || inspections[0].StackService.Name != "mariadb" ||
		inspections[0].ServiceRevision.Manifest == nil ||
		len(inspections[0].ServiceRevision.Manifest.Imports) != 1 ||
		inspections[1].StackService.Name != "php" ||
		inspections[1].ServiceRevision.Manifest == nil ||
		inspections[1].ServiceRevision.Manifest.Build == nil ||
		!inspections[1].ServiceRevision.Manifest.Build.Connect {
		t.Fatalf("inspections = %#v", inspections)
	}

	wantPaths := []string{
		"/v1/stacks",
		"/v1/stack-revisions/71/services",
		"/v1/service-revisions/102",
		"/v1/service-revisions/101",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
}

func TestResolveMigrationStackRevisionUsesExactOriginName(t *testing.T) {
	sourceOrigin := "drupal11"
	otherOrigin := "drupal10"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stacks" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("search"); got != sourceOrigin {
			t.Fatalf("stack search = %q, want %q", got, sourceOrigin)
		}
		writeTargetExecutionJSON(t, w, TargetStacksResponse{
			Items: []TargetStack{
				{
					ID: 7, Name: "acme/drupal11", Status: "OK", RevID: 71,
					OriginStackRevName: &sourceOrigin, OrgID: 8,
				},
				{
					ID: 8, Name: "acme/drupal11-legacy", Status: "OK", RevID: 81,
					OriginStackRevName: &otherOrigin, OrgID: 8,
				},
			},
			TotalCount: 2,
		})
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	stack, err := client.resolveMigrationStackRevision(
		context.Background(),
		8,
		9,
		StackPlan{Name: sourceOrigin, Target: sourceOrigin},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stack.ID != 7 || stack.Name != "acme/drupal11" {
		t.Fatalf("resolved stack = %#v", stack)
	}
}

func TestResolveMigrationStackRevisionPreservesExplicitName(t *testing.T) {
	sourceOrigin := "drupal11"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stacks" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("search"); got != "acme/selected" {
			t.Fatalf("stack search = %q, want exact explicit name", got)
		}
		writeTargetExecutionJSON(t, w, TargetStacksResponse{
			Items: []TargetStack{
				{
					ID: 17, Name: "acme/selected", Status: "OK", RevID: 171,
					OriginStackRevName: &sourceOrigin, OrgID: 8,
				},
				{
					ID: 18, Name: "acme/drupal11", Status: "OK", RevID: 181,
					OriginStackRevName: &sourceOrigin, OrgID: 8,
				},
			},
			TotalCount: 2,
		})
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	stack, err := client.resolveMigrationStackRevision(
		context.Background(),
		8,
		9,
		StackPlan{
			Name: sourceOrigin, Target: "acme/selected", ExplicitMapping: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stack.ID != 17 || stack.Name != "acme/selected" {
		t.Fatalf("resolved stack = %#v", stack)
	}
}

func TestResolveMigrationStackRevisionRequiresMappingForUnresolvedOrigin(t *testing.T) {
	sourceOrigin := "drupal11"
	otherOrigin := "drupal10"
	tests := []struct {
		name      string
		items     []TargetStack
		ambiguous bool
		want      string
	}{
		{
			name: "not found",
			items: []TargetStack{{
				ID: 7, Name: "acme/drupal11-old", Status: "OK", RevID: 71,
				OriginStackRevName: &otherOrigin, OrgID: 8,
			}},
			want: "--target-stack-map",
		},
		{
			name: "ambiguous",
			items: []TargetStack{
				{
					ID: 7, Name: "acme/drupal11", Status: "OK", RevID: 71,
					OriginStackRevName: &sourceOrigin, OrgID: 8,
				},
				{
					ID: 8, Name: "acme/drupal11-1", Status: "OK", RevID: 81,
					OriginStackRevName: &sourceOrigin, OrgID: 8,
				},
			},
			ambiguous: true,
			want:      "--target-stack-map",
		},
		{
			name: "wrong organization",
			items: []TargetStack{{
				ID: 7, Name: "acme/drupal11", Status: "OK", RevID: 71,
				OriginStackRevName: &sourceOrigin, OrgID: 99,
			}},
			want: "expected organization ID 8",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/stacks" {
					http.NotFound(w, r)
					return
				}
				writeTargetExecutionJSON(t, w, TargetStacksResponse{
					Items: test.items, TotalCount: len(test.items),
				})
			}))
			defer server.Close()

			client := mustTargetExecutionClient(t, server.URL)
			_, err := client.resolveMigrationStackRevision(
				context.Background(),
				8,
				9,
				StackPlan{Name: sourceOrigin, Target: sourceOrigin},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
			var ambiguous *TargetAmbiguousMatchError
			if got := errors.As(err, &ambiguous); got != test.ambiguous {
				t.Fatalf("ambiguous error = %t, want %t: %v", got, test.ambiguous, err)
			}
		})
	}
}

func TestFindStackServiceExactRejectsAmbiguity(t *testing.T) {
	_, found, err := FindStackServiceExact([]TargetStackService{
		{ID: 1, Name: "php"},
		{ID: 2, Name: "php"},
	}, "php")
	if found {
		t.Fatal("ambiguous match must not be returned")
	}
	var ambiguous *TargetAmbiguousMatchError
	if !errors.As(err, &ambiguous) || ambiguous.Count != 2 {
		t.Fatalf("err = %#v", err)
	}
}

func TestTargetClientCreatesAppAndInstanceWithServerDefaultsOmitted(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps":
			body := decodeTargetExecutionObject(t, r)
			assertTargetExecutionAbsent(t, body, "services", "domain")
			assertTargetExecutionNumber(t, body, "orgId", 8)
			assertTargetExecutionNumber(t, body, "projectId", 9)
			assertTargetExecutionNumber(t, body, "clusterId", 10)
			assertTargetExecutionNumber(t, body, "envId", 11)
			assertTargetExecutionNumber(t, body, "stackRevId", 12)
			writeTargetExecutionJSON(t, w, TargetApp{ID: 20, Name: "example", OrgID: 8})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			if r.URL.Query().Get("orgId") != "8" {
				t.Fatalf("apps query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetApp{{ID: 20, Name: "example", OrgID: 8}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-instances":
			body := decodeTargetExecutionObject(t, r)
			assertTargetExecutionAbsent(t, body, "services", "domain")
			assertTargetExecutionNumber(t, body, "appId", 20)
			writeTargetExecutionJSON(t, w, TargetAppInstance{
				ID: 21, Name: "stage", AppID: 20, ClusterID: 10, EnvID: 11,
				StackID: 5, StackRevID: 12,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-instances":
			if got := r.URL.Query(); got.Get("orgId") != "8" || got.Get("appId") != "20" {
				t.Fatalf("instances query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetAppInstance{{
				ID: 21, Name: "stage", AppID: 20, ClusterID: 10, EnvID: 11,
				StackID: 5, StackRevID: 12,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	app, err := client.CreateApp(context.Background(), TargetCreateAppInput{
		OrgID: 8, Name: "example", InstanceName: "prod", ProjectID: 9,
		StackRevID: 12, ClusterID: 10, EnvID: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != 20 {
		t.Fatalf("app = %#v", app)
	}

	instance, err := client.CreateAppInstance(context.Background(), TargetCreateAppInstanceInput{
		AppID: 20, InstanceName: "stage", StackRevID: 12, ClusterID: 10, EnvID: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID != 21 {
		t.Fatalf("instance = %#v", instance)
	}

	foundInstance, found, err := client.FindAppInstanceExact(context.Background(), 8, "example", "stage")
	if err != nil {
		t.Fatal(err)
	}
	if !found || foundInstance.ID != 21 {
		t.Fatalf("found = %v, instance = %#v", found, foundInstance)
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want 4", requests)
	}
}

func TestTargetClientFindAppExactRejectsAmbiguousResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps" || r.URL.Query().Get("orgId") != "5" {
			http.NotFound(w, r)
			return
		}
		writeTargetExecutionJSON(t, w, []TargetApp{
			{ID: 1, Name: "same", OrgID: 5},
			{ID: 2, Name: "same", OrgID: 5},
		})
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	_, found, err := client.FindAppExact(context.Background(), 5, "same")
	if found {
		t.Fatal("ambiguous app must not be returned")
	}
	var ambiguous *TargetAmbiguousMatchError
	if !errors.As(err, &ambiguous) || ambiguous.Resource != "app" {
		t.Fatalf("err = %#v", err)
	}
}

func TestTargetClientMutatesServiceConfigurationRoutesAuthAndImports(t *testing.T) {
	branch := TargetGitRefBranch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-services/30":
			body := decodeTargetExecutionObject(t, r)
			build := body["buildSource"].(map[string]any)
			if build["buildSourceType"] != TargetBuildSourceConnect ||
				build["remoteGitRepoId"] != "repo-17" ||
				build["gitRef"] != "main" ||
				build["gitRefType"] != TargetGitRefBranch {
				t.Fatalf("build source = %#v", build)
			}
			assertTargetExecutionNumber(t, build, "integrationId", 44)
			writeTargetExecutionJSON(t, w, TargetAppService{
				ID: 30, Name: "php", AppInstanceID: 20, ServiceRevID: 90,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/30/env-vars":
			body := decodeTargetExecutionObject(t, r)
			if body["name"] != "APP_ENV" || body["value"] != "prod" || body["secret"] != false {
				t.Fatalf("env body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceEnvVar{
				ID: 31, AppServiceID: 30, Name: "APP_ENV", Value: "prod",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-service-env-vars/31":
			body := decodeTargetExecutionObject(t, r)
			if body["value"] != "production" || body["secret"] != true {
				t.Fatalf("env update body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceEnvVar{
				ID: 31, AppServiceID: 30, Name: "APP_ENV", Value: "production",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-services/30/settings/docroot":
			body := decodeTargetExecutionObject(t, r)
			if body["value"] != "web" {
				t.Fatalf("service setting body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceSetting{
				ID: 41, AppServiceID: 30, Name: "docroot", Value: "web",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/30/cron-schedules":
			body := decodeTargetExecutionObject(t, r)
			if body["name"] != "w1-deadbeef" || body["crontab"] != "0 * * * *" {
				t.Fatalf("cron body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceCronSchedule{
				ID: 32, AppServiceID: 30, Name: "w1-deadbeef",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-service-cron-schedules/32":
			body := decodeTargetExecutionObject(t, r)
			if body["crontab"] != "15 * * * *" {
				t.Fatalf("cron update body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppServiceCronSchedule{
				ID: 32, AppServiceID: 30, Name: "w1-deadbeef", Crontab: "15 * * * *",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-routes":
			body := decodeTargetExecutionObject(t, r)
			assertTargetExecutionNumber(t, body, "appServiceId", 30)
			assertTargetExecutionNumber(t, body, "port", 98765)
			if body["host"] != "www.example.com" || body["action"] != TargetRouteActionBackend {
				t.Fatalf("route body = %#v", body)
			}
			assertTargetExecutionAbsent(t, body, "authLogin", "authPassword", "authId", "options")
			writeTargetExecutionJSON(t, w, TargetAppRoute{
				ID: 33, Host: "www.example.com", AppInstanceID: 20,
				AppServiceID: 30, PortID: 98765,
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-routes/33":
			body := decodeTargetExecutionObject(t, r)
			if body["action"] != TargetRouteActionRedirect ||
				body["redirectHost"] != "example.com" {
				t.Fatalf("route update body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppRoute{
				ID: 33, Host: "www.example.com", Action: TargetRouteActionRedirect,
				AppInstanceID: 20, AppServiceID: 30, PortID: 98765,
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-routes/33/settings/NO_INDEX":
			body := decodeTargetExecutionObject(t, r)
			if body["value"] != "true" {
				t.Fatalf("setting body = %#v", body)
			}
			writeTargetExecutionJSON(t, w, TargetAppRouteSetting{
				ID: 35, AppInstanceID: 20, RouteID: 33, Name: "NO_INDEX", Value: "true",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-auths":
			body := decodeTargetExecutionObject(t, r)
			assertTargetExecutionNumber(t, body, "appInstanceId", 20)
			assertTargetExecutionNumber(t, body, "appServiceId", 30)
			assertTargetExecutionNumber(t, body, "appRouteId", 33)
			if body["login"] != "ada" || body["password"] != "secret" {
				t.Fatalf("auth body = %#v", body)
			}
			routeID := 33
			serviceID := 30
			writeTargetExecutionJSON(t, w, TargetAppAuth{
				ID: 36, AppInstanceID: 20, AppServiceID: &serviceID,
				AppRouteID: &routeID, Login: "ada", Realm: "Restricted",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/app-auths/36":
			body := decodeTargetExecutionObject(t, r)
			if body["login"] != "grace" || body["realm"] != "Restricted" {
				t.Fatalf("auth update body = %#v", body)
			}
			assertTargetExecutionAbsent(t, body, "password")
			routeID := 33
			serviceID := 30
			writeTargetExecutionJSON(t, w, TargetAppAuth{
				ID: 36, AppInstanceID: 20, AppServiceID: &serviceID,
				AppRouteID: &routeID, Login: "grace", Realm: "Restricted",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/imports":
			body := decodeTargetExecutionObject(t, r)
			assertTargetExecutionNumber(t, body, "appServiceId", 30)
			importBody := body["import"].(map[string]any)
			if importBody["source"] != "URL" || importBody["importName"] != "database" ||
				importBody["url"] != "https://objects.example.test/db.sql.gz?signature=redacted" {
				t.Fatalf("import body = %#v", importBody)
			}
			taskID := 38
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true, TaskID: &taskID})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	integrationID := 44
	repoID := "repo-17"
	ref := "main"
	_, err := client.UpdateAppServiceBuildSource(context.Background(), 30, TargetBuildSourceInput{
		BuildSourceType: TargetBuildSourceConnect,
		IntegrationID:   &integrationID,
		RemoteGitRepoID: &repoID,
		GitRef:          &ref,
		GitRefType:      &branch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetAppServiceSetting(context.Background(), 30, "docroot", "web"); err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateAppServiceEnvVar(context.Background(), 30, TargetCreateAppServiceEnvVarInput{
		Name: "APP_ENV", Value: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedValue := "production"
	_, err = client.UpdateAppServiceEnvVar(context.Background(), 31, TargetUpdateAppServiceEnvVarInput{
		Value: &updatedValue, Secret: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cronName := "w1-deadbeef"
	_, err = client.CreateAppServiceCronSchedule(context.Background(), 30, TargetCreateAppServiceCronScheduleInput{
		Name: &cronName, Title: "Wodby 1 cron", Crontab: "0 * * * *", Command: "drush cron",
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedCrontab := "15 * * * *"
	_, err = client.UpdateAppServiceCronSchedule(context.Background(), 32, TargetUpdateAppServiceCronScheduleInput{
		Crontab: &updatedCrontab,
	})
	if err != nil {
		t.Fatal(err)
	}
	action := TargetRouteActionBackend
	route, err := client.CreateAppRoute(context.Background(), TargetCreateAppRouteInput{
		AppServiceID: 30, Port: 98765, Host: "www.example.com",
		Action: &action,
	})
	if err != nil {
		t.Fatal(err)
	}
	redirectAction := TargetRouteActionRedirect
	redirectHost := "example.com"
	_, err = client.UpdateAppRoute(context.Background(), route.ID, TargetUpdateAppRouteInput{
		Action: &redirectAction, RedirectHost: &redirectHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SetAppRouteSetting(context.Background(), route.ID, TargetRouteSettingNoIndex, "true")
	if err != nil {
		t.Fatal(err)
	}
	serviceID := 30
	routeID := route.ID
	auth, err := client.CreateAppAuth(context.Background(), TargetCreateAppAuthInput{
		AppInstanceID: 20, AppServiceID: &serviceID, AppRouteID: &routeID,
		Login: "ada", Password: "secret", Realm: "Restricted",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UpdateAppAuth(context.Background(), auth.ID, TargetUpdateAppAuthInput{
		AppServiceID: &serviceID, AppRouteID: &routeID, Login: "grace", Realm: "Restricted",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.StartURLImport(context.Background(), TargetStartURLImportInput{
		AppServiceID: 30,
		ImportName:   "database",
		URL:          "https://objects.example.test/db.sql.gz?signature=redacted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == nil || *result.TaskID != 38 {
		t.Fatalf("result = %#v", result)
	}
}

func TestTargetClientListsInstanceConfigurationResources(t *testing.T) {
	serviceID := 30
	routeID := 33
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-services":
			if r.URL.Query().Get("appInstanceId") != "20" {
				t.Fatalf("services query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetAppService{{
				ID: 30, Name: "php", AppInstanceID: 20, ServiceRevID: 90,
			}})
		case "/v1/app-ports":
			if r.URL.Query().Get("appInstanceId") != "20" {
				t.Fatalf("ports query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetAppPort{{
				ID: 34, Number: 8080, AppEndpointID: 91, AppInstanceID: 20, AppServiceID: 30,
			}})
		case "/v1/app-routes":
			if r.URL.Query().Get("appInstanceId") != "20" {
				t.Fatalf("routes query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetAppRoute{{
				ID: 33, Host: "example.com", AppInstanceID: 20, AppServiceID: 30, PortID: 34,
			}})
		case "/v1/app-routes/33/settings":
			writeTargetExecutionJSON(t, w, []TargetAppRouteSetting{{
				ID: 35, AppInstanceID: 20, RouteID: 33, Name: TargetRouteSettingNoIndex,
			}})
		case "/v1/app-auths":
			if r.URL.Query().Get("appInstanceId") != "20" {
				t.Fatalf("auth query = %q", r.URL.RawQuery)
			}
			writeTargetExecutionJSON(t, w, []TargetAppAuth{{
				ID: 36, AppInstanceID: 20, AppServiceID: &serviceID, AppRouteID: &routeID,
				Login: "ada", Realm: "Restricted",
			}})
		case "/v1/app-services/30/env-vars":
			writeTargetExecutionJSON(t, w, []TargetAppServiceEnvVar{{
				ID: 31, AppServiceID: 30, Name: "APP_ENV",
			}})
		case "/v1/app-services/30/settings":
			writeTargetExecutionJSON(t, w, []TargetAppServiceSetting{{
				ID: 41, AppServiceID: 30, Name: "docroot", Value: "web",
			}})
		case "/v1/app-services/30/cron-schedules":
			writeTargetExecutionJSON(t, w, []TargetAppServiceCronSchedule{{
				ID: 32, AppServiceID: 30, Name: "w1-deadbeef",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	if items, err := client.ListAppServices(context.Background(), 20); err != nil || len(items) != 1 {
		t.Fatalf("services = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppPorts(context.Background(), 20); err != nil || len(items) != 1 {
		t.Fatalf("ports = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppRoutes(context.Background(), 20); err != nil || len(items) != 1 {
		t.Fatalf("routes = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppRouteSettings(context.Background(), 33); err != nil || len(items) != 1 {
		t.Fatalf("settings = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppAuths(context.Background(), 20); err != nil || len(items) != 1 {
		t.Fatalf("auths = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppServiceEnvVars(context.Background(), 30); err != nil || len(items) != 1 {
		t.Fatalf("env vars = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppServiceSettings(context.Background(), 30); err != nil || len(items) != 1 {
		t.Fatalf("service settings = %#v, err = %v", items, err)
	}
	if items, err := client.ListAppServiceCronSchedules(context.Background(), 30); err != nil || len(items) != 1 {
		t.Fatalf("cron schedules = %#v, err = %v", items, err)
	}
}

func TestTargetClientListsImportWithExactRelationship(t *testing.T) {
	appInstanceID := 20
	appServiceID := 30
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/imports" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query(); got.Get("appInstanceId") != "20" || got.Get("appServiceId") != "30" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		taskID := 40
		writeTargetExecutionJSON(t, w, []TargetImport{{
			ID: 39, Name: "database", Source: "URL", Status: "COMPLETED",
			AppInstanceID: &appInstanceID, AppServiceID: &appServiceID, TaskID: &taskID,
		}})
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	items, err := client.ListImports(context.Background(), TargetImportFilters{
		AppInstanceID: &appInstanceID,
		AppServiceID:  &appServiceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != 39 {
		t.Fatalf("items = %#v", items)
	}
}

func TestTargetClientBuildAndDeploymentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-builds":
			body := decodeTargetExecutionObject(t, r)
			if !reflect.DeepEqual(body["appServiceIds"], []any{float64(30), float64(31)}) {
				t.Fatalf("build body = %#v", body)
			}
			taskID := 70
			writeTargetExecutionJSON(t, w, TargetAppBuildsCreateResponse{
				Items: []TargetAppBuild{
					{ID: 50, AppInstanceID: 20, AppServiceID: 30},
					{ID: 51, AppInstanceID: 20, AppServiceID: 31},
				},
				TaskID: &taskID,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-builds":
			assertTargetExecutionPageQuery(t, r.URL.Query(), "20", "2", "50")
			writeTargetExecutionJSON(t, w, TargetAppBuildsResponse{
				Items:      []TargetAppBuild{{ID: 50, AppInstanceID: 20, AppServiceID: 30}},
				TotalCount: 1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-deployments":
			body := decodeTargetExecutionObject(t, r)
			services := body["services"].([]any)
			if len(services) != 1 {
				t.Fatalf("deployment body = %#v", body)
			}
			service := services[0].(map[string]any)
			assertTargetExecutionNumber(t, service, "appServiceId", 30)
			assertTargetExecutionNumber(t, service, "appServiceBuildId", 60)
			if service["force"] != true {
				t.Fatalf("deployment service = %#v", service)
			}
			buildID := 60
			writeTargetExecutionJSON(t, w, TargetAppDeployment{
				ID: 80, AppInstanceID: 20,
				AppServiceDeployments: []TargetAppServiceDeployment{{
					ID: 81, AppServiceID: 30, AppServiceBuildID: &buildID,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-deployments":
			assertTargetExecutionPageQuery(t, r.URL.Query(), "20", "1", "25")
			writeTargetExecutionJSON(t, w, TargetAppDeploymentsResponse{
				Items:      []TargetAppDeployment{{ID: 80, AppInstanceID: 20}},
				TotalCount: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustTargetExecutionClient(t, server.URL)
	builds, err := client.CreateAppBuild(context.Background(), []int{31, 30, 31})
	if err != nil {
		t.Fatal(err)
	}
	if len(builds.Items) != 2 || builds.TaskID == nil || *builds.TaskID != 70 {
		t.Fatalf("builds = %#v", builds)
	}
	if _, err := client.ListAppBuilds(context.Background(), 20, TargetPageOptions{Page: 2, PageSize: 50}); err != nil {
		t.Fatal(err)
	}

	buildID := 60
	deployment, err := client.CreateAppDeployment(context.Background(), TargetCreateAppDeploymentInput{
		Services: []TargetAppServiceDeploymentInput{{
			AppServiceID: 30, AppServiceBuildID: &buildID, Force: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ID != 80 {
		t.Fatalf("deployment = %#v", deployment)
	}
	if _, err := client.ListAppDeployments(context.Background(), 20, TargetPageOptions{Page: 1, PageSize: 25}); err != nil {
		t.Fatal(err)
	}
}

func TestTargetExecutionRejectsInvalidInputsBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)

	tests := []struct {
		name string
		run  func() error
	}{
		{"invalid stack scope", func() error {
			_, err := client.ResolveStackRevisionByName(context.Background(), 0, 1, "acme/stack")
			return err
		}},
		{"invalid app organization", func() error {
			_, err := client.CreateApp(context.Background(), TargetCreateAppInput{})
			return err
		}},
		{"invalid instance app", func() error {
			_, err := client.CreateAppInstance(context.Background(), TargetCreateAppInstanceInput{})
			return err
		}},
		{"invalid service ID", func() error {
			_, err := client.ListAppServices(context.Background(), 0)
			return err
		}},
		{"empty service update", func() error {
			_, err := client.UpdateAppService(context.Background(), 1, TargetAppServiceUpdateInput{})
			return err
		}},
		{"invalid route port", func() error {
			_, err := client.CreateAppRoute(context.Background(), TargetCreateAppRouteInput{AppServiceID: 1, Host: "example.com"})
			return err
		}},
		{"unscoped import list", func() error {
			_, err := client.ListImports(context.Background(), TargetImportFilters{})
			return err
		}},
		{"insecure import URL", func() error {
			_, err := client.StartURLImport(context.Background(), TargetStartURLImportInput{
				AppServiceID: 1, ImportName: "database", URL: "http://objects.example.test/data",
			})
			return err
		}},
		{"empty build", func() error {
			_, err := client.CreateAppBuild(context.Background(), nil)
			return err
		}},
		{"empty deployment", func() error {
			_, err := client.CreateAppDeployment(context.Background(), TargetCreateAppDeploymentInput{})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid inputs triggered %d network requests", calls)
	}
}

func TestTargetExecutionRejectsWrongResponseRelationships(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/app-services":
			writeTargetExecutionJSON(t, w, []TargetAppService{{
				ID: 3, Name: "php", AppInstanceID: 99, ServiceRevID: 4,
			}})
		case "/v1/apps":
			writeTargetExecutionJSON(t, w, []TargetApp{{ID: 1, Name: "app", OrgID: 99}})
		case "/v1/app-routes":
			writeTargetExecutionJSON(t, w, TargetAppRoute{
				ID: 4, Host: "app.example.com", AppInstanceID: 20,
				AppServiceID: 3, PortID: 80,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)

	if _, err := client.ListAppServices(context.Background(), 20); err == nil ||
		!strings.Contains(err.Error(), "expected 20") {
		t.Fatalf("service relationship err = %v", err)
	}
	if _, _, err := client.FindAppExact(context.Background(), 8, "app"); err == nil ||
		!strings.Contains(err.Error(), "expected 8") {
		t.Fatalf("app relationship err = %v", err)
	}
	if _, err := client.CreateAppRoute(context.Background(), TargetCreateAppRouteInput{
		AppServiceID: 3,
		Port:         98765,
		Host:         "app.example.com",
	}); err == nil || !strings.Contains(err.Error(), "service/port/host") {
		t.Fatalf("route relationship err = %v", err)
	}
}

func mustTargetExecutionClient(t *testing.T, serverURL string) *TargetClient {
	t.Helper()
	client, err := NewTargetClient(types.APIConfig{Endpoint: serverURL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeTargetExecutionJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func decodeTargetExecutionObject(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertTargetExecutionAbsent(t *testing.T, object map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if _, ok := object[field]; ok {
			t.Fatalf("field %q must be omitted from %#v", field, object)
		}
	}
}

func assertTargetExecutionNumber(t *testing.T, object map[string]any, field string, want int) {
	t.Helper()
	if got, ok := object[field].(float64); !ok || int(got) != want {
		t.Fatalf("%s = %#v, want %d", field, object[field], want)
	}
}

func assertTargetExecutionPageQuery(t *testing.T, query url.Values, instance, page, pageSize string) {
	t.Helper()
	if query.Get("appInstanceId") != instance || query.Get("page") != page || query.Get("pageSize") != pageSize {
		t.Fatalf("query = %q", query.Encode())
	}
}
