package wodby1

import (
	"context"
	"testing"
)

func TestPreflightVanillaDefaultBoilerplate(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		boilerplates                        []TargetServiceBuildBoilerplate
		connect, repository, skip, disabled bool
		want                                string
		blocked                             bool
	}{
		{name: "marked default", connect: true, boilerplates: []TargetServiceBuildBoilerplate{{Name: "first"}, {Name: "starter", Default: true}}, want: "starter"},
		{name: "first fallback", boilerplates: []TargetServiceBuildBoilerplate{{Name: "first"}, {Name: "second"}}, want: "first"},
		{name: "app repository does not override vanilla", connect: true, repository: true, boilerplates: []TargetServiceBuildBoilerplate{{Name: "starter"}}, want: "starter"},
		{name: "missing boilerplate", connect: true, blocked: true},
		{name: "no build source", blocked: true},
		{name: "unnamed boilerplate", boilerplates: []TargetServiceBuildBoilerplate{{}}, blocked: true},
		{name: "disabled service", disabled: true, boilerplates: []TargetServiceBuildBoilerplate{{Name: "starter"}}, blocked: true},
		{name: "skip code", skip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			export := preflightFixtureExport(tc.repository)
			source := &export.Apps[0].Instances[0]
			source.Properties = map[string]interface{}{"deployment_type": "vanilla"}
			source.Services = []Service{{Name: "php", Enabled: !tc.disabled}}
			catalog := preflightSingleBuildCatalog("php", tc.disabled)
			catalog.revisions[101].Manifest.Build = &TargetServiceBuildCapability{Connect: tc.connect, Boilerplates: tc.boilerplates}
			api := newPreflightTargetAPI(t, catalog)
			options := preflightOwnerPlanOptions()
			if tc.repository {
				options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
			}
			plan := preflightBuildPlan(t, export, options)
			opts := TargetPreflightOptions{SkipData: true, SkipCode: tc.skip}
			prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, opts)
			if err != nil {
				t.Fatal(err)
			}
			if (plan.Summary.Blocking > 0) != tc.blocked {
				t.Fatalf("unexpected findings: %#v", plan.Review)
			}
			instance := prepared.Instances[0]
			if tc.blocked || tc.skip {
				if instance.BuildSource != nil {
					t.Fatalf("unexpected build source: %#v", instance.BuildSource)
				}
				return
			}
			build := instance.BuildSource
			if build == nil || build.Input.BuildSourceType != TargetBuildSourcePublic || build.Input.Boilerplate == nil || *build.Input.Boilerplate != tc.want {
				t.Fatalf("build source = %#v", build)
			}
			if !instance.UsesWodbyCI || instance.ExternalCIOnly || instance.CIIntegrationKey != "" {
				t.Fatalf("unexpected CI: %#v", instance)
			}
			if build.Input.IntegrationID != nil || build.Input.RemoteGitRepoID != nil || build.Input.GitRef != nil {
				t.Fatalf("unexpected Git overrides: %#v", build.Input)
			}
			if plan.Apps[0].Instances[0].BuildServiceID != 11 || plan.Apps[0].Instances[0].BuildServiceRevID != 101 {
				t.Fatal("missing build service pins")
			}
			// Repeating preflight must accept the reviewed selection without drift.
			if _, err := api.client.PreflightTarget(context.Background(), export, &plan, opts); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVanillaBoilerplatesFromRawManifest(t *testing.T) {
	manifest, err := decodeTargetServiceManifest(&TargetServiceManifest{
		Raw: `{"name":"php","build":{"connect":true,"boilerplates":[{"name":"plain"},{"name":"starter","default":true}]}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	build, findings := prepareBuildSource(App{Name: "example"}, Instance{
		Name: "prod", Properties: map[string]interface{}{"deployment_type": "vanilla"},
	}, nil, []TargetStackServiceInspection{{
		StackService:    TargetStackService{ID: 11, Name: "php"},
		ServiceRevision: TargetServiceRevision{Manifest: manifest},
	}}, map[string]bool{"php": true}, TargetPreflightOptions{})
	if build == nil || build.Input.Boilerplate == nil || *build.Input.Boilerplate != "starter" {
		t.Fatalf("raw manifest selection = %#v, findings = %#v", build, findings)
	}
}
