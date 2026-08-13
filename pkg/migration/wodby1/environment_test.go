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

func TestMigratedEnvironmentValueRewritesGotenbergEndpoint(t *testing.T) {
	variable := EnvVar{Name: "GOTENBERG_ENDPOINT", Value: "https://old.example.com", Enabled: true}
	for _, targets := range []map[string]string{
		{"gotenberg": "gotenberg"},
		{"athenapdf": "gotenberg"},
	} {
		if got := migratedEnvironmentValue(Service{Name: "php"}, variable, targets); got != "http://gotenberg:3000" {
			t.Fatalf("GOTENBERG_ENDPOINT = %q", got)
		}
	}
}

func TestMigratedEnvironmentReferencesKeepsLegacyWodbyRuntimeNames(t *testing.T) {
	input := `drush -l ${WODBY_URL_PRIMARY} --uri=$WODBY_HOST_PRIMARY --env=${WODBY_ENVIRONMENT_NAME} --build=$APP_BUILD_NUM`
	want := `drush -l ${WODBY_URL_PRIMARY} --uri=$WODBY_HOST_PRIMARY --env=${WODBY_ENVIRONMENT_NAME} --build=$WODBY_BUILD_NUMBER`
	if got := migratedEnvironmentReferences(input); got != want {
		t.Fatalf("migrated references = %q, want %q", got, want)
	}
}

func TestWodby1GeneratedEnvironmentDefinitionsAreNotMigrated(t *testing.T) {
	for _, name := range []string{
		"WODBY_APP_NAME", "WODBY_APP_SUBSITE", "WODBY_APP_TYPE", "WODBY_APP_VERSION",
		"WODBY_DB_HOST", "WODBY_HOME", "WODBY_INSTANCE_NAME", "WODBY_NAMESPACE",
		"WODBY_URL_PRIMARY", "HTTP_ROOT", "PHP_FPM_ENV_VARS",
	} {
		if sourceEnvVarRequiresMigration(nil, EnvVar{Name: name, Value: "source", Enabled: true, Origin: "custom_stack"}) {
			t.Fatalf("Wodby 1 generated environment variable %s must remain target-owned", name)
		}
	}
	if !sourceEnvVarRequiresMigration(nil, EnvVar{Name: "CUSTOM_URL", Value: "${WODBY_URL_PRIMARY}", Enabled: true, Origin: "custom_stack"}) {
		t.Fatal("customer variable referencing a Wodby runtime variable must be migrated")
	}
}

func TestCustomerWodbyNamespaceVariablesAreBlockedByTargetReservation(t *testing.T) {
	for _, name := range []string{"WODBY", "WODBY2", "WODBY_CUSTOM"} {
		variable := EnvVar{Name: name, Value: "source", Enabled: true, Origin: "custom"}
		if !sourceEnvVarBlockedByTargetReservation(nil, variable) {
			t.Fatalf("customer environment variable %s must be reported as blocked", name)
		}
		if sourceEnvVarRequiresMigration(nil, variable) {
			t.Fatalf("customer environment variable %s must not be sent to Wodby 2", name)
		}
	}
	lowercase := EnvVar{Name: "wodby_custom", Value: "source", Enabled: true, Origin: "custom"}
	if sourceEnvVarBlockedByTargetReservation(nil, lowercase) || !sourceEnvVarRequiresMigration(nil, lowercase) {
		t.Fatal("lowercase name does not belong to Wodby 2's case-sensitive reserved namespace")
	}
}

func TestGeneratedAndManagedDefaultWodbyVariablesAreNotReportedAsCustomerBlockers(t *testing.T) {
	for _, variable := range []EnvVar{
		{Name: "WODBY_APP_NAME", Value: "source", Enabled: true, Origin: "custom_stack"},
		{Name: "WODBY_SERVICE_DEFAULT", Value: "source", Enabled: true, Origin: "default"},
		{Name: "WODBY_DISABLED", Value: "source", Enabled: false, Origin: "custom"},
	} {
		if sourceEnvVarBlockedByTargetReservation(nil, variable) {
			t.Fatalf("environment variable %#v must not be reported as a customer blocker", variable)
		}
	}
}
