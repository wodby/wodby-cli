package wodby1

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type capacityServiceRef struct {
	instanceIndex int
	sourceName    string
	service       PreparedService
}

// prepareServiceCapacity converts Wodby 1 service capacity into the exact
// workload/container target accepted by the Wodby 2 resource API.
func prepareServiceCapacity(appName, instanceName string, source Service, target TargetStackServiceInspection) (*int, *PreparedServiceResources, []ReviewItem) {
	replicas, resources, err := sourceServiceCapacity(source)
	if err != nil {
		return nil, nil, []ReviewItem{stackConfigBlocker(appName, instanceName, "service "+source.Name+" capacity", err.Error())}
	}
	findings := []ReviewItem{}
	if replicas != nil && *replicas > 1 && (target.ServiceRevision.Manifest == nil || !target.ServiceRevision.Manifest.Scalable) {
		findings = append(findings, stackConfigBlocker(
			appName,
			instanceName,
			"service "+source.Name+" replicas",
			fmt.Sprintf("source uses %d replicas but target service %q does not support horizontal replicas", *replicas, target.StackService.Name),
		))
	}
	if !resources.HasValues() {
		return replicas, nil, findings
	}
	workload, container, err := targetPrimaryContainer(target.ServiceRevision.Manifest)
	if err != nil {
		findings = append(findings, stackConfigBlocker(appName, instanceName, "service "+source.Name+" resources", err.Error()))
		return replicas, nil, findings
	}
	prepared := &PreparedServiceResources{
		Workload: workload, Container: container,
		RequestCPU: cloneInt(resources.RequestCPU), RequestMem: cloneInt(resources.RequestMem),
		LimitCPU: cloneInt(resources.LimitCPU), LimitMem: cloneInt(resources.LimitMem),
	}
	return replicas, prepared, findings
}

// promoteSharedServiceCapacity moves capacity to the shared stack when every
// migrated instance maps the same target service and uses the same value. Any
// drift remains an app-service override on the affected instance.
func promoteSharedServiceCapacity(app *PreparedAppMigration, configuration *PreparedStackConfiguration) {
	if app == nil || configuration == nil || len(app.Instances) == 0 {
		return
	}
	byTarget := map[string][]capacityServiceRef{}
	for instanceIndex := range app.Instances {
		for sourceName, service := range app.Instances[instanceIndex].Services {
			name := service.Target.StackService.Name
			if strings.TrimSpace(name) == "" {
				continue
			}
			byTarget[name] = append(byTarget[name], capacityServiceRef{instanceIndex: instanceIndex, sourceName: sourceName, service: service})
		}
	}
	for targetName, refs := range byTarget {
		// A stack-level value would affect every instance. Keep overrides at
		// instance level unless this target service occurs exactly once in each.
		if len(refs) != len(app.Instances) || !capacityRefsCoverInstances(refs, len(app.Instances)) {
			continue
		}
		serviceConfig := configuration.Services[targetName]
		if serviceConfig.Settings == nil {
			serviceConfig.Settings = map[string]string{}
		}
		if sharedReplicas(refs) {
			serviceConfig.Replicas = cloneInt(refs[0].service.Replicas)
			for _, ref := range refs {
				service := app.Instances[ref.instanceIndex].Services[ref.sourceName]
				service.Replicas = nil
				app.Instances[ref.instanceIndex].Services[ref.sourceName] = service
			}
		}
		if sharedResources(refs) {
			serviceConfig.Resources = clonePreparedServiceResources(refs[0].service.Resources)
			for _, ref := range refs {
				service := app.Instances[ref.instanceIndex].Services[ref.sourceName]
				service.Resources = nil
				app.Instances[ref.instanceIndex].Services[ref.sourceName] = service
			}
		}
		if preparedStackServiceConfigurationHasChanges(serviceConfig) {
			configuration.Services[targetName] = serviceConfig
		}
	}
}

func capacityRefsCoverInstances(refs []capacityServiceRef, instanceCount int) bool {
	covered := make(map[int]bool, len(refs))
	for _, ref := range refs {
		if ref.instanceIndex < 0 || ref.instanceIndex >= instanceCount || covered[ref.instanceIndex] {
			return false
		}
		covered[ref.instanceIndex] = true
	}
	return len(covered) == instanceCount
}

