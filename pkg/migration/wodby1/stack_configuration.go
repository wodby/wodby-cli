package wodby1

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	wodby1AppDocrootPattern  = regexp.MustCompile(`^[a-zA-Z0-9_./-]*$`)
	wodby1AppSiteNamePattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
)

func mergePreparedStackAdditions(appName string, instances []PreparedInstance) ([]PreparedStackServiceAddition, []ReviewItem) {
	byName := map[string]PreparedStackServiceAddition{}
	findings := []ReviewItem{}
	for _, instance := range instances {
		for _, addition := range instance.StackAdditions {
			current, exists := byName[addition.Name]
			if exists && (current.ServiceID != addition.ServiceID || current.ServiceRevisionID != addition.ServiceRevisionID) {
				findings = append(findings, stackConfigBlocker(
					appName,
					instance.Source.Name,
					"additional stack service "+addition.Name,
					"app instances resolve different Wodby 2 service revisions for one shared stack",
				))
				continue
			}
			byName[addition.Name] = addition
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]PreparedStackServiceAddition, 0, len(names))
	for _, name := range names {
		result = append(result, byName[name])
	}
	return result, findings
}

type stackConfigServiceInstance struct {
	instance PreparedInstance
	source   Service
	mapping  PreparedService
}

type stackEnvObservation struct {
	instanceID string
	envType    string
	value      string
	secret     bool
}

type stackCronObservation struct {
	instanceID string
	envType    string
	cron       PreparedStackCronSchedule
}

func prepareStackConfiguration(app PreparedAppMigration) (PreparedStackConfiguration, []ReviewItem, error) {
	configuration := PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{}}
	findings := []ReviewItem{}
	services := map[string][]stackConfigServiceInstance{}
	inspections := map[string]TargetStackServiceInspection{}

	for _, instance := range app.Instances {
		for _, source := range instance.Source.Services {
			mapping, ok := instance.Services[source.Name]
			if !ok || !source.Enabled {
				continue
			}
			targetName := mapping.Target.StackService.Name
			services[targetName] = append(services[targetName], stackConfigServiceInstance{
				instance: instance,
				source:   source,
				mapping:  mapping,
			})
			inspections[targetName] = mapping.Target
		}
	}

	targetNames := make([]string, 0, len(services))
	for name := range services {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	for _, targetName := range targetNames {
		items := services[targetName]
		serviceConfig := PreparedStackServiceConfiguration{Settings: map[string]string{}}

		versionOptions, versionFindings, err := preparedStackVersionOptions(app.App.App.Name, targetName, items)
		if err != nil {
			return PreparedStackConfiguration{}, nil, err
		}
		serviceConfig.VersionOptions = versionOptions
		findings = append(findings, versionFindings...)

		envVars, envFindings := preparedStackEnvVars(app.App.App.Name, targetName, items)
		serviceConfig.EnvVars = envVars
		findings = append(findings, envFindings...)

		settings, settingFindings := preparedStackSettings(app.App.App.Name, targetName, items)
		serviceConfig.Settings = settings
		findings = append(findings, settingFindings...)

		crons, cronFindings := preparedStackCrons(app.App.App.Name, targetName, items)
		serviceConfig.CronSchedules = append(serviceConfig.CronSchedules, crons...)
		findings = append(findings, cronFindings...)
		serviceConfig.CronSchedules = append(serviceConfig.CronSchedules, disabledDefaultCronSchedules(inspections[targetName])...)
		sortPreparedStackServiceConfiguration(&serviceConfig)
		configuration.Services[targetName] = serviceConfig
	}
	appSettingFindings := prepareDrupalAppSettings(app, &configuration)
	findings = append(findings, appSettingFindings...)
	gotenbergFindings := prepareGotenbergEndpoint(app, &configuration)
	findings = append(findings, gotenbergFindings...)
	return configuration, findings, nil
}

