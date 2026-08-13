package wodby1

import (
	"encoding/json"
	"testing"
)

func TestSourceServiceCapacityUsesNormalizedExportAndLegacyFallback(t *testing.T) {
	replicas := 3
	requestCPU, limitCPU, requestMem, limitMem := 250, 500, 256, 512
	gotReplicas, gotResources, err := sourceServiceCapacity(Service{
		Replicas: &replicas,
		Resources: &ServiceResources{
			RequestCPU: &requestCPU, LimitCPU: &limitCPU,
			RequestMem: &requestMem, LimitMem: &limitMem,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotReplicas == nil || *gotReplicas != replicas || gotResources == nil ||
		*gotResources.RequestCPU != requestCPU || *gotResources.LimitCPU != limitCPU ||
		*gotResources.RequestMem != requestMem || *gotResources.LimitMem != limitMem {
		t.Fatalf("unexpected normalized capacity: replicas=%v resources=%+v", gotReplicas, gotResources)
	}

	gotReplicas, gotResources, err = sourceServiceCapacity(Service{Configuration: map[string]interface{}{
		"deployment": map[string]interface{}{"replicas": json.Number("2")},
		"resources": map[string]interface{}{
			"cpu":    map[string]interface{}{"request": json.Number("0.25"), "limit": json.Number("0.5")},
			"memory": map[string]interface{}{"request": json.Number("128"), "limit": json.Number("256")},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if gotReplicas == nil || *gotReplicas != 2 || gotResources == nil ||
		*gotResources.RequestCPU != 250 || *gotResources.LimitCPU != 500 ||
		*gotResources.RequestMem != 128 || *gotResources.LimitMem != 256 {
		t.Fatalf("unexpected legacy capacity: replicas=%v resources=%+v", gotReplicas, gotResources)
	}
}

func TestPromoteSharedServiceCapacityUsesStackAndKeepsDriftOnInstances(t *testing.T) {
	sharedReplicas := 2
	devReplicas, prodReplicas := 1, 3
	requestCPU := 250
	shared := &PreparedServiceResources{Workload: "app", Container: "app", RequestCPU: &requestCPU}
	app := PreparedAppMigration{Instances: []PreparedInstance{
		{
			Source: Instance{UUID: "dev", Services: []Service{{Name: "nginx"}, {Name: "php"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &sharedReplicas, Resources: clonePreparedServiceResources(shared)},
				"php":   {Target: capacityTestTarget("php"), Replicas: &devReplicas},
			},
		},
		{
			Source: Instance{UUID: "prod", Services: []Service{{Name: "nginx"}, {Name: "php"}}},
			Services: map[string]PreparedService{
				"nginx": {Target: capacityTestTarget("nginx"), Replicas: &sharedReplicas, Resources: clonePreparedServiceResources(shared)},
				"php":   {Target: capacityTestTarget("php"), Replicas: &prodReplicas},
			},
		},
	}}
	configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
	promoteSharedServiceCapacity(&app, &configuration)

	nginx := configuration.Services["nginx"]
	if nginx.Replicas == nil || *nginx.Replicas != 2 || !preparedServiceResourcesEqual(nginx.Resources, shared) {
		t.Fatalf("shared nginx capacity not promoted: %+v", nginx)
	}
	for _, instance := range app.Instances {
		if service := instance.Services["nginx"]; service.Replicas != nil || service.Resources != nil {
			t.Fatalf("promoted nginx capacity remained on instance: %+v", service)
		}
	}
	if _, exists := configuration.Services["php"]; exists {
		t.Fatalf("drifting php replicas unexpectedly promoted: %+v", configuration.Services["php"])
	}
	if *app.Instances[0].Services["php"].Replicas != 1 || *app.Instances[1].Services["php"].Replicas != 3 {
		t.Fatal("drifting replicas were not retained as app-service overrides")
	}
}

func TestPrepareServiceCapacityBlocksUnsupportedReplicaCount(t *testing.T) {
	replicas := 2
	_, _, findings := prepareServiceCapacity("app", "dev", Service{Name: "nginx", Replicas: &replicas}, capacityTestTarget("nginx"))
	if len(findings) != 1 || findings[0].Severity != SeverityBlocking {
		t.Fatalf("expected replica blocker, got %+v", findings)
	}
}

func TestHandledLegacyCapacityIssue(t *testing.T) {
	if !handledLegacyCapacityIssue(ExportIssue{Code: "service.configuration_unsupported", Path: "apps.a.instances.i.services.nginx.configuration.resources"}) {
		t.Fatal("expected legacy resource issue to be handled")
	}
	if handledLegacyCapacityIssue(ExportIssue{Code: "service.configuration_unsupported", Path: "apps.a.instances.i.services.nginx.configuration.ports"}) {
		t.Fatal("ports must remain blocking")
	}
}

func TestCapacityResourceMatchingRequiresExactTargetAndValues(t *testing.T) {
	desired := PreparedServiceResources{
		Workload: "app", Container: "app",
		RequestCPU: targetTestIntPtr(250), LimitMem: targetTestIntPtr(512),
	}
	stackItems := []TargetStackServiceContainer{{
		StackServiceID: 10, Workload: "app", Name: "app",
		RequestCPU: targetTestIntPtr(250), LimitMem: targetTestIntPtr(512),
	}}
	appItems := []TargetAppServiceContainer{{
		AppServiceID: 20, Workload: "app", Name: "app",
		RequestCPU: targetTestIntPtr(250), LimitMem: targetTestIntPtr(512),
	}}
	if !stackServiceResourcesMatch(stackItems, desired) || !appServiceResourcesMatch(appItems, desired) {
		t.Fatal("exact resource values did not match")
	}
	stackItems[0].LimitMem = targetTestIntPtr(1024)
	appItems[0].LimitMem = targetTestIntPtr(1024)
	if stackServiceResourcesMatch(stackItems, desired) || appServiceResourcesMatch(appItems, desired) {
		t.Fatal("resource drift unexpectedly matched")
	}
}

func capacityTestTarget(name string) TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService: TargetStackService{Name: name},
		ServiceRevision: TargetServiceRevision{Manifest: &TargetServiceManifest{
			Workloads: []TargetServiceWorkload{{Name: "app", Containers: []TargetServiceContainer{{Name: "app"}}}},
		}},
	}
}
