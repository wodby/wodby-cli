package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSMTPProviderSelection(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{
			name: "brevo", want: "brevo",
			values: map[string]string{"RELAY_HOST": "smtp-relay.brevo.com", "RELAY_USER": "user", "RELAY_PASSWORD": "key"},
		},
		{
			name: "ses smtp credentials cannot become aws credentials", want: "custom-smtp",
			values: map[string]string{"RELAY_HOST": "email-smtp.eu-west-1.amazonaws.com", "RELAY_USER": "AKIA", "RELAY_PASSWORD": "derived"},
		},
		{
			name: "ses raw aws credentials", want: "aws",
			values: map[string]string{
				"RELAY_HOST":        "email-smtp.eu-west-1.amazonaws.com",
				"AWS_ACCESS_KEY_ID": "AKIA", "AWS_SECRET_ACCESS_KEY": "secret",
			},
		},
		{
			name: "custom", want: "custom-smtp",
			values: map[string]string{"RELAY_HOST": "smtp.example.com"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := smtpProvider(test.values); got != test.want {
				t.Fatalf("smtpProvider() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMigrationResourceNameIsStableAndBounded(t *testing.T) {
	first := migrationResourceName("smtp", "A Very Long Customer Application Name That Exceeds The Limit", "app-uuid")
	second := migrationResourceName("smtp", "A Very Long Customer Application Name That Exceeds The Limit", "app-uuid")
	if first != second || len(first) > 50 {
		t.Fatalf("migration resource names = %q and %q", first, second)
	}
	if first == migrationResourceName("smtp", "A Very Long Customer Application Name That Exceeds The Limit", "other-app-uuid") {
		t.Fatal("different source identities produced the same migration resource name")
	}
}

func TestExternalCIConfigurationWarningUsesDetectedProviderAndStack(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo", Type: "drupal11"}},
		Instances: []PreparedInstance{{Source: Instance{
			UUID: "prod-1", Name: "prod",
			Properties: map[string]interface{}{"deployment_type": "ci", "ci_provider": "circleci"},
			Stack:      Stack{Name: "drupal11"},
		}}},
	}
	findings := externalCIConfigurationFindings(app)
	if len(findings) != 1 || findings[0].Severity != SeverityServiceWarning {
		t.Fatalf("findings = %#v", findings)
	}
	// Wodby 2 has CircleCI, but not the API token behind it, so the warning has
	// to point at the integration the customer must bring.
	for _, want := range []string{
		"Wodby 1 uses CircleCI, which Wodby 2 supports",
		"API token cannot be migrated",
		"pass --target-ci-integration-id",
		"otherwise Custom CI is used",
		"WODBY_API_KEY and WODBY_APP_SERVICE_ID",
		"https://github.com/wodby/wodby-ci/blob/2.0/drupal/circleci/config.yml",
	} {
		if !strings.Contains(findings[0].Message, want) {
			t.Fatalf("warning missing %q: %s", want, findings[0].Message)
		}
	}
	if strings.Contains(findings[0].Message, "does not identify a supported CI provider") {
		t.Fatalf("a detected provider must not be reported as unidentified: %s", findings[0].Message)
	}
}

// Wodby 2 has no Bitbucket provider, so a Bitbucket app migrates as Custom CI
// and must not be told to create an integration Wodby 2 cannot offer.
func TestExternalCIConfigurationWarningTreatsUnsupportedProvidersAsCustomCI(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo", Type: "drupal11"}},
		Instances: []PreparedInstance{{Source: Instance{
			UUID: "prod-1", Name: "prod",
			Properties: map[string]interface{}{"deployment_type": "ci", "ci_provider": "bitbucket-pipelines"},
			Stack:      Stack{Name: "drupal11"},
		}}},
	}
	findings := externalCIConfigurationFindings(app)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	for _, want := range []string{
		"Wodby 1 uses Bitbucket Pipelines, which Wodby 2 does not support",
		"Custom CI is used",
		"wodby ci init --provider bitbucket-pipelines",
	} {
		if !strings.Contains(findings[0].Message, want) {
			t.Fatalf("warning missing %q: %s", want, findings[0].Message)
		}
	}
	if strings.Contains(findings[0].Message, "--target-ci-integration-id") {
		t.Fatalf("unsupported providers must not advertise an integration override: %s", findings[0].Message)
	}
}

func TestPrepareCIIntegrationDoesNotNameUnsupportedProviders(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "bitbucket-pipelines")

	integration, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{})
	if err != nil {
		t.Fatal(err)
	}
	// The created integration really is custom-ci; naming it "Bitbucket
	// Pipelines for ..." would imply a provider Wodby 2 does not have.
	if !strings.HasPrefix(integration.Name, "ci-example-app-") || integration.Title != "CI for Example App" {
		t.Fatalf("name = %q, title = %q", integration.Name, integration.Title)
	}
	if guidance := app.Instances[0].ExternalCI; guidance == nil ||
		guidance.ProviderLabel != "Bitbucket Pipelines" || guidance.ProviderSupported {
		t.Fatalf("guidance = %#v", app.Instances[0].ExternalCI)
	}
}

