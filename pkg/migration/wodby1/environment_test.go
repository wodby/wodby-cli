package wodby1

import "testing"

func TestMigratedEnvironmentValueRewritesMailhogSMTPEndpoint(t *testing.T) {
	service := Service{EnvVars: []EnvVar{
		{Name: "SMTP_HOST", Value: "mailhog.source-instance-uuid", Enabled: true, Origin: "custom_stack"},
		{Name: "SMTP_PORT", Value: "25", Enabled: true, Origin: "custom_stack"},
		{Name: "SMTP_SECURE", Value: "false", Enabled: true, Origin: "custom_stack"},
	}}
	targets := map[string]string{"mailhog": "mailpit"}

	want := map[string]string{
		"SMTP_HOST":   "mailpit",
		"SMTP_PORT":   "1025",
		"SMTP_SECURE": "false",
	}
	for _, variable := range service.EnvVars {
		if got := migratedEnvironmentValue(service, variable, targets); got != want[variable.Name] {
			t.Fatalf("%s = %q, want %q", variable.Name, got, want[variable.Name])
		}
	}
	message, ok := smtpEndpointMigrationReview(service, nil, targets)
	if !ok || message != "SMTP endpoint will be rewritten from mailhog:25 to mailpit:1025" {
		t.Fatalf("SMTP review = %q, %t", message, ok)
	}
}

func TestMigratedEnvironmentValueDoesNotRewriteUnknownSMTPService(t *testing.T) {
	service := Service{EnvVars: []EnvVar{
		{Name: "SMTP_HOST", Value: "smtp.example.com", Enabled: true, Origin: "custom"},
		{Name: "SMTP_PORT", Value: "25", Enabled: true, Origin: "custom"},
	}}
	for _, variable := range service.EnvVars {
		if got := migratedEnvironmentValue(service, variable, map[string]string{"mailhog": "mailpit"}); got != variable.Value {
			t.Fatalf("%s unexpectedly rewritten to %q", variable.Name, got)
		}
	}
}

func TestMigratedEnvironmentReferencesUsesWodby2RuntimeNames(t *testing.T) {
	input := `drush -l ${WODBY_URL_PRIMARY} --uri=$WODBY_HOST_PRIMARY --env=${WODBY_ENVIRONMENT_NAME} --build=$APP_BUILD_NUM`
	want := `drush -l ${WODBY_PRIMARY_URL} --uri=$WODBY_PRIMARY_HOST --env=${WODBY_ENV_NAME} --build=$WODBY_BUILD_NUMBER`
	if got := migratedEnvironmentReferences(input); got != want {
		t.Fatalf("migrated references = %q, want %q", got, want)
	}
}

func TestWodby1GeneratedEnvironmentDefinitionsAreNotMigrated(t *testing.T) {
	for _, name := range []string{"WODBY_APP_NAME", "WODBY_INSTANCE_NAME", "WODBY_URL_PRIMARY", "HTTP_ROOT", "PHP_FPM_ENV_VARS"} {
		if sourceEnvVarRequiresMigration(nil, EnvVar{Name: name, Value: "source", Enabled: true, Origin: "custom_stack"}) {
			t.Fatalf("Wodby 1 generated environment variable %s must remain target-owned", name)
		}
	}
	if !sourceEnvVarRequiresMigration(nil, EnvVar{Name: "CUSTOM_URL", Value: "${WODBY_URL_PRIMARY}", Enabled: true, Origin: "custom_stack"}) {
		t.Fatal("customer variable referencing a Wodby runtime variable must be migrated")
	}
}
