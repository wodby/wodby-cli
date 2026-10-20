package wodby1

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Wodby 1 lets a custom environment variable be set to an empty value; Wodby 2
// stores no empty values. Wodby's images read their variables through gotpl's
// getenv, which returns the default for an empty variable as for an unset one,
// so leaving the variable unset in Wodby 2 keeps the behavior. That does not
// hold when Wodby 2 gives the variable a value of its own: the empty Wodby 1
// override switched that value off, and leaving it unset would switch it back
// on. The preview blocks then, and the member decides in Wodby 1.

// sourceEnvVarIsEmpty reports a Wodby 1 override with an empty value. A secret
// or protected one without a value is a redacted export, reported as such.
func sourceEnvVarIsEmpty(envVar EnvVar) bool {
	return envVar.Value == "" && !envVar.IsRedacted()
}

// sourceEnvVarLeftUnset reports an empty override that would otherwise be
// migrated, and is left unset instead.
func sourceEnvVarLeftUnset(properties map[string]interface{}, envVar EnvVar) bool {
	return sourceEnvVarWouldOtherwiseMigrate(properties, envVar) &&
		!isWodby2ReservedEnvironmentName(envVar.Name) &&
		sourceEnvVarIsEmpty(envVar)
}

func emptyEnvVarSkippedMessage(name string) string {
	return fmt.Sprintf("the Wodby 1 value of %s is empty, and Wodby 2 does not store empty values; it is left unset, which Wodby images treat the same as empty", name)
}

// emptyEnvVarTargetDefaultFindings blocks the empty overrides of a source
// service whose variable the target stack service, or its stack, sets to a
// value.
func emptyEnvVarTargetDefaultFindings(appName string, source Instance, service Service, target TargetStackServiceInspection, stackManifest string) []ReviewItem {
	findings := []ReviewItem{}
	for _, variable := range service.EnvVars {
		if !sourceEnvVarLeftUnset(source.Properties, variable) {
			continue
		}
		value, secret, from, found := targetEnvVarDefault(target, stackManifest, variable.Name)
		if !found {
			continue
		}
		shown := fmt.Sprintf("%q", value)
		if secret {
			shown = "a secret value"
		}
		findings = append(findings, ReviewItem{
			Severity: SeverityBlocking,
			App:      appName,
			Instance: source.Name,
			Subject:  "service " + service.Name + " env var " + variable.Name,
			Message: fmt.Sprintf(
				"Wodby 1 sets %s to an empty value, but the target %s sets it to %s. Wodby 2 cannot store an empty value, so the migrated app would get %s instead. In Wodby 1, remove the override to accept that value, or set the value the app needs, then preview again",
				variable.Name, from, shown, shown,
			),
		})
	}
	return findings
}

// targetEnvVarDefault finds a non-empty value the target gives a variable: an
// env entry or a setting default in the service's manifest, or an env entry of
// the stack or of this service in the stack's manifest. from names where.
func targetEnvVarDefault(target TargetStackServiceInspection, stackManifest string, name string) (value string, secret bool, from string, found bool) {
	if manifest := target.ServiceRevision.Manifest; manifest != nil && strings.TrimSpace(manifest.Raw) != "" {
		var document interface{}
		if json.Unmarshal([]byte(manifest.Raw), &document) == nil {
			if value, secret, found = manifestEnvDefault(document, name); found {
				return value, secret, fmt.Sprintf("service %q", firstNonEmpty(target.ServiceRevision.Name, manifest.Name)), true
			}
		}
	}
	var stack map[string]interface{}
	if strings.TrimSpace(stackManifest) == "" || yaml.Unmarshal([]byte(stackManifest), &stack) != nil {
		return "", false, "", false
	}
	if value, secret, found = manifestEnvDefault(map[string]interface{}{"env": stack["env"]}, name); found {
		return value, secret, "stack", true
	}
	services, _ := stack["services"].([]interface{})
	for _, item := range services {
		service, _ := item.(map[string]interface{})
		if service == nil || service["name"] != target.StackService.Name {
			continue
		}
		if value, secret, found = manifestEnvDefault(service, name); found {
			return value, secret, fmt.Sprintf("stack service %q", target.StackService.Name), true
		}
	}
	return "", false, "", false
}

// manifestEnvDefault walks a manifest for a non-empty value of a variable: an
// item of an env list, wherever the list is, or a setting whose var is the
// variable and whose default is set.
func manifestEnvDefault(node interface{}, name string) (string, bool, bool) {
	switch node := node.(type) {
	case map[string]interface{}:
		if node["var"] == name {
			if value, ok := node["default"].(string); ok && value != "" {
				secret, _ := node["secret"].(bool)
				return value, secret, true
			}
		}
		if list, ok := node["env"].([]interface{}); ok {
			for _, item := range list {
				variable, _ := item.(map[string]interface{})
				if variable == nil || variable["name"] != name {
					continue
				}
				if value, ok := variable["value"].(string); ok && value != "" {
					secret, _ := variable["secret"].(bool)
					return value, secret, true
				}
			}
		}
		// Keys in order, so the same manifest always gives the same finding: the
		// plan's hash covers it.
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value, secret, found := manifestEnvDefault(node[key], name); found {
				return value, secret, true
			}
		}
	case []interface{}:
		for _, child := range node {
			if value, secret, found := manifestEnvDefault(child, name); found {
				return value, secret, true
			}
		}
	}
	return "", false, false
}