func TestExternalCIConfigurationWarningFallsBackToStackExamples(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo", Type: "wordpress"}},
		Instances: []PreparedInstance{{Source: Instance{
			UUID: "prod-1", Name: "prod",
			Properties: map[string]interface{}{"deployment_type": "ci"},
		}}},
	}
	findings := externalCIConfigurationFindings(app)
	if len(findings) != 1 ||
		!strings.Contains(findings[0].Message, "reports no provider") ||
		!strings.Contains(findings[0].Message, "Custom CI is used") ||
		!strings.Contains(findings[0].Message, "https://github.com/wodby/wodby-ci/tree/2.0/wordpress") {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestPrepareSMTPIntegrationUsesSESKindForAWS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/providers/by-name/aws" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(TargetProvider{ID: 7, RevID: 70, Name: "aws", Title: "AWS"})
	}))
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo", Title: "Demo"}},
		Instances: []PreparedInstance{{
			Source: Instance{UUID: "prod-1", Name: "prod", Services: []Service{{
				Name: "opensmtpd", Enabled: true, EnvVars: []EnvVar{
					{Name: "RELAY_HOST", Value: "email-smtp.eu-west-1.amazonaws.com", Enabled: true},
					{Name: "AWS_ACCESS_KEY_ID", Value: "AKIAEXAMPLE1234", Enabled: true},
					{Name: "AWS_SECRET_ACCESS_KEY", Value: "secret", Enabled: true, Secret: true},
				},
			}}},
			Services: map[string]PreparedService{"opensmtpd": {Target: TargetStackServiceInspection{StackService: TargetStackService{Name: "opensmtpd"}}}},
		}},
	}
	item, findings, err := client.prepareSMTPIntegration(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.ProviderName != "aws" || item.Kind != "ses" || item.Scope == nil || *item.Scope != "eu-west-1" {
		t.Fatalf("SMTP integration = %#v", item)
	}
	if len(findings) != 1 || findings[0].Severity != SeverityMigration {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestAggregateIntegrationFieldsScopesDifferentEnvironments(t *testing.T) {
	observations := []smtpRelayObservation{
		{instanceID: "prod", envType: "PROD", values: map[string]string{"RELAY_HOST": "smtp.example.com", "RELAY_USER": "prod"}},
		{instanceID: "dev", envType: "DEV", values: map[string]string{"RELAY_HOST": "smtp.example.com", "RELAY_USER": "dev"}},
	}
	fields, findings := aggregateIntegrationFields("app", "SMTP", observations, map[string]string{
		"host": "RELAY_HOST", "username": "RELAY_USER",
	})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
	want := []TargetIntegrationFieldInput{
		{Name: "host", Value: "smtp.example.com"},
		{Name: "username", Value: "dev", EnvType: stringPointer("DEV")},
		{Name: "username", Value: "prod", EnvType: stringPointer("PROD")},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %#v, want %#v", fields, want)
	}
}

func TestPrepareBackupIntegrationsUsesWodbyBlobPlaceholderAndAWSProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/providers/by-name/aws" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(TargetProvider{ID: 7, RevID: 70, Name: "aws", Title: "AWS"})
	}))
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo", Title: "Demo"}},
		Instances: []PreparedInstance{
			{Source: Instance{UUID: "dev-1", Name: "dev", BackupConfig: &BackupConfig{Enabled: true, Provider: "wodby"}}},
			{Source: Instance{UUID: "prod-1", Name: "prod", BackupConfig: &BackupConfig{
				Enabled: true, Provider: "aws_s3", Region: "eu-west-1", Bucket: "backups",
				AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret", Secret: true,
			}}},
		},
	}
	items, findings, err := client.prepareBackupIntegrations(context.Background(), &app, PlanTarget{
		OrgDefaultTimeZone: "Europe/Paris", OrgCapabilities: &TargetOrgCapabilities{AutoBackups: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProviderName != "aws" || items[0].Kind != "s3" || items[0].Scope == nil || *items[0].Scope != "eu-west-1" {
		t.Fatalf("integrations = %#v", items)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %#v", findings)
	}
	if got := app.Instances[0].BackupDestination; got == nil || got.IntegrationID != 0 || !got.Auto || got.TimeZone != "Europe/Paris" {
		t.Fatalf("Wodby Blob destination = %#v", got)
	}
	if got := app.Instances[1].BackupDestination; got == nil || got.IntegrationKey == "" || got.Bucket != "backups" || !got.Auto {
		t.Fatalf("AWS destination = %#v", got)
	}
}

func TestPrepareBackupIntegrationsCreatesDisabledWodbyBlobPresetOnFreePlan(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances: []PreparedInstance{{Source: Instance{
			UUID: "dev-1", Name: "dev", BackupConfig: &BackupConfig{Provider: "wodby"},
		}}},
	}
	client := &TargetClient{}
	items, findings, err := client.prepareBackupIntegrations(context.Background(), &app, PlanTarget{})
	if err != nil || len(items) != 0 || len(findings) != 2 {
		t.Fatalf("items = %#v, findings = %#v, err = %v", items, findings, err)
	}
	got := app.Instances[0].BackupDestination
	if got == nil || !got.Auto || !got.Disabled || got.IntegrationID != 0 || got.TimeZone != "UTC" {
		t.Fatalf("destination = %#v", got)
	}
}

func TestPrepareBackupIntegrationsDefaultsMissingMirrorToWodbyBlob(t *testing.T) {
	tests := []struct {
		name         string
		config       *BackupConfig
		autoBackups  bool
		wantAuto     bool
		wantDisabled bool
		severity     string
		message      string
	}{
		{
			name: "automatic backups on paid plan", config: &BackupConfig{Enabled: true}, autoBackups: true,
			wantAuto: true, severity: SeverityMigration,
			message: "Wodby 1 automatic backups are enabled, so enabled automatic-backup presets will be created",
		},
		{
			name: "manual backups on paid plan", config: &BackupConfig{Enabled: false}, autoBackups: true,
			severity: SeverityMigration,
			message:  "Wodby 1 automatic backups are not enabled, so manual backup presets will be created",
		},
		{
			name: "automatic backups on free plan", config: &BackupConfig{Enabled: true}, autoBackups: false,
			wantAuto: true, wantDisabled: true, severity: SeverityConfirmation,
			message: "Wodby 1 automatic backups are enabled, but the target subscription does not allow them",
		},
		{
			name: "missing legacy config on free plan", config: nil, autoBackups: false,
			wantAuto: true, wantDisabled: true, severity: SeverityConfirmation,
			message: "source export does not report whether automatic backups are enabled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := PreparedAppMigration{
				App: AppExport{App: App{UUID: "app-1", Name: "demo"}},
				Instances: []PreparedInstance{{Source: Instance{
					UUID: "dev-1", Name: "dev", BackupConfig: test.config,
				}}},
			}
			client := &TargetClient{}
			items, findings, err := client.prepareBackupIntegrations(context.Background(), &app, PlanTarget{
				OrgCapabilities: &TargetOrgCapabilities{AutoBackups: test.autoBackups},
			})
			if err != nil || len(items) != 0 || len(findings) != 2 {
				t.Fatalf("items = %#v, findings = %#v, err = %v", items, findings, err)
			}
			destination := app.Instances[0].BackupDestination
			if destination == nil || destination.IntegrationID != wodbyBlobIntegrationID ||
				destination.Auto != test.wantAuto || destination.Disabled != test.wantDisabled {
				t.Fatalf("destination = %#v", destination)
			}
			if !hasReviewMessage(findings, SeverityMigration, "target backup presets will use Wodby Blob storage") ||
				!hasReviewMessage(findings, test.severity, test.message) {
				t.Fatalf("findings = %#v", findings)
			}
		})
	}
}