// prepareGotenbergEndpoint gives managed Drupal and WordPress application code
// a stable private endpoint for the mapped Gotenberg service. The Drupal
// Gotenberg module stores its base URL in Drupal configuration rather than
// reading this variable itself; the public migration guide documents the
// settings.php override needed to consume it.
func prepareGotenbergEndpoint(app PreparedAppMigration, configuration *PreparedStackConfiguration) []ReviewItem {
	if configuration == nil || len(app.Instances) == 0 {
		return nil
	}

	expectedByEnv := map[string]map[string]bool{}
	observations := map[string][]stackEnvObservation{}
	for _, instance := range app.Instances {
		family := sourceStackFamily(instance.Source.Stack)
		if !strings.HasPrefix(family, "drupal") && !strings.HasPrefix(family, "wordpress") {
			return nil
		}
		envType := normalizedTargetEnvType(instance.TargetEnvType)
		if expectedByEnv[envType] == nil {
			expectedByEnv[envType] = map[string]bool{}
		}
		expectedByEnv[envType][instance.Source.UUID] = true

		targets := serviceTargetNamesFromPrepared(instance)
		targetName, found := mappedGotenbergTarget(targets)
		if !found || !instance.EffectiveState[targetName] {
			continue
		}
		if !instance.EffectiveState["php"] {
			return []ReviewItem{stackConfigBlocker(app.App.App.Name, instance.Source.Name, "Gotenberg endpoint", "the mapped Gotenberg service is enabled but the managed target PHP service is not enabled")}
		}
		endpoint, _ := privateGotenbergEndpoint(targets)
		observations[envType] = append(observations[envType], stackEnvObservation{
			instanceID: instance.Source.UUID,
			envType:    envType,
			value:      endpoint,
		})
	}
	if len(observations) == 0 {
		return nil
	}

	for envType, expected := range expectedByEnv {
		seen := uniqueStackEnvInstances(observations[envType])
		if len(seen) != 0 && len(seen) != len(expected) {
			return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Gotenberg endpoint", fmt.Sprintf("instances with target environment type %s do not agree on whether Gotenberg is enabled", envType))}
		}
		if _, _, consistent := commonStackEnvValue(observations[envType]); !consistent {
			return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Gotenberg endpoint", fmt.Sprintf("instances with target environment type %s resolve different private Gotenberg endpoints", envType))}
		}
	}

	service := configuration.Services["php"]
	filtered := service.EnvVars[:0]
	for _, variable := range service.EnvVars {
		if !strings.EqualFold(variable.Name, "GOTENBERG_ENDPOINT") {
			filtered = append(filtered, variable)
		}
	}
	service.EnvVars = filtered

	all := []stackEnvObservation{}
	for _, items := range observations {
		all = append(all, items...)
	}
	expectedTotal := 0
	for _, expected := range expectedByEnv {
		expectedTotal += len(expected)
	}
	if value, _, same := commonStackEnvValue(all); same && len(uniqueStackEnvInstances(all)) == expectedTotal {
		service.EnvVars = append(service.EnvVars, PreparedStackEnvVar{Name: "GOTENBERG_ENDPOINT", Value: value})
	} else {
		envTypes := make([]string, 0, len(observations))
		for envType := range observations {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		for _, envType := range envTypes {
			value, _, _ := commonStackEnvValue(observations[envType])
			scope := envType
			service.EnvVars = append(service.EnvVars, PreparedStackEnvVar{Name: "GOTENBERG_ENDPOINT", Value: value, EnvType: &scope})
		}
	}
	sortPreparedStackServiceConfiguration(&service)
	configuration.Services["php"] = service

	return []ReviewItem{{
		Severity: SeverityMigration,
		App:      app.App.App.Name,
		Subject:  "Gotenberg endpoint",
		Message:  "target PHP will receive GOTENBERG_ENDPOINT with the private in-cluster Gotenberg URL",
	}}
}

// prepareDrupalAppSettings preserves the Wodby 1 app-level new-app choices on
// the Wodby 2 Drupal PHP stack service. Nginx derives both settings from its
// backend service, so duplicating the values on nginx would be incorrect.
func prepareDrupalAppSettings(app PreparedAppMigration, configuration *PreparedStackConfiguration) []ReviewItem {
	if configuration == nil || len(app.Instances) == 0 {
		return nil
	}
	isDrupal := false
	hasOtherFamily := false
	for _, instance := range app.Instances {
		family := sourceStackFamily(instance.Source.Stack)
		if strings.HasPrefix(family, "drupal") {
			isDrupal = true
		} else {
			hasOtherFamily = true
		}
	}
	if !isDrupal {
		return nil
	}
	if hasOtherFamily {
		return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Drupal app settings", "the app's migrated instances do not resolve to one Drupal stack family")}
	}
	if app.App.App.Docroot == nil || app.App.App.SiteName == nil {
		return []ReviewItem{stackConfigBlocker(
			app.App.App.Name,
			"",
			"Drupal app settings",
			"the Wodby 1 export does not include the raw app docroot and site directory; deploy the current Wodby 1 migration API before retrying",
		)}
	}
	docroot := strings.TrimSpace(*app.App.App.Docroot)
	if len(docroot) > 128 || !wodby1AppDocrootPattern.MatchString(docroot) || strings.Contains(docroot, "..") || strings.HasPrefix(docroot, "/") {
		return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Drupal app docroot", fmt.Sprintf("source app docroot %q is not a safe relative path", docroot))}
	}
	siteName := strings.TrimSpace(*app.App.App.SiteName)
	if siteName == "" {
		siteName = "default"
	}
	if len(siteName) > 128 || !wodby1AppSiteNamePattern.MatchString(siteName) {
		return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Drupal site directory", fmt.Sprintf("source Drupal site directory %q is invalid", siteName))}
	}

	desiredSettings := map[string]string{"docroot": docroot, "sitedir": siteName}
	settingNeedsUpdate := map[string]bool{}
	for _, instance := range app.Instances {
		php, found := targetStackInspectionByName(instance.StackServices, "php")
		if !found || !instance.EffectiveState["php"] {
			return []ReviewItem{stackConfigBlocker(app.App.App.Name, instance.Source.Name, "Drupal app settings", "the enabled target Drupal PHP service named \"php\" was not found")}
		}
		available := map[string]TargetServiceSettingCapability{}
		if php.ServiceRevision.Manifest != nil {
			for _, setting := range php.ServiceRevision.Manifest.Settings {
				available[setting.Name] = setting
			}
		}
		overrides := map[string]TargetStackServiceSetting{}
		for _, setting := range php.StackService.Settings {
			overrides[setting.Name] = setting
		}
		for _, name := range []string{"docroot", "sitedir"} {
			setting, found := available[name]
			if !found {
				return []ReviewItem{stackConfigBlocker(app.App.App.Name, instance.Source.Name, "Drupal app settings", fmt.Sprintf("target PHP service does not expose required setting %q", name))}
			}
			currentValue := setting.Default
			if override, found := overrides[name]; found {
				currentValue = override.Value
			}
			if currentValue != desiredSettings[name] {
				settingNeedsUpdate[name] = true
			}
		}
	}

	service := configuration.Services["php"]
	if service.Settings == nil {
		service.Settings = map[string]string{}
	}
	for name, value := range desiredSettings {
		if existing, found := service.Settings[name]; found && existing != value {
			return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "Drupal app settings", fmt.Sprintf("target PHP setting %q resolves both from source service configuration (%q) and the Wodby 1 app setting (%q)", name, existing, value))}
		}
		if settingNeedsUpdate[name] {
			service.Settings[name] = value
		}
	}
	if len(service.Settings) != 0 || len(service.VersionOptions) != 0 || len(service.EnvVars) != 0 || len(service.CronSchedules) != 0 || len(service.Integrations) != 0 || len(service.Links) != 0 {
		configuration.Services["php"] = service
	}
	return []ReviewItem{{
		Severity: SeverityMigration,
		App:      app.App.App.Name,
		Subject:  "Drupal app settings",
		Message:  fmt.Sprintf("Wodby 1 Drupal subdirectory %q and site directory %q will map to target PHP settings docroot and sitedir", docroot, siteName),
	}}
}

