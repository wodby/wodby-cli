package wodby1

import "testing"

func TestPrepareStackConfigurationScopesEnvironmentValues(t *testing.T) {
	app := stackConfigurationTestApp(
		stackConfigurationTestInstance("prod", "PROD", "production", "shared"),
		stackConfigurationTestInstance("dev", "DEV", "development", "shared"),
	)
	configuration, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	variables := configuration.Services["php"].EnvVars
	if len(variables) != 3 {
		t.Fatalf("env vars = %#v, want three", variables)
	}
	assertPreparedStackEnvVar(t, variables, "SHARED", "shared", nil)
	prod := "PROD"
	dev := "DEV"
	assertPreparedStackEnvVar(t, variables, "APP_MODE", "production", &prod)
	assertPreparedStackEnvVar(t, variables, "APP_MODE", "development", &dev)
}

func TestPrepareStackConfigurationBlocksSameEnvironmentTypeDifferences(t *testing.T) {
	app := stackConfigurationTestApp(
		stackConfigurationTestInstance("dev-a", "DEV", "one", "shared"),
		stackConfigurationTestInstance("dev-b", "DEV", "two", "shared"),
	)
	_, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v, want blocking env conflict", findings)
	}
}

func TestPrepareStackConfigurationBlocksGlobalSettingDifferences(t *testing.T) {
	left := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	right := stackConfigurationTestInstance("dev", "DEV", "development", "shared")
	left.Source.Services[0].Configuration = map[string]interface{}{"memory": "256M"}
	right.Source.Services[0].Configuration = map[string]interface{}{"memory": "512M"}
	_, findings, err := prepareStackConfiguration(stackConfigurationTestApp(left, right))
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v, want blocking setting conflict", findings)
	}
}

func TestPrepareStackConfigurationSelectsOneSharedVersionAndDisablesDefaultCron(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	configuration, findings, err := prepareStackConfiguration(stackConfigurationTestApp(instance))
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	service := configuration.Services["php"]
	if got := selectedStackServiceVersion(service.VersionOptions); got != "8.3" {
		t.Fatalf("selected version = %q, want 8.3", got)
	}
	foundDefault := false
	for _, cron := range service.CronSchedules {
		if cron.Name == "drupal-cron" {
			foundDefault = true
			if !cron.Disabled || cron.EnvType != nil {
				t.Fatalf("default cron = %#v, want disabled global override", cron)
			}
		}
	}
	if !foundDefault {
		t.Fatal("disabled default PHP cron override not prepared")
	}
}

func TestPrepareStackConfigurationMapsDrupalAppSettingsToPHP(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.EffectiveState = map[string]bool{"php": true}
	php := instance.Services["php"]
	php.Target.StackService.Settings = []TargetStackServiceSetting{
		{ID: 101, StackServiceID: 10, Name: "docroot", Value: "web"},
		{ID: 102, StackServiceID: 10, Name: "sitedir", Value: "default"},
	}
	instance.Services["php"] = php
	instance.StackServices = []TargetStackServiceInspection{php.Target}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("docroot/web")
	app.App.App.SiteName = stringPointer("customer.example")

	configuration, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	settings := configuration.Services["php"].Settings
	if settings["docroot"] != "docroot/web" || settings["sitedir"] != "customer.example" {
		t.Fatalf("Drupal PHP settings = %#v", settings)
	}
	if !hasReviewMessage(findings, SeverityMigration, `Drupal subdirectory "docroot/web" and site directory "customer.example"`) {
		t.Fatalf("Drupal app setting migration detail missing: %#v", findings)
	}
}

func TestPrepareStackConfigurationDoesNotRewriteMatchingDrupalAppSettings(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.Source.Services = nil
	instance.Services = nil
	instance.EffectiveState = map[string]bool{"php": true}
	php := TargetStackServiceInspection{StackService: TargetStackService{
		ID: 10, Name: "php", ServiceRevID: 11,
		Settings: []TargetStackServiceSetting{
			{ID: 101, StackServiceID: 10, Name: "docroot", Value: "web"},
			{ID: 102, StackServiceID: 10, Name: "sitedir", Value: "default"},
		},
	}}
	instance.StackServices = []TargetStackServiceInspection{php}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("web")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if stackConfigurationHasChanges(configuration) {
		t.Fatalf("matching Drupal PHP defaults prepared an unnecessary stack change: %#v", configuration)
	}
	if !hasReviewMessage(findings, SeverityMigration, `Drupal subdirectory "web" and site directory "default"`) {
		t.Fatalf("Drupal app setting migration detail missing: %#v", findings)
	}
}