func sharedReplicas(refs []capacityServiceRef) bool {
	if len(refs) == 0 || refs[0].service.Replicas == nil {
		return false
	}
	value := *refs[0].service.Replicas
	for _, ref := range refs[1:] {
		if ref.service.Replicas == nil || *ref.service.Replicas != value {
			return false
		}
	}
	return true
}

func sharedResources(refs []capacityServiceRef) bool {
	if len(refs) == 0 || refs[0].service.Resources == nil {
		return false
	}
	for _, ref := range refs[1:] {
		if !preparedServiceResourcesEqual(refs[0].service.Resources, ref.service.Resources) {
			return false
		}
	}
	return true
}

func sourceServiceCapacity(service Service) (*int, *ServiceResources, error) {
	replicas := cloneInt(service.Replicas)
	resources := cloneServiceResources(service.Resources)
	if replicas == nil {
		if deployment, ok := objectValue(service.Configuration["deployment"]); ok {
			if raw, exists := deployment["replicas"]; exists {
				value, err := integerValue(raw)
				if err != nil {
					return nil, nil, fmt.Errorf("source replicas %v", err)
				}
				replicas = &value
			}
		}
	}
	if !resources.HasValues() {
		legacy, err := legacyServiceResources(service.Configuration["resources"])
		if err != nil {
			return nil, nil, err
		}
		resources = legacy
	}
	if replicas != nil && *replicas < 0 {
		return nil, nil, fmt.Errorf("source replicas must not be negative")
	}
	if resources.HasValues() {
		if err := validateTargetResourceValues(resources.RequestCPU, resources.RequestMem, resources.LimitCPU, resources.LimitMem); err != nil {
			return nil, nil, fmt.Errorf("source resources are invalid: %v", err)
		}
	}
	return replicas, resources, nil
}

func handledLegacyCapacityIssue(issue ExportIssue) bool {
	if issue.Code != "service.configuration_unsupported" {
		return false
	}
	path := strings.TrimSpace(issue.Path)
	return strings.HasSuffix(path, ".configuration.deployment") || strings.HasSuffix(path, ".configuration.resources")
}

func migratableServiceSettingCount(configuration map[string]interface{}) int {
	count := 0
	for name := range configuration {
		if name != "deployment" && name != "resources" {
			count++
		}
	}
	return count
}

func legacyServiceResources(raw interface{}) (*ServiceResources, error) {
	root, ok := objectValue(raw)
	if !ok || len(root) == 0 {
		return nil, nil
	}
	result := &ServiceResources{}
	for scope, fields := range map[string]struct {
		request **int
		limit   **int
		cpu     bool
	}{
		"cpu":    {request: &result.RequestCPU, limit: &result.LimitCPU, cpu: true},
		"memory": {request: &result.RequestMem, limit: &result.LimitMem},
	} {
		values, exists := objectValue(root[scope])
		if !exists {
			continue
		}
		for name, target := range map[string]**int{"request": fields.request, "limit": fields.limit} {
			rawValue, exists := values[name]
			if !exists || rawValue == nil {
				continue
			}
			value, err := numericValue(rawValue)
			if err != nil {
				return nil, fmt.Errorf("source %s %s %v", scope, name, err)
			}
			if fields.cpu {
				value *= 1000
			}
			rounded := math.Round(value)
			if math.Abs(value-rounded) > 0.000001 {
				return nil, fmt.Errorf("source %s %s must resolve to a whole target API unit", scope, name)
			}
			integer := int(rounded)
			*target = &integer
		}
	}
	if !result.HasValues() {
		return nil, nil
	}
	return result, nil
}