func targetStackInspectionByName(items []TargetStackServiceInspection, name string) (TargetStackServiceInspection, bool) {
	for _, item := range items {
		if item.StackService.Name == name {
			return item, true
		}
	}
	return TargetStackServiceInspection{}, false
}

func preparedStackVersionOptions(appName, targetName string, items []stackConfigServiceInstance) ([]TargetStackServiceOptionInput, []ReviewItem, error) {
	selected := ""
	for _, item := range items {
		version := strings.TrimSpace(item.mapping.TargetVersion)
		if version == "" {
			continue
		}
		if selected != "" && selected != version {
			return nil, []ReviewItem{{
				Severity: SeverityBlocking,
				App:      appName,
				Subject:  "stack service " + targetName + " version",
				Message:  fmt.Sprintf("app instances resolve different target versions (%s and %s); one shared stack cannot represent both, so use consistent --target-version-map overrides", selected, version),
			}}, nil
		}
		selected = version
	}
	if selected == "" || len(items) == 0 {
		return nil, nil, nil
	}
	first := items[0]
	options, err := effectiveTargetVersionOptions(first.mapping.Target, first.instance.Stack.RevisionManifest)
	if err != nil {
		return nil, nil, err
	}
	currentDefault := ""
	inputs := make([]TargetStackServiceOptionInput, 0, len(options))
	found := false
	for _, option := range options {
		if option.Default && !option.Disabled {
			currentDefault = option.Version
		}
		isSelected := option.Version == selected
		found = found || isSelected
		inputs = append(inputs, TargetStackServiceOptionInput{
			Version: option.Version, Default: isSelected, Disabled: option.Disabled && !isSelected,
		})
	}
	if !found {
		return nil, nil, fmt.Errorf("selected target version %q disappeared from stack service %q options", selected, targetName)
	}
	if currentDefault == selected {
		return nil, nil, nil
	}
	return inputs, nil, nil
}

