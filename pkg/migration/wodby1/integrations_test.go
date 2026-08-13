package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
