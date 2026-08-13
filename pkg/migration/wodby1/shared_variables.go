package wodby1

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type sharedVariableObservation struct {
	appIndex    int
	appUUID     string
	serviceName string
	variable    PreparedStackEnvVar
}

type sharedVariableGroup struct {
	serviceName string
	appIndexes  []int
	appUUIDs    []string
	variables   []PreparedStackEnvVar
}

func prepareSharedVariableIntegrations(prepared *PreparedMigration, plan *Plan) ([]ReviewItem, error) {
	if prepared == nil || plan == nil || plan.Source.Kind != "server" || len(prepared.Apps) < 2 {
		return nil, nil
	}
	observed := map[string][]sharedVariableObservation{}
	for appIndex, app := range prepared.Apps {
		for serviceName, configuration := range app.StackConfiguration.Services {
			if !appSupportsVariableIntegration(app, serviceName) {
				continue
			}
			for _, variable := range configuration.EnvVars {
				key := sharedVariableIdentity(serviceName, variable)
				observed[key] = append(observed[key], sharedVariableObservation{
					appIndex: appIndex, appUUID: app.App.App.UUID, serviceName: serviceName, variable: variable,
				})
			}
		}
	}
	grouped := map[string]*sharedVariableGroup{}
	for _, observations := range observed {
		byApp := map[int]sharedVariableObservation{}
		for _, observation := range observations {
			byApp[observation.appIndex] = observation
		}
		if len(byApp) < 2 {
			continue
		}
		appIndexes := make([]int, 0, len(byApp))
		appUUIDs := make([]string, 0, len(byApp))
		var sample sharedVariableObservation
		for appIndex, observation := range byApp {
			appIndexes = append(appIndexes, appIndex)
			appUUIDs = append(appUUIDs, observation.appUUID)
			sample = observation
		}
		sort.Ints(appIndexes)
		sort.Strings(appUUIDs)
		groupKey := sample.serviceName + "\x00" + strings.Join(appUUIDs, "\x00")
		group := grouped[groupKey]
		if group == nil {
			group = &sharedVariableGroup{serviceName: sample.serviceName, appIndexes: appIndexes, appUUIDs: appUUIDs}
			grouped[groupKey] = group
		}
		group.variables = append(group.variables, sample.variable)
	}

	candidates := []sharedVariableGroup{}
	for _, group := range grouped {
		if len(uniqueSharedVariableNames(group.variables)) < 2 || sharedVariableFieldsConflict(group.variables) {
			continue
		}
		sort.SliceStable(group.variables, func(i, j int) bool {
			if group.variables[i].Name != group.variables[j].Name {
				return group.variables[i].Name < group.variables[j].Name
			}
			return optionalStringValue(group.variables[i].EnvType) < optionalStringValue(group.variables[j].EnvType)
		})
		candidates = append(candidates, *group)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := len(candidates[i].variables) * len(candidates[i].appIndexes)
		right := len(candidates[j].variables) * len(candidates[j].appIndexes)
		if left != right {
			return left > right
		}
		return candidates[i].serviceName+strings.Join(candidates[i].appUUIDs, "") < candidates[j].serviceName+strings.Join(candidates[j].appUUIDs, "")
	})

	selectedForService := map[string]bool{}
	generatedPlans := make(map[string][]IntegrationPlan, len(prepared.Apps))
	findings := []ReviewItem{}
	for _, group := range candidates {
		conflict := false
		for _, appIndex := range group.appIndexes {
			key := fmt.Sprintf("%d\x00%s", appIndex, group.serviceName)
			if selectedForService[key] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		variableNames := uniqueSharedVariableNames(group.variables)
		sort.Strings(variableNames)
		resourceDigestArgs := append([]string{plan.Source.ID, group.serviceName}, group.appUUIDs...)
		for _, name := range variableNames {
			resourceDigestArgs = append(resourceDigestArgs, fmt.Sprintf("%s:secret=%t", name, sharedVariableSecret(group.variables, name)))
		}
		digest := shortDigest(resourceDigestArgs...)
		providerName := "w1-shared-vars-" + digest
		key := "shared-vars-" + digest
		fields, values := sharedVariableProviderFields(group.variables)
		provider := &PreparedVariableProvider{
			Name: providerName, Title: "Shared Wodby 1 variables", Fields: fields,
		}
		for _, appIndex := range group.appIndexes {
			app := &prepared.Apps[appIndex]
			configuration := app.StackConfiguration.Services[group.serviceName]
			filtered := configuration.EnvVars[:0]
			for _, variable := range configuration.EnvVars {
				if !containsPreparedStackEnvVar(group.variables, variable) {
					filtered = append(filtered, variable)
				}
			}
			configuration.EnvVars = filtered
			configuration.Integrations = append(configuration.Integrations, PreparedStackIntegrationLink{Name: "variable", IntegrationKey: key})
			sortPreparedStackServiceConfiguration(&configuration)
			app.StackConfiguration.Services[group.serviceName] = configuration
			app.Integrations = append(app.Integrations, PreparedIntegration{
				Key: key, ProviderName: providerName, ProviderID: 0,
				Name: providerName, Title: "Shared Wodby 1 variables", Kind: "variable", Service: group.serviceName,
				Fields: append([]TargetIntegrationFieldInput(nil), values...), VariableProvider: provider,
			})
			generatedPlans[app.App.App.UUID] = append(generatedPlans[app.App.App.UUID], IntegrationPlan{
				Key: key, ProviderName: providerName, Kind: "variable", Service: group.serviceName,
				Action: integrationActionVariableProvider, Variables: append([]string(nil), variableNames...),
			})
			selectedForService[fmt.Sprintf("%d\x00%s", appIndex, group.serviceName)] = true
		}
		appNames := make([]string, 0, len(group.appIndexes))
		for _, appIndex := range group.appIndexes {
			appNames = append(appNames, prepared.Apps[appIndex].App.App.Name)
		}
		findings = append(findings, ReviewItem{
			Severity: SeverityMigration, Subject: "shared variable integration",
			Message: fmt.Sprintf("%d identical variable(s) on target service %q will move into one reusable custom variable integration shared by apps %s", len(group.variables), group.serviceName, strings.Join(appNames, ", ")),
		})
	}

	planApps := map[string]*AppPlan{}
	for index := range plan.Apps {
		planApps[plan.Apps[index].SourceUUID] = &plan.Apps[index]
	}
	for _, app := range prepared.Apps {
		appPlan := planApps[app.App.App.UUID]
		if appPlan == nil {
			return nil, fmt.Errorf("migration plan is missing source app %q while preparing shared variables", app.App.App.UUID)
		}
		base := []IntegrationPlan{}
		existingShared := []IntegrationPlan{}
		for _, item := range appPlan.Integrations {
			if item.Action == integrationActionVariableProvider {
				existingShared = append(existingShared, item)
			} else {
				base = append(base, item)
			}
		}
		generated := generatedPlans[app.App.App.UUID]
		if len(existingShared) != 0 && !sameIntegrationPlans(existingShared, generated) {
			return nil, currentPlanDriftError("shared variable integration mapping changed")
		}
		appPlan.Integrations = append(base, generated...)
	}
	return findings, nil
}

