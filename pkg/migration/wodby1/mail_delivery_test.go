package wodby1

import "testing"

func TestPrepareMailDeliveryLinksUsesStackScopeForSharedSelection(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances: []PreparedInstance{
			mailDeliveryTestInstance("prod", "opensmtpd"),
			mailDeliveryTestInstance("dev", "opensmtpd"),
		},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {Settings: map[string]string{}},
		}},
	}

	findings := prepareMailDeliveryLinks(&app)
	if hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v", findings)
	}
	links := app.StackConfiguration.Services["php"].Links
	if len(links) != 1 || links[0].Name != "sendmail" || links[0].LinkedServiceName != "opensmtpd" {
		t.Fatalf("stack links = %#v", links)
	}
	for _, instance := range app.Instances {
		if len(instance.ServiceLinks) != 0 {
			t.Fatalf("instance links = %#v", instance.ServiceLinks)
		}
	}
	if !hasReviewMessage(findings, SeverityMigration, "will be set stack-wide") {
		t.Fatalf("findings = %#v", findings)
	}
	if links := app.StackConfiguration.Services["sshd"].Links; len(links) != 0 {
		t.Fatalf("PHP SSH derivative received a direct mail link: %#v", links)
	}
	if hasReviewMessage(findings, SeverityMigration, `stack service "sshd" link "sendmail"`) {
		t.Fatalf("PHP SSH derivative link was reported separately: %#v", findings)
	}
}

func TestPrepareMailDeliveryLinksUsesInstanceScopeForDifferentSelections(t *testing.T) {
	app := PreparedAppMigration{
		App: AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances: []PreparedInstance{
			mailDeliveryTestInstance("prod", "opensmtpd"),
			mailDeliveryTestInstance("dev", "mailhog"),
		},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {Settings: map[string]string{}},
		}},
	}

	findings := prepareMailDeliveryLinks(&app)
	if hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v", findings)
	}
	if len(app.StackConfiguration.Services["php"].Links) != 0 {
		t.Fatalf("mail delivery should not be stack-wide: %#v", app.StackConfiguration.Services["php"].Links)
	}
	want := []string{"opensmtpd", "mailpit"}
	for index, instance := range app.Instances {
		if len(instance.ServiceLinks) != 1 || instance.ServiceLinks[0].ServiceName != "php" ||
			instance.ServiceLinks[0].Name != "sendmail" || instance.ServiceLinks[0].LinkedServiceName != want[index] {
			t.Fatalf("instance %d links = %#v", index, instance.ServiceLinks)
		}
	}
}

func TestPrepareMailDeliveryLinksRejectsUnsupportedSelection(t *testing.T) {
	instance := mailDeliveryTestInstance("prod", "opensmtpd")
	instance.Source.Properties["mail_service"] = "postfix"
	app := PreparedAppMigration{
		App:                AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances:          []PreparedInstance{instance},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}},
	}
	findings := prepareMailDeliveryLinks(&app)
	if !hasReviewMessage(findings, SeverityBlocking, "expected opensmtpd or mailhog") {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSelectedSourceMailServicePrefersMailServiceOverLegacyToggle(t *testing.T) {
	instance := mailDeliveryTestInstance("prod", "opensmtpd").Source
	instance.Properties["php_mail_catcher"] = true
	service, found, err := selectedSourceMailService(instance)
	if err != nil || !found || service != "opensmtpd" {
		t.Fatalf("service = %q, found = %t, err = %v", service, found, err)
	}
}

func TestSelectedSourceMailServiceUsesLegacyToggleAsFallback(t *testing.T) {
	instance := mailDeliveryTestInstance("prod", "opensmtpd").Source
	delete(instance.Properties, "mail_service")
	instance.Properties["php_mail_catcher"] = true
	service, found, err := selectedSourceMailService(instance)
	if err != nil || !found || service != "mailhog" {
		t.Fatalf("service = %q, found = %t, err = %v", service, found, err)
	}
}

func mailDeliveryTestInstance(name, selected string) PreparedInstance {
	phpManifest := &TargetServiceManifest{Links: []TargetServiceLinkCapability{{Name: "sendmail"}}}
	php := PreparedService{Target: TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 10, Name: "php", ServiceRevID: 100},
		ServiceRevision: TargetServiceRevision{ID: 100, ServiceID: 1000, Manifest: phpManifest},
	}}
	opensmtpd := PreparedService{Target: TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 11, Name: "opensmtpd", ServiceRevID: 101},
		ServiceRevision: TargetServiceRevision{ID: 101, ServiceID: 1001},
	}}
	mailpit := PreparedService{Target: TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 12, Name: "mailpit", ServiceRevID: 102},
		ServiceRevision: TargetServiceRevision{ID: 102, ServiceID: 1002},
	}}
	sshd := PreparedService{Target: TargetStackServiceInspection{
		StackService: TargetStackService{ID: 13, Name: "sshd", Type: "ssh", ServiceRevID: 100},
		ServiceRevision: TargetServiceRevision{
			ID: 100, Name: "drupal-php", Type: "php", ServiceID: 1000, Manifest: phpManifest,
		},
	}}
	return PreparedInstance{
		Source: Instance{
			UUID: name, Name: name, Stack: Stack{Name: "drupal11"},
			Properties: map[string]interface{}{"mail_service": selected},
			Services: []Service{
				{Name: "php", Enabled: true},
				{Name: "opensmtpd", Enabled: true},
				{Name: "mailhog", Enabled: true},
			},
		},
		Services: map[string]PreparedService{
			"php": php, "opensmtpd": opensmtpd, "mailhog": mailpit, "sshd": sshd,
		},
		EffectiveState: map[string]bool{"php": true, "opensmtpd": true, "mailpit": true, "sshd": true},
	}
}