func TestTargetBackupCapabilitiesIncludeEnabledDatabaseAndFiles(t *testing.T) {
	instance := PreparedInstance{
		EffectiveState: map[string]bool{"mariadb": true, "files-nfs": true, "disabled": false},
		StackServices: []TargetStackServiceInspection{
			backupInspection("mariadb", "database"),
			backupInspection("files-nfs", "files"),
			backupInspection("disabled", "other"),
		},
	}
	want := []preparedBackupCapability{{serviceName: "files-nfs", backupName: "files"}, {serviceName: "mariadb", backupName: "database"}}
	if got := targetBackupCapabilities(instance); !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}
}

func TestPrepareSharedVariableIntegrationsMovesCommonBundleOutOfPerAppStacks(t *testing.T) {
	shared := []PreparedStackEnvVar{
		{Name: "API_ENDPOINT", Value: "https://api.example.com"},
		{Name: "API_TOKEN", Value: "secret", Secret: true},
	}
	prepared := PreparedMigration{Apps: []PreparedAppMigration{
		sharedVariableTestApp("app-1", "one", shared),
		sharedVariableTestApp("app-2", "two", shared),
	}}
	plan := Plan{
		Source: PlanSource{Kind: "server", ID: "server-1"},
		Apps:   []AppPlan{{SourceUUID: "app-1"}, {SourceUUID: "app-2"}},
	}
	findings, err := prepareSharedVariableIntegrations(&prepared, &plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != SeverityMigration {
		t.Fatalf("findings = %#v", findings)
	}
	for index, app := range prepared.Apps {
		configuration := app.StackConfiguration.Services["php"]
		if len(configuration.EnvVars) != 0 || len(configuration.Integrations) != 1 || configuration.Integrations[0].Name != "variable" {
			t.Fatalf("app %d configuration = %#v", index, configuration)
		}
		if len(app.Integrations) != 1 || app.Integrations[0].VariableProvider == nil || len(app.Integrations[0].VariableProvider.Fields) != 2 {
			t.Fatalf("app %d integrations = %#v", index, app.Integrations)
		}
		if len(plan.Apps[index].Integrations) != 1 || !reflect.DeepEqual(plan.Apps[index].Integrations[0].Variables, []string{"API_ENDPOINT", "API_TOKEN"}) {
			t.Fatalf("app %d plan integrations = %#v", index, plan.Apps[index].Integrations)
		}
		if plan.Apps[index].Integrations[0].ProviderName != plan.Apps[0].Integrations[0].ProviderName {
			t.Fatal("shared apps must use the same deterministic variable provider")
		}
	}
}

func sharedVariableTestApp(uuid, name string, envVars []PreparedStackEnvVar) PreparedAppMigration {
	inspection := TargetStackServiceInspection{
		StackService: TargetStackService{Name: "php"},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Integrations: []TargetServiceIntegrationCapability{{Name: "variable", Type: "variable"}},
		}},
	}
	return PreparedAppMigration{
		App:       AppExport{App: App{UUID: uuid, Name: name}},
		Instances: []PreparedInstance{{StackServices: []TargetStackServiceInspection{inspection}}},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {EnvVars: append([]PreparedStackEnvVar(nil), envVars...)},
		}},
	}
}

