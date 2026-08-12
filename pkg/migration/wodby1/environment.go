package wodby1

import "strings"

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
	value := variable.Value
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
