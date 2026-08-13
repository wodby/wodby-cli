package wodby1

import (
	"strings"
	"testing"
)

func prepareStackConfigurationTest(app PreparedAppMigration) (PreparedStackConfiguration, []ReviewItem, error) {
	return prepareStackConfiguration(&app)
}

func TestPrepareStackConfigurationScopesEnvironmentValues(t *testing.T) {
	app := stackConfigurationTestApp(
		stackConfigurationTestInstance("prod", "PROD", "production", "shared"),
		stackConfigurationTestInstance("dev", "DEV", "development", "shared"),
	)
	configuration, findings, err := prepareStackConfigurationTest(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	variables := configuration.Services["php"].EnvVars
	if len(variables) != 4 {
		t.Fatalf("env vars = %#v, want three source variables and the compatibility marker", variables)
	}
	assertPreparedStackEnvVar(t, variables, wodby1LegacyEnvVarsMarker, "true", nil)
	assertPreparedStackEnvVar(t, variables, "SHARED", "shared", nil)
	prod := "PROD"
	dev := "DEV"
	assertPreparedStackEnvVar(t, variables, "APP_MODE", "production", &prod)
	assertPreparedStackEnvVar(t, variables, "APP_MODE", "development", &dev)
}

func TestPrepareStackConfigurationCollapsesVariablePresentInEveryDevInstance(t *testing.T) {
	devA := stackConfigurationTestInstance("dev-a", "DEV", "development", "shared")
	devB := stackConfigurationTestInstance("dev-b", "DEV", "development", "shared")
	prod := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	devA.Source.Services[0].EnvVars = append(devA.Source.Services[0].EnvVars, EnvVar{Name: "DEV_ONLY", Value: "enabled", Enabled: true})
	devB.Source.Services[0].EnvVars = append(devB.Source.Services[0].EnvVars, EnvVar{Name: "DEV_ONLY", Value: "enabled", Enabled: true})

	configuration, findings, err := prepareStackConfigurationTest(stackConfigurationTestApp(devA, devB, prod))
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	dev := "DEV"
	assertPreparedStackEnvVar(t, configuration.Services["php"].EnvVars, "DEV_ONLY", "enabled", &dev)
	for _, variable := range configuration.Services["php"].EnvVars {
		if variable.Name == "DEV_ONLY" && variable.EnvType == nil {
			t.Fatalf("DEV_ONLY was incorrectly made global: %#v", configuration.Services["php"].EnvVars)
		}
	}
}

func TestPrepareStackConfigurationKeepsSameEnvironmentTypeDifferencesOnInstances(t *testing.T) {
	app := stackConfigurationTestApp(
		stackConfigurationTestInstance("dev-a", "DEV", "one", "shared"),
		stackConfigurationTestInstance("dev-b", "DEV", "two", "shared"),
	)
	configuration, findings, err := prepareStackConfiguration(&app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	for _, variable := range configuration.Services["php"].EnvVars {
		if variable.Name == "APP_MODE" {
			t.Fatalf("divergent APP_MODE was incorrectly promoted to the stack: %#v", configuration.Services["php"].EnvVars)
		}
	}
	for _, instance := range app.Instances {
		variables := instance.Services["php"].InstanceEnvVars
		if len(variables) != 1 || variables[0].Name != "APP_MODE" {
			t.Fatalf("instance %q overrides = %#v, want APP_MODE", instance.Source.Name, variables)
		}
	}
}

func TestPrepareStackConfigurationKeepsDifferentCronsOnInstances(t *testing.T) {
	left := stackConfigurationTestInstance("dev-a", "DEV", "development", "shared")
	right := stackConfigurationTestInstance("dev-b", "DEV", "development", "shared")
	left.Source.Services[0].CronJobs = []CronJob{{Title: "Drupal cron", Crontab: "*/5 * * * *", Command: "drush cron", Enabled: true}}
	right.Source.Services[0].CronJobs = []CronJob{{Title: "Drupal cron", Crontab: "*/15 * * * *", Command: "drush cron", Enabled: true}}
	leftService := left.Services["php"]
	leftService.Source = left.Source.Services[0]
	left.Services["php"] = leftService
	rightService := right.Services["php"]
	rightService.Source = right.Source.Services[0]
	right.Services["php"] = rightService
	app := stackConfigurationTestApp(left, right)

	configuration, findings, err := prepareStackConfiguration(&app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	for _, instance := range app.Instances {
		if schedules := instance.Services["php"].InstanceCronJobs; len(schedules) != 1 {
			t.Fatalf("instance %q schedules = %#v, want one override", instance.Source.Name, schedules)
		}
	}
	for _, schedule := range configuration.Services["php"].CronSchedules {
		if strings.HasPrefix(schedule.Name, "w1-") {
			t.Fatalf("divergent cron was incorrectly promoted to stack: %#v", schedule)
		}
	}
}

func TestPrepareStackConfigurationKeepsDifferentVersionsOnInstances(t *testing.T) {
	left := stackConfigurationTestInstance("dev", "DEV", "development", "shared")
	right := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	leftService := left.Services["php"]
	leftService.TargetVersion = "8.2"
	left.Services["php"] = leftService
	rightService := right.Services["php"]
	rightService.TargetVersion = "8.3"
	right.Services["php"] = rightService
	app := stackConfigurationTestApp(left, right)

	configuration, findings, err := prepareStackConfiguration(&app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if len(configuration.Services["php"].VersionOptions) != 0 {
		t.Fatalf("divergent versions changed shared stack options: %#v", configuration.Services["php"].VersionOptions)
	}
	if got := app.Instances[0].Services["php"].InstanceVersion; got != "8.2" {
		t.Fatalf("dev instance version = %q, want 8.2", got)
	}
	if got := app.Instances[1].Services["php"].InstanceVersion; got != "8.3" {
		t.Fatalf("prod instance version = %q, want 8.3", got)
	}
}

func TestPrepareStackConfigurationBlocksGlobalSettingDifferences(t *testing.T) {
	left := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	right := stackConfigurationTestInstance("dev", "DEV", "development", "shared")
	left.Source.Services[0].Configuration = map[string]interface{}{"memory": "256M"}
	right.Source.Services[0].Configuration = map[string]interface{}{"memory": "512M"}
	_, findings, err := prepareStackConfigurationTest(stackConfigurationTestApp(left, right))
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v, want blocking setting conflict", findings)
	}
}

func TestPrepareStackConfigurationSelectsOneSharedVersionAndDisablesDefaultCron(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	configuration, findings, err := prepareStackConfigurationTest(stackConfigurationTestApp(instance))
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

func TestPrepareStackConfigurationOnlyAppliesDerivativeSpecificEnvironmentOverrides(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	derivativeManifest := drupalPHPSettingManifest()
	derivativeManifest.Options = []TargetServiceOption{{Version: "8.3", Default: true}, {Version: "8.4"}}
	derivativeManifest.CronSchedules = []TargetServiceCronSchedule{{
		Name: "drupal-cron", Title: "Drupal cron", Schedule: "0 * * * *", Command: "drush cron",
	}}
	sshdSource := Service{
		Name: "sshd", Enabled: true,
		EnvVars: []EnvVar{{Name: "SSH_ONLY", Value: "yes", Enabled: true}},
	}
	sshdTarget := TargetStackServiceInspection{
		StackService: TargetStackService{ID: 20, Name: "sshd", Type: "ssh", ServiceRevID: 21},
		ServiceRevision: TargetServiceRevision{
			ID: 21, Name: "drupal-php", Type: "php", ServiceID: 1000, Manifest: derivativeManifest,
		},
	}
	instance.Source.Services = append(instance.Source.Services, sshdSource)
	instance.Services["sshd"] = PreparedService{Source: sshdSource, Target: sshdTarget, TargetVersion: "8.4"}
	instance.StackServices = append(instance.StackServices, sshdTarget)
	instance.EffectiveState = map[string]bool{"php": true, "sshd": true}

	configuration, findings, err := prepareStackConfigurationTest(stackConfigurationTestApp(instance))
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	sshd := configuration.Services["sshd"]
	if len(sshd.VersionOptions) != 0 || len(sshd.Settings) != 0 || len(sshd.CronSchedules) != 0 || len(sshd.Integrations) != 0 || len(sshd.Links) != 0 {
		t.Fatalf("derivative received parent-owned configuration: %#v", sshd)
	}
	assertPreparedStackEnvVar(t, sshd.EnvVars, "SSH_ONLY", "yes", nil)
	for _, variable := range sshd.EnvVars {
		if variable.Name == wodby1LegacyEnvVarsMarker {
			t.Fatalf("derivative received redundant compatibility marker: %#v", sshd.EnvVars)
		}
	}
}

func TestPrepareStackConfigurationMapsDrupalAppSettingsToPHP(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.EffectiveState = map[string]bool{"php": true}
	php := instance.Services["php"]
	instance.Services["php"] = php
	instance.StackServices = []TargetStackServiceInspection{php.Target}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("docroot/web")
	app.App.App.SiteName = stringPointer("customer.example")

	configuration, findings, err := prepareStackConfigurationTest(app)
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
	assertPreparedStackSettingMapping(t, configuration.Services["php"].SettingMappings, "docroot", "docroot/web", "set stack override")
	assertPreparedStackSettingMapping(t, configuration.Services["php"].SettingMappings, "sitedir", "customer.example", "set stack override")
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
	}, ServiceRevision: TargetServiceRevision{ID: 11, Manifest: drupalPHPSettingManifest()}}
	instance.StackServices = []TargetStackServiceInspection{php}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("web")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfigurationTest(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if settings := configuration.Services["php"].Settings; len(settings) != 0 {
		t.Fatalf("matching Drupal PHP defaults prepared unnecessary setting overrides: %#v", settings)
	}
	assertPreparedStackSettingMapping(t, configuration.Services["php"].SettingMappings, "docroot", "web", "already matches target")
	assertPreparedStackSettingMapping(t, configuration.Services["php"].SettingMappings, "sitedir", "default", "already matches target")
	if !hasReviewMessage(findings, SeverityMigration, `Drupal subdirectory "web" and site directory "default"`) {
		t.Fatalf("Drupal app setting migration detail missing: %#v", findings)
	}
}

func TestPrepareStackConfigurationUsesExistingStackSettingOverrideAsEffectiveValue(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.Source.Services = nil
	instance.Services = nil
	instance.EffectiveState = map[string]bool{"php": true}
	php := TargetStackServiceInspection{
		StackService: TargetStackService{
			ID: 10, Name: "php", ServiceRevID: 11,
			Settings: []TargetStackServiceSetting{{
				ID: 101, StackServiceID: 10, Name: "docroot", Value: "custom/web",
			}},
		},
		ServiceRevision: TargetServiceRevision{ID: 11, Manifest: drupalPHPSettingManifest()},
	}
	instance.StackServices = []TargetStackServiceInspection{php}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("custom/web")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfigurationTest(app)
	if err != nil {
		t.Fatal(err)
	}
	if hasBlockingFindings(findings) {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if settings := configuration.Services["php"].Settings; len(settings) != 0 {
		t.Fatalf("matching stack override prepared unnecessary setting overrides: %#v", settings)
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
	instance.Services["php"] = php
	instance.StackServices = []TargetStackServiceInspection{php.Target, gotenberg}
	instance.EffectiveState = map[string]bool{"php": true, "gotenberg": true}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("web")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfigurationTest(app)
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

	_, findings, err := prepareStackConfigurationTest(app)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(findings, SeverityBlocking, "does not include the raw app docroot and site directory") {
		t.Fatalf("missing export blocker = %#v", findings)
	}
}

func TestPrepareStackConfigurationBlocksMissingTargetDrupalSettingCapability(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.EffectiveState = map[string]bool{"php": true}
	php := instance.Services["php"]
	php.Target.ServiceRevision.Manifest.Settings = php.Target.ServiceRevision.Manifest.Settings[:1]
	instance.Services["php"] = php
	instance.StackServices = []TargetStackServiceInspection{php.Target}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("web")
	app.App.App.SiteName = stringPointer("default")

	_, findings, err := prepareStackConfigurationTest(app)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(findings, SeverityBlocking, `does not expose required setting "sitedir"`) {
		t.Fatalf("missing target setting blocker = %#v", findings)
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
	manifest := drupalPHPSettingManifest()
	manifest.Options = []TargetServiceOption{
		{Version: "8.2", Default: true},
		{Version: "8.3"},
	}
	manifest.CronSchedules = []TargetServiceCronSchedule{{
		Name: "drupal-cron", Title: "Drupal cron", Schedule: "*/5 * * * *", Command: "drush cron",
	}}
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

func drupalPHPSettingManifest() *TargetServiceManifest {
	return &TargetServiceManifest{
		Settings: []TargetServiceSettingCapability{
			{Name: "docroot", Default: "web"},
			{Name: "sitedir", Default: "default"},
		},
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

func assertPreparedStackSettingMapping(t *testing.T, mappings []PreparedStackSettingMapping, name, value, action string) {
	t.Helper()
	for _, mapping := range mappings {
		if mapping.Name == name {
			if mapping.Value != value || mapping.Action != action {
				t.Fatalf("setting mapping %s = %#v, want value %q and action %q", name, mapping, value, action)
			}
			return
		}
	}
	t.Fatalf("setting mapping %s not found in %#v", name, mappings)
}

func hasBlockingFindings(findings []ReviewItem) bool {
	for _, finding := range findings {
		if finding.Severity == SeverityBlocking {
			return true
		}
	}
	return false
}