func sharedVariableSecret(variables []PreparedStackEnvVar, name string) bool {
	for _, variable := range variables {
		if variable.Name == name {
			return variable.Secret
		}
	}
	return false
}

func appSupportsVariableIntegration(app PreparedAppMigration, serviceName string) bool {
	for _, instance := range app.Instances {
		found := false
		for _, inspection := range instance.StackServices {
			if inspection.StackService.Name != serviceName || inspection.ServiceRevision.Manifest == nil {
				continue
			}
			for _, integration := range inspection.ServiceRevision.Manifest.Integrations {
				if integration.Name == "variable" && integration.Type == "variable" {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return len(app.Instances) != 0
}

func sharedVariableIdentity(service string, variable PreparedStackEnvVar) string {
	return strings.Join([]string{service, variable.Name, optionalStringValue(variable.EnvType), fmt.Sprint(variable.Secret), variable.Value}, "\x00")
}

func uniqueSharedVariableNames(variables []PreparedStackEnvVar) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, variable := range variables {
		if !seen[variable.Name] {
			seen[variable.Name] = true
			result = append(result, variable.Name)
		}
	}
	return result
}

func sharedVariableFieldsConflict(variables []PreparedStackEnvVar) bool {
	secrets := map[string]bool{}
	seen := map[string]bool{}
	for _, variable := range variables {
		if seen[variable.Name] && secrets[variable.Name] != variable.Secret {
			return true
		}
		seen[variable.Name] = true
		secrets[variable.Name] = variable.Secret
	}
	return false
}

func sharedVariableProviderFields(variables []PreparedStackEnvVar) ([]TargetVariableProviderFieldInput, []TargetIntegrationFieldInput) {
	fieldByVariable := map[string]string{}
	usedFields := map[string]bool{}
	fields := []TargetVariableProviderFieldInput{}
	values := []TargetIntegrationFieldInput{}
	for _, variable := range variables {
		fieldName := fieldByVariable[variable.Name]
		if fieldName == "" {
			fieldName = variableProviderFieldName(variable.Name)
			base := fieldName
			for suffix := 2; usedFields[fieldName]; suffix++ {
				fieldName = fmt.Sprintf("%s_%d", base, suffix)
			}
			fieldByVariable[variable.Name] = fieldName
			usedFields[fieldName] = true
			fields = append(fields, TargetVariableProviderFieldInput{
				Name: fieldName, Label: variable.Name, Variable: variable.Name, Secret: variable.Secret,
			})
		}
		values = append(values, TargetIntegrationFieldInput{
			Name: fieldName, Value: variable.Value, EnvType: variable.EnvType,
		})
	}
	return fields, values
}

func variableProviderFieldName(variable string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return unicode.ToLower(r)
		}
		return '_'
	}, strings.TrimSpace(variable))
	name = strings.Trim(name, "_")
	if name == "" || unicode.IsDigit(rune(name[0])) {
		name = "var_" + name
	}
	return name
}

func containsPreparedStackEnvVar(items []PreparedStackEnvVar, target PreparedStackEnvVar) bool {
	for _, item := range items {
		if item.Name == target.Name && item.Value == target.Value && item.Secret == target.Secret && sameOptionalString(item.EnvType, target.EnvType) {
			return true
		}
	}
	return false
}