func backupInspection(serviceName, backupName string) TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService: TargetStackService{Name: serviceName},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Backups: []TargetServiceBackupCapability{{Name: backupName}},
		}},
	}
}

func stringPointer(value string) *string { return &value }

func customCIIntegrationTestClient(t *testing.T) *TargetClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/providers/by-name/custom-ci" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(TargetProvider{ID: 501, RevID: 502, Name: "custom-ci", Title: "Custom CI"})
	}))
	t.Cleanup(server.Close)
	return mustTargetExecutionClient(t, server.URL)
}

func externalCITestApp(t *testing.T, providers ...string) *PreparedAppMigration {
	t.Helper()
	app := &PreparedAppMigration{
		App:       AppExport{App: App{UUID: "app-1", Name: "example-app", Title: "Example App", Type: "drupal11"}},
		Instances: make([]PreparedInstance, 0, len(providers)),
	}
	for index, provider := range providers {
		properties := map[string]interface{}{"deployment_type": "ci"}
		if provider != "" {
			properties["ci_provider"] = provider
		}
		app.Instances = append(app.Instances, PreparedInstance{Source: Instance{
			UUID:       "instance-" + string(rune('a'+index)),
			Name:       "instance-" + string(rune('a'+index)),
			Properties: properties,
			Stack:      Stack{Name: "drupal11"},
		}})
	}
	return app
}