func preparedStackEnvVars(appName, targetName string, items []stackConfigServiceInstance) ([]PreparedStackEnvVar, []ReviewItem) {
	expectedByEnv := map[string]map[string]bool{}
	observations := map[string][]stackEnvObservation{}
	findings := []ReviewItem{}
	for _, item := range items {
		envType := normalizedTargetEnvType(item.instance.TargetEnvType)
		if expectedByEnv[envType] == nil {
			expectedByEnv[envType] = map[string]bool{}
		}
		expectedByEnv[envType][item.instance.Source.UUID] = true
		targets := serviceTargetNamesFromPrepared(item.instance)
		for _, variable := range item.source.EnvVars {
			if !sourceEnvVarRequiresMigration(item.instance.Source.Properties, variable) {
				continue
			}
			name := strings.TrimSpace(variable.Name)
			if name == "" {
				findings = append(findings, stackConfigBlocker(appName, item.instance.Source.Name, "stack service "+targetName+" environment", "source custom environment variable name is empty"))
				continue
			}
			if variable.IsRedacted() {
				findings = append(findings, stackConfigBlocker(appName, item.instance.Source.Name, "env var "+name, "protected source value is redacted; create a fresh Wodby 1 export with secret access before migration"))
				continue
			}
			observations[name] = append(observations[name], stackEnvObservation{
				instanceID: item.instance.Source.UUID,
				envType:    envType,
				value:      migratedEnvironmentValue(item.source, variable, targets),
				secret:     variable.Secret || variable.Protected,
			})
		}
	}

	names := make([]string, 0, len(observations))
	for name := range observations {
		names = append(names, name)
	}
	sort.Strings(names)
	result := []PreparedStackEnvVar{}
	for _, name := range names {
		byEnv := map[string][]stackEnvObservation{}
		all := observations[name]
		for _, observation := range all {
			byEnv[observation.envType] = append(byEnv[observation.envType], observation)
		}
		conflict := false
		for envType, expected := range expectedByEnv {
			seen := map[string]stackEnvObservation{}
			for _, observation := range byEnv[envType] {
				if previous, exists := seen[observation.instanceID]; exists && (previous.value != observation.value || previous.secret != observation.secret) {
					conflict = true
				}
				seen[observation.instanceID] = observation
			}
			if len(seen) != 0 && len(seen) != len(expected) {
				findings = append(findings, stackConfigBlocker(appName, "", "stack env var "+name, fmt.Sprintf("instances with target environment type %s do not agree on whether this variable exists", envType)))
				conflict = true
			}
			value, secret, consistent := commonStackEnvValue(byEnv[envType])
			_ = value
			_ = secret
			if len(byEnv[envType]) != 0 && !consistent {
				findings = append(findings, stackConfigBlocker(appName, "", "stack env var "+name, fmt.Sprintf("instances with target environment type %s have different values; environment-type scoping cannot distinguish them", envType)))
				conflict = true
			}
		}
		if conflict {
			continue
		}
		allValue, allSecret, allSame := commonStackEnvValue(all)
		expectedTotal := 0
		for _, expected := range expectedByEnv {
			expectedTotal += len(expected)
		}
		if allSame && len(uniqueStackEnvInstances(all)) == expectedTotal {
			result = append(result, PreparedStackEnvVar{Name: name, Value: allValue, Secret: allSecret})
			continue
		}
		envTypes := make([]string, 0, len(byEnv))
		for envType := range byEnv {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		for _, envType := range envTypes {
			value, secret, _ := commonStackEnvValue(byEnv[envType])
			scope := envType
			result = append(result, PreparedStackEnvVar{Name: name, Value: value, Secret: secret, EnvType: &scope})
		}
	}
	return result, findings
}

func preparedStackSettings(appName, targetName string, items []stackConfigServiceInstance) (map[string]string, []ReviewItem) {
	values := map[string]map[string]string{}
	findings := []ReviewItem{}
	for _, item := range items {
		for name, raw := range item.source.Configuration {
			value, err := scalarConfigurationValue(raw)
			if err != nil {
				findings = append(findings, stackConfigBlocker(appName, item.instance.Source.Name, "stack service "+targetName+" setting "+name, err.Error()))
				continue
			}
			if values[name] == nil {
				values[name] = map[string]string{}
			}
			values[name][item.instance.Source.UUID] = migratedEnvironmentReferences(value)
		}
	}
	result := map[string]string{}
	for name, perInstance := range values {
		if len(perInstance) != len(items) {
			findings = append(findings, stackConfigBlocker(appName, "", "stack service "+targetName+" setting "+name, "the setting is not present on every migrated instance; Wodby 2 stack settings are global and cannot represent this difference"))
			continue
		}
		value := ""
		consistent := true
		first := true
		for _, itemValue := range perInstance {
			if first {
				value, first = itemValue, false
			} else if itemValue != value {
				consistent = false
			}
		}
		if !consistent {
			findings = append(findings, stackConfigBlocker(appName, "", "stack service "+targetName+" setting "+name, "migrated instances use different values; Wodby 2 stack settings are global"))
			continue
		}
		result[name] = value
	}
	return result, findings
}

func preparedStackCrons(appName, targetName string, items []stackConfigServiceInstance) ([]PreparedStackCronSchedule, []ReviewItem) {
	expectedByEnv := map[string]map[string]bool{}
	observations := map[string][]stackCronObservation{}
	findings := []ReviewItem{}
	for _, item := range items {
		envType := normalizedTargetEnvType(item.instance.TargetEnvType)
		if expectedByEnv[envType] == nil {
			expectedByEnv[envType] = map[string]bool{}
		}
		expectedByEnv[envType][item.instance.Source.UUID] = true
		for _, cron := range item.source.CronJobs {
			if !cron.Enabled || cron.Classification == "source_only_infrastructure" {
				continue
			}
			if strings.TrimSpace(cron.Crontab) == "" || strings.TrimSpace(cron.Command) == "" {
				findings = append(findings, stackConfigBlocker(appName, item.instance.Source.Name, "stack service "+targetName+" cron", "source application cron requires both schedule and command"))
				continue
			}
			title := strings.TrimSpace(cron.Title)
			if title == "" {
				title = "Migrated Wodby 1 cron"
			}
			command := migratedEnvironmentReferences(cron.Command)
			name := "w1-" + shortDigest(targetName, title, cron.Crontab, command)
			observations[name] = append(observations[name], stackCronObservation{
				instanceID: item.instance.Source.UUID,
				envType:    envType,
				cron: PreparedStackCronSchedule{
					Name: name, Title: title, Crontab: cron.Crontab, Command: command,
					Disabled: item.instance.DisableCronSchedules,
				},
			})
		}
	}
	result := []PreparedStackCronSchedule{}
	names := make([]string, 0, len(observations))
	for name := range observations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		all := observations[name]
		byEnv := map[string][]stackCronObservation{}
		for _, observation := range all {
			byEnv[observation.envType] = append(byEnv[observation.envType], observation)
		}
		conflict := false
		for envType, expected := range expectedByEnv {
			seen := map[string]bool{}
			for _, observation := range byEnv[envType] {
				seen[observation.instanceID] = true
			}
			if len(seen) != 0 && len(seen) != len(expected) {
				findings = append(findings, stackConfigBlocker(appName, "", "stack cron "+name, fmt.Sprintf("instances with target environment type %s do not use the same schedule", envType)))
				conflict = true
			}
		}
		if conflict {
			continue
		}
		expectedTotal := 0
		for _, expected := range expectedByEnv {
			expectedTotal += len(expected)
		}
		if len(uniqueStackCronInstances(all)) == expectedTotal {
			result = append(result, all[0].cron)
			continue
		}
		envTypes := make([]string, 0, len(byEnv))
		for envType := range byEnv {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		for _, envType := range envTypes {
			cron := byEnv[envType][0].cron
			scope := envType
			cron.EnvType = &scope
			result = append(result, cron)
		}
	}
	return result, findings
}

func disabledDefaultCronSchedules(inspection TargetStackServiceInspection) []PreparedStackCronSchedule {
	if inspection.ServiceRevision.Manifest == nil || !strings.Contains(strings.ToLower(inspection.StackService.Name), "php") {
		return nil
	}
	result := []PreparedStackCronSchedule{}
	for _, cron := range inspection.ServiceRevision.Manifest.CronSchedules {
		if strings.TrimSpace(cron.Name) == "" || strings.TrimSpace(cron.Schedule) == "" || strings.TrimSpace(cron.Command) == "" {
			continue
		}
		result = append(result, PreparedStackCronSchedule{
			Name: cron.Name, Title: cron.Title, Crontab: cron.Schedule, Command: cron.Command, Disabled: true,
		})
	}
	return result
}

func commonStackEnvValue(items []stackEnvObservation) (string, bool, bool) {
	if len(items) == 0 {
		return "", false, true
	}
	value, secret := items[0].value, items[0].secret
	for _, item := range items[1:] {
		if item.value != value || item.secret != secret {
			return "", false, false
		}
	}
	return value, secret, true
}

func uniqueStackEnvInstances(items []stackEnvObservation) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {
		result[item.instanceID] = true
	}
	return result
}

