package wodby1

import "strings"

var wodby1EnvironmentReferenceReplacements = []string{
	"WODBY_INSTANCE_NAME", "WODBY_APP_INSTANCE_NAME",
	"WODBY_INSTANCE_TYPE", "WODBY_ENV_TYPE",
	"WODBY_ENVIRONMENT_NAME", "WODBY_ENV_NAME",
	"WODBY_ENVIRONMENT_TYPE", "WODBY_ENV_TYPE",
	"WODBY_HOST_PRIMARY", "WODBY_PRIMARY_HOST",
	"WODBY_URL_PRIMARY", "WODBY_PRIMARY_URL",
	"APP_BUILD_NUM", "WODBY_BUILD_NUMBER",
}

var wodby1GeneratedEnvironmentNames = map[string]bool{
	"APP_BUILD_NUM":          true,
	"APP_ROOT":               true,
	"CONF_DIR":               true,
	"FILES_DIR":              true,
	"HTTP_ROOT":              true,
	"PHP_FPM_ENV_VARS":       true,
	"WODBY_APP_DOCROOT":      true,
	"WODBY_APP_NAME":         true,
	"WODBY_APP_ROOT":         true,
	"WODBY_APP_UUID":         true,
	"WODBY_CONF":             true,
	"WODBY_DIR_CONF":         true,
	"WODBY_ENVIRONMENT_NAME": true,
	"WODBY_ENVIRONMENT_TYPE": true,
	"WODBY_HOST_PRIMARY":     true,
	"WODBY_HOSTS":            true,
	"WODBY_INSTANCE_NAME":    true,
	"WODBY_INSTANCE_TYPE":    true,
	"WODBY_INSTANCE_UUID":    true,
	"WODBY_URL_PRIMARY":      true,
}

// migratedEnvironmentReferences updates references in customer-defined values
// and commands to the equivalent Wodby 2 runtime variables. Wodby-generated
// definitions themselves are not copied; the target platform supplies them.
func migratedEnvironmentReferences(value string) string {
	return strings.NewReplacer(wodby1EnvironmentReferenceReplacements...).Replace(value)
}

func isWodby1GeneratedEnvironmentName(name string) bool {
	return wodby1GeneratedEnvironmentNames[strings.ToUpper(strings.TrimSpace(name))]
}

func serviceTargetNamesFromPlans(plans []ServicePlan) map[string]string {
	targets := make(map[string]string, len(plans))
	for _, service := range plans {
		source := strings.ToLower(strings.TrimSpace(service.SourceName))
		target := strings.TrimSpace(service.TargetName)
		if source != "" && target != "" {
			targets[source] = target
		}
	}
	return targets
}

func serviceTargetNamesFromPrepared(prepared PreparedInstance) map[string]string {
	targets := make(map[string]string, len(prepared.Services))
	for source, service := range prepared.Services {
		source = strings.ToLower(strings.TrimSpace(source))
		target := strings.TrimSpace(service.Target.StackService.Name)
		if source != "" && target != "" {
			targets[source] = target
		}
	}
	return targets
}

// migratedEnvironmentValue applies the small set of endpoint translations
// implied by hardcoded service substitutions. It deliberately does not rewrite
// arbitrary strings: only known endpoint variables are changed.
func migratedEnvironmentValue(source Service, variable EnvVar, serviceTargets map[string]string) string {
	value := migratedEnvironmentReferences(variable.Value)
	if strings.EqualFold(strings.TrimSpace(variable.Name), "GOTENBERG_ENDPOINT") {
		if endpoint, ok := privateGotenbergEndpoint(serviceTargets); ok {
			return endpoint
		}
	}
	sourceHost, targetHost, mapped := mappedSMTPHost(source, serviceTargets)
	if !mapped {
		return value
	}

	switch strings.ToUpper(strings.TrimSpace(variable.Name)) {
	case "SMTP_HOST":
		return targetHost
	case "SMTP_PORT":
		if sourceHost == "mailhog" && strings.EqualFold(targetHost, "mailpit") && strings.TrimSpace(value) == "25" {
			return "1025"
		}
	}
	return value
}

func privateGotenbergEndpoint(serviceTargets map[string]string) (string, bool) {
	targetName, ok := mappedGotenbergTarget(serviceTargets)
	if !ok {
		return "", false
	}
	return "http://" + targetName + ":3000", true
}

func mappedGotenbergTarget(serviceTargets map[string]string) (string, bool) {
	for _, sourceName := range []string{"gotenberg", "athenapdf"} {
		targetName := strings.TrimSpace(serviceTargets[sourceName])
		if targetName != "" {
			return targetName, true
		}
	}
	return "", false
}

func mappedSMTPHost(source Service, serviceTargets map[string]string) (string, string, bool) {
	for _, variable := range source.EnvVars {
		if !variable.Enabled || !strings.EqualFold(strings.TrimSpace(variable.Name), "SMTP_HOST") {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(variable.Value))
		if host == "" {
			return "", "", false
		}
		serviceName := strings.SplitN(strings.TrimSuffix(host, "."), ".", 2)[0]
		target, ok := serviceTargets[serviceName]
		if !ok || strings.TrimSpace(target) == "" || strings.EqualFold(host, target) {
			return "", "", false
		}
		return serviceName, strings.TrimSpace(target), true
	}
	return "", "", false
}

func smtpEndpointMigrationReview(source Service, properties map[string]interface{}, serviceTargets map[string]string) (string, bool) {
	sourceHost, targetHost, mapped := mappedSMTPHost(source, serviceTargets)
	if !mapped {
		return "", false
	}

	hostMigrated := false
	portMigrated := false
	sourcePort := ""
	targetPort := ""
	for _, variable := range source.EnvVars {
		if !sourceEnvVarRequiresMigration(properties, variable) {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(variable.Name)) {
		case "SMTP_HOST":
			hostMigrated = migratedEnvironmentValue(source, variable, serviceTargets) != variable.Value
		case "SMTP_PORT":
			targetPort = migratedEnvironmentValue(source, variable, serviceTargets)
			portMigrated = targetPort != variable.Value
			sourcePort = strings.TrimSpace(variable.Value)
		}
	}
	if !hostMigrated && !portMigrated {
		return "", false
	}

	if sourcePort != "" {
		if targetPort == "" {
			targetPort = sourcePort
		}
		return "SMTP endpoint will be rewritten from " + sourceHost + ":" + sourcePort + " to " + targetHost + ":" + targetPort, true
	}
	return "SMTP host will be rewritten from " + sourceHost + " to " + targetHost, true
}
