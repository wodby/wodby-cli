package wodby1

import "testing"

// The tests below migrate one instance while its sibling is read only for
// context. Shared stack values must still match what a whole-app migration
// would write, because the sibling later joins the same target app.

func TestPromoteSharedServiceCapacityKeepsOverrideWhenContextInstanceDiffers(t *testing.T) {
	migratedReplicas, contextReplicas := 2, 1
	app := PreparedAppMigration{
		Instances: []PreparedInstance{{
			Source: Instance{UUID: "prod", Services: []Service{{Name: "nginx"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &migratedReplicas},
			},
		}},
		ContextInstances: []PreparedInstance{{
			Source: Instance{UUID: "dev", Services: []Service{{Name: "nginx"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &contextReplicas},
			},
		}},
	}
	configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
	promoteSharedServiceCapacity(&app, &configuration)

	if service, exists := configuration.Services["nginx"]; exists && service.Replicas != nil {
		t.Fatalf("replicas promoted to the stack despite the context instance: %+v", service)
	}
	if replicas := app.Instances[0].Services["nginx"].Replicas; replicas == nil || *replicas != 2 {
		t.Fatalf("migrated instance lost its replica override: %v", replicas)
	}
}

func TestPromoteSharedServiceCapacityPromotesWhenContextInstanceAgrees(t *testing.T) {
	migratedReplicas, contextReplicas := 2, 2
	app := PreparedAppMigration{
		Instances: []PreparedInstance{{
			Source: Instance{UUID: "prod", Services: []Service{{Name: "nginx"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &migratedReplicas},
			},
		}},
		ContextInstances: []PreparedInstance{{
			Source: Instance{UUID: "dev", Services: []Service{{Name: "nginx"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &contextReplicas},
			},
		}},
	}
	configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
	promoteSharedServiceCapacity(&app, &configuration)

	if replicas := configuration.Services["nginx"].Replicas; replicas == nil || *replicas != 2 {
		t.Fatalf("shared replicas were not promoted: %v", replicas)
	}
	if replicas := app.Instances[0].Services["nginx"].Replicas; replicas != nil {
		t.Fatalf("promoted replicas remained on the migrated instance: %v", *replicas)
	}
	if replicas := app.ContextInstances[0].Services["nginx"].Replicas; replicas == nil || *replicas != 2 {
		t.Fatalf("context instance was modified: %v", replicas)
	}
}

func TestPrepareMailDeliveryLinksUsesInstanceScopeWhenContextInstanceDiffers(t *testing.T) {
	app := PreparedAppMigration{
		App:              AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances:        []PreparedInstance{mailDeliveryTestInstance("prod", "opensmtpd")},
		ContextInstances: []PreparedInstance{mailDeliveryTestInstance("dev", "mailhog")},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {Settings: map[string]string{}},
		}},
	}

	findings := prepareMailDeliveryLinks(&app)
	if hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v", findings)
	}
	if links := app.StackConfiguration.Services["php"].Links; len(links) != 0 {
		t.Fatalf("mail delivery should not be stack-wide: %#v", links)
	}
	links := app.Instances[0].ServiceLinks
	if len(links) != 1 || links[0].Name != "sendmail" || links[0].LinkedServiceName != "opensmtpd" {
		t.Fatalf("instance links = %#v", links)
	}
	if len(app.ContextInstances[0].ServiceLinks) != 0 {
		t.Fatalf("context instance received links: %#v", app.ContextInstances[0].ServiceLinks)
	}
}

func TestPrepareMailDeliveryLinksUsesStackScopeWhenContextInstanceAgrees(t *testing.T) {
	app := PreparedAppMigration{
		App:              AppExport{App: App{UUID: "app-1", Name: "demo"}},
		Instances:        []PreparedInstance{mailDeliveryTestInstance("prod", "opensmtpd")},
		ContextInstances: []PreparedInstance{mailDeliveryTestInstance("dev", "opensmtpd")},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {Settings: map[string]string{}},
		}},
	}

	findings := prepareMailDeliveryLinks(&app)
	if hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v", findings)
	}
	links := app.StackConfiguration.Services["php"].Links
	if len(links) != 1 || links[0].LinkedServiceName != "opensmtpd" {
		t.Fatalf("stack links = %#v", links)
	}
	if len(app.Instances[0].ServiceLinks) != 0 {
		t.Fatalf("instance links = %#v", app.Instances[0].ServiceLinks)
	}
}

func TestPrepareGotenbergEndpointScopesValueWhenContextInstanceHasNoGotenberg(t *testing.T) {
	prod := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	prod.Source.Stack = Stack{Name: "drupal11"}
	prod.Source.Services = append(prod.Source.Services, Service{Name: "athenapdf", Enabled: true})
	gotenberg := TargetStackServiceInspection{
		StackService:    TargetStackService{ID: 20, Name: "gotenberg", ServiceRevID: 21},
		ServiceRevision: TargetServiceRevision{ID: 21, Name: "gotenberg"},
	}
	prod.Services["athenapdf"] = PreparedService{Source: prod.Source.Services[1], Target: gotenberg}
	prod.StackServices = []TargetStackServiceInspection{prod.Services["php"].Target, gotenberg}
	prod.EffectiveState = map[string]bool{"php": true, "gotenberg": true}

	dev := stackConfigurationTestInstance("dev", "DEV", "development", "shared")
	dev.Source.Stack = Stack{Name: "drupal11"}
	dev.EffectiveState = map[string]bool{"php": true}

	app := stackConfigurationTestApp(prod)
	app.ContextInstances = []PreparedInstance{dev}
	configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
	findings := prepareGotenbergEndpoint(app, &configuration)
	if hasBlockingFindings(findings) {
		t.Fatalf("findings = %#v", findings)
	}
	prodScope := "PROD"
	variables := configuration.Services["php"].EnvVars
	if len(variables) != 1 {
		t.Fatalf("GOTENBERG_ENDPOINT variables = %#v, want one for production only", variables)
	}
	assertPreparedStackEnvVar(t, variables, "GOTENBERG_ENDPOINT", "http://gotenberg:3000", &prodScope)
}
