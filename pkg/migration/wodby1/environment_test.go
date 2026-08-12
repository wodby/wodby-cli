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