func targetPrimaryContainer(manifest *TargetServiceManifest) (string, string, error) {
	if manifest == nil || len(manifest.Workloads) == 0 {
		return "", "", fmt.Errorf("target service does not expose a workload for resource migration")
	}
	index := -1
	for i, workload := range manifest.Workloads {
		if workload.Primary {
			if index != -1 {
				return "", "", fmt.Errorf("target service exposes multiple primary workloads")
			}
			index = i
		}
	}
	if index == -1 && len(manifest.Workloads) == 1 {
		index = 0
	}
	if index == -1 {
		return "", "", fmt.Errorf("target service does not expose one unambiguous primary workload")
	}
	workload := manifest.Workloads[index]
	if strings.TrimSpace(workload.Name) == "" || len(workload.Containers) == 0 || strings.TrimSpace(workload.Containers[0].Name) == "" {
		return "", "", fmt.Errorf("target service primary workload does not expose a container")
	}
	return workload.Name, workload.Containers[0].Name, nil
}

func objectValue(value interface{}) (map[string]interface{}, bool) {
	item, ok := value.(map[string]interface{})
	return item, ok
}

func integerValue(value interface{}) (int, error) {
	number, err := numericValue(value)
	if err != nil {
		return 0, err
	}
	rounded := math.Round(number)
	if math.Abs(number-rounded) > 0.000001 {
		return 0, fmt.Errorf("must be an integer")
	}
	return int(rounded), nil
}

func numericValue(value interface{}) (float64, error) {
	switch item := value.(type) {
	case json.Number:
		result, err := strconv.ParseFloat(item.String(), 64)
		if err != nil {
			return 0, fmt.Errorf("must be numeric")
		}
		return result, nil
	case float64:
		return item, nil
	case float32:
		return float64(item), nil
	case int:
		return float64(item), nil
	case int64:
		return float64(item), nil
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		if err != nil {
			return 0, fmt.Errorf("must be numeric")
		}
		return result, nil
	default:
		return 0, fmt.Errorf("must be numeric")
	}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneServiceResources(value *ServiceResources) *ServiceResources {
	if value == nil {
		return nil
	}
	return &ServiceResources{
		RequestCPU: cloneInt(value.RequestCPU), RequestMem: cloneInt(value.RequestMem),
		LimitCPU: cloneInt(value.LimitCPU), LimitMem: cloneInt(value.LimitMem),
	}
}

func clonePreparedServiceResources(value *PreparedServiceResources) *PreparedServiceResources {
	if value == nil {
		return nil
	}
	return &PreparedServiceResources{
		Workload: value.Workload, Container: value.Container,
		RequestCPU: cloneInt(value.RequestCPU), RequestMem: cloneInt(value.RequestMem),
		LimitCPU: cloneInt(value.LimitCPU), LimitMem: cloneInt(value.LimitMem),
	}
}

func preparedServiceResourcesEqual(left, right *PreparedServiceResources) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Workload == right.Workload && left.Container == right.Container &&
		intPointersEqual(left.RequestCPU, right.RequestCPU) && intPointersEqual(left.RequestMem, right.RequestMem) &&
		intPointersEqual(left.LimitCPU, right.LimitCPU) && intPointersEqual(left.LimitMem, right.LimitMem)
}

func intPointersEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (r PreparedServiceResources) TargetInput() TargetResourcesInput {
	workload, container := r.Workload, r.Container
	return TargetResourcesInput{
		Workload: &workload, Container: &container,
		RequestCPU: cloneInt(r.RequestCPU), RequestMem: cloneInt(r.RequestMem),
		LimitCPU: cloneInt(r.LimitCPU), LimitMem: cloneInt(r.LimitMem),
	}
}

func serviceResourcesSummary(resources *PreparedServiceResources) string {
	if resources == nil {
		return "-"
	}
	parts := []string{}
	if resources.RequestCPU != nil {
		parts = append(parts, fmt.Sprintf("CPU request %dm", *resources.RequestCPU))
	}
	if resources.LimitCPU != nil {
		parts = append(parts, fmt.Sprintf("CPU limit %dm", *resources.LimitCPU))
	}
	if resources.RequestMem != nil {
		parts = append(parts, fmt.Sprintf("memory request %dMi", *resources.RequestMem))
	}
	if resources.LimitMem != nil {
		parts = append(parts, fmt.Sprintf("memory limit %dMi", *resources.LimitMem))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