func TestPrepareStackConfigurationAddsPrivateGotenbergEndpoint(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.Source.Services = append(instance.Source.Services, Service{Name: "athenapdf", Enabled: true})
	gotenberg := TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 20, Name: "gotenberg", ServiceRevID: 21},
		ServiceRevision: TargetServiceRevision{ID: 21, Name: "gotenberg"},
	}
	instance.Services["athenapdf"] = PreparedService{
		Source: instance.Source.Services[1],
		Target: gotenberg,
	}
	php := instance.Services["php"]
	php.Target.StackService.Settings = []TargetStackServiceSetting{
		{ID: 101, StackServiceID: 10, Name: "docroot", Value: "web"},
		{ID: 102, StackServiceID: 10, Name: "sitedir", Value: "default"},
	}
	instance.Services["php"] = php
	instance.StackServices = []TargetStackServiceInspection{php.Target, gotenberg}
	instance.EffectiveState = map[string]bool{"php": true, "gotenberg": true}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("web")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	assertPreparedStackEnvVar(t, configuration.Services["php"].EnvVars, "GOTENBERG_ENDPOINT", "http://gotenberg:3000", nil)
	if !hasReviewMessage(findings, SeverityMigration, "private in-cluster Gotenberg URL") {
		t.Fatalf("Gotenberg endpoint migration detail missing: %#v", findings)
	}
}

func TestPrepareStackConfigurationBlocksMissingDrupalAppSettingsExport(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.EffectiveState = map[string]bool{"php": true}
	app := stackConfigurationTestApp(instance)

	_, findings, err := prepareStackConfiguration(app)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(findings, SeverityBlocking, "does not include the raw app docroot and site directory") {
		t.Fatalf("missing export blocker = %#v", findings)
	}
}

func stackConfigurationTestApp(instances ...PreparedInstance) PreparedAppMigration {
	exported := make([]Instance, 0, len(instances))
	for _, instance := range instances {
		exported = append(exported, instance.Source)
	}
	return PreparedAppMigration{
		App:       AppExport{App: App{UUID: "app-1", Name: "example"}, Instances: exported},
		Instances: instances,
	}
}

func stackConfigurationTestInstance(id, envType, mode, shared string) PreparedInstance {
	manifest := &TargetServiceManifest{
		Options: []TargetServiceOption{
			{Version: "8.2", Default: true},
			{Version: "8.3"},
		},
		CronSchedules: []TargetServiceCronSchedule{{
			Name: "drupal-cron", Title: "Drupal cron", Schedule: "*/5 * * * *", Command: "drush cron",
		}},
	}
	inspection := TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 10, Name: "php", ServiceRevID: 11},
		ServiceRevision: TargetServiceRevision{ID: 11, Manifest: manifest},
	}
	source := Service{
		Name: "php", Enabled: true,
		EnvVars: []EnvVar{
			{Name: "APP_MODE", Value: mode, Enabled: true},
			{Name: "SHARED", Value: shared, Enabled: true},
		},
	}
	return PreparedInstance{
		Source: Instance{UUID: id, Name: id, Services: []Service{source}},
		Stack:  TargetStack{ID: 1, RevID: 2},
		Services: map[string]PreparedService{
			"php": {Source: source, Target: inspection, TargetVersion: "8.3"},
		},
		TargetEnvType: envType,
	}
}

func assertPreparedStackEnvVar(t *testing.T, variables []PreparedStackEnvVar, name, value string, envType *string) {
	t.Helper()
	for _, variable := range variables {
		if variable.Name == name && sameOptionalString(variable.EnvType, envType) {
			if variable.Value != value {
				t.Fatalf("%s value = %q, want %q", name, variable.Value, value)
			}
			return
		}
	}
	t.Fatalf("env var %s/%v not found in %#v", name, envType, variables)
}

func hasBlockingFindings(findings []ReviewItem) bool {
	for _, finding := range findings {
		if finding.Severity == SeverityBlocking {
			return true
		}
	}
	return false
}