func uniqueStackCronInstances(items []stackCronObservation) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {
		result[item.instanceID] = true
	}
	return result
}

func normalizedTargetEnvType(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "DEV"
	}
	return value
}

func stackConfigBlocker(app, instance, subject, message string) ReviewItem {
	return ReviewItem{Severity: SeverityBlocking, App: app, Instance: instance, Subject: subject, Message: message}
}

func sortPreparedStackServiceConfiguration(configuration *PreparedStackServiceConfiguration) {
	sort.SliceStable(configuration.EnvVars, func(i, j int) bool {
		left, right := configuration.EnvVars[i], configuration.EnvVars[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return optionalStringValue(left.EnvType) < optionalStringValue(right.EnvType)
	})
	sort.SliceStable(configuration.CronSchedules, func(i, j int) bool {
		left, right := configuration.CronSchedules[i], configuration.CronSchedules[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return optionalStringValue(left.EnvType) < optionalStringValue(right.EnvType)
	})
	sort.SliceStable(configuration.Integrations, func(i, j int) bool {
		return configuration.Integrations[i].Name < configuration.Integrations[j].Name
	})
	sort.SliceStable(configuration.Links, func(i, j int) bool {
		return configuration.Links[i].Name < configuration.Links[j].Name
	})
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stackConfigurationHasChanges(configuration PreparedStackConfiguration) bool {
	for _, service := range configuration.Services {
		if len(service.VersionOptions) != 0 || len(service.EnvVars) != 0 || len(service.Settings) != 0 || len(service.CronSchedules) != 0 || len(service.Integrations) != 0 || len(service.Links) != 0 {
			return true
		}
	}
	return false
}

func appUsesExplicitTargetStack(plan *AppPlan) bool {
	if plan == nil {
		return false
	}
	for _, instance := range plan.Instances {
		if !instance.Stack.CreateTarget {
			return true
		}
	}
	return false
}