func TestPrepareCIIntegrationNamesItAfterTheWodby1Provider(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "github")

	integration, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if integration == nil {
		t.Fatal("external CI app must prepare a CI integration")
	}
	// Wodby 2 still backs it with custom-ci; only the identity is provider-named.
	if integration.ProviderName != "custom-ci" {
		t.Fatalf("provider = %q, want custom-ci", integration.ProviderName)
	}
	if !strings.HasPrefix(integration.Name, "ci-github-") {
		t.Fatalf("name = %q, want a github-prefixed name", integration.Name)
	}
	if integration.Title != "GitHub Actions for Example App" {
		t.Fatalf("title = %q", integration.Title)
	}
}

func TestPrepareCIIntegrationFallsBackWhenProvidersDisagree(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "github", "gitlab")

	integration, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(integration.Name, "ci-example-app-") || integration.Title != "CI for Example App" {
		t.Fatalf("conflicting providers must stay generic: name = %q, title = %q", integration.Name, integration.Title)
	}
}

func TestPrepareCIIntegrationIgnoresInstancesWithoutAReportedProvider(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "circleci", "")

	integration, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(integration.Name, "ci-circleci-") || integration.Title != "CircleCI for Example App" {
		t.Fatalf("name = %q, title = %q", integration.Name, integration.Title)
	}
}

func TestPrepareCIIntegrationStaysGenericWithoutAnyProvider(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "")

	integration, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(integration.Name, "ci-example-app-") || integration.Title != "CI for Example App" {
		t.Fatalf("name = %q, title = %q", integration.Name, integration.Title)
	}
}

func TestPrepareCIIntegrationRecordsBootstrapGuidancePerInstance(t *testing.T) {
	client := customCIIntegrationTestClient(t)
	app := externalCITestApp(t, "github")

	if _, _, err := client.prepareCIIntegration(context.Background(), app, PlanTarget{}); err != nil {
		t.Fatal(err)
	}
	guidance := app.Instances[0].ExternalCI
	if guidance == nil {
		t.Fatal("external CI instances must carry bootstrap guidance into the executor")
	}
	if guidance.ProviderLabel != "GitHub Actions" || !guidance.ProviderSupported ||
		guidance.ExampleURL != "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml" {
		t.Fatalf("guidance = %#v", guidance)
	}
}
