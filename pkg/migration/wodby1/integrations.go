package wodby1

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

const (
	integrationActionResolve          = "create_or_reuse"
	integrationActionVariableProvider = "create_or_reuse_variable_provider"
	wodbyBlobIntegrationID            = 0
)

var smtpRelayEnvironmentNames = map[string]bool{
	"RELAY_HOST":            true,
	"RELAY_PORT":            true,
	"RELAY_PROTO":           true,
	"RELAY_USER":            true,
	"RELAY_PASSWORD":        true,
	"AWS_ACCESS_KEY_ID":     true,
	"AWS_SECRET_ACCESS_KEY": true,
}

type smtpRelayObservation struct {
	instanceID   string
	instanceName string
	envType      string
	values       map[string]string
	redacted     map[string]bool
}

func (c *TargetClient) prepareAppIntegrations(
	ctx context.Context,
	app *PreparedAppMigration,
	appPlan *AppPlan,
	target PlanTarget,
) ([]ReviewItem, error) {
	if app == nil || appPlan == nil {
		return nil, fmt.Errorf("prepared app and app plan are required")
	}
	findings := []ReviewItem{}
	prepared := []PreparedIntegration{}
	ci, ciFindings, err := c.prepareCIIntegration(ctx, app, target)
	if err != nil {
		return nil, err
	}
	findings = append(findings, ciFindings...)
	if ci != nil {
		prepared = append(prepared, *ci)
	}

	if target.SMTPIntegrationID > 0 {
		ownFindings, err := c.linkOwnSMTPIntegration(ctx, app, target)
		if err != nil {
			return nil, err
		}
		findings = append(findings, ownFindings...)
	}
	var smtp *PreparedIntegration
	var relayInstances map[string]bool
	var smtpFindings []ReviewItem
	if target.SMTPIntegrationID <= 0 {
		smtp, relayInstances, smtpFindings, err = c.prepareSMTPIntegration(ctx, *app)
		if err != nil {
			return nil, err
		}
	}
	findings = append(findings, smtpFindings...)
	if smtp != nil {
		prepared = append(prepared, *smtp)
		configuration := app.StackConfiguration.Services[smtp.Service]
		if relayInstances == nil {
			configuration.Integrations = append(configuration.Integrations, PreparedStackIntegrationLink{
				Name: "smtp", IntegrationKey: smtp.Key,
			})
		}
		// Only some instances used a relay: link it to their app environments
		// alone, so the others keep delivering mail without one.
		for index := range app.Instances {
			instance := &app.Instances[index]
			if relayInstances != nil && relayInstances[instance.Source.UUID] {
				instance.ServiceIntegrations = append(instance.ServiceIntegrations, PreparedAppServiceIntegration{
					ServiceName: smtp.Service, Name: "smtp", IntegrationKey: smtp.Key,
				})
			}
			removeInstanceRelayVariables(instance, smtp.Service)
		}
		// The integration is the single source of truth for relay variables.
		// Leaving copied RELAY_* variables on the stack would override it.
		filtered := configuration.EnvVars[:0]
		for _, variable := range configuration.EnvVars {
			if !smtpRelayEnvironmentNames[strings.ToUpper(strings.TrimSpace(variable.Name))] {
				filtered = append(filtered, variable)
			}
		}
		configuration.EnvVars = filtered
		sortPreparedStackServiceConfiguration(&configuration)
		app.StackConfiguration.Services[smtp.Service] = configuration
	}

	backupIntegrations, backupFindings, err := c.prepareBackupIntegrations(ctx, app, target)
	if err != nil {
		return nil, err
	}
	prepared = append(prepared, backupIntegrations...)
	findings = append(findings, backupFindings...)

	sort.SliceStable(prepared, func(i, j int) bool { return prepared[i].Key < prepared[j].Key })
	planned := make([]IntegrationPlan, 0, len(prepared))
	for _, item := range prepared {
		planned = append(planned, IntegrationPlan{
			Key: item.Key, ProviderName: item.ProviderName, ProviderID: item.ProviderID, ProviderRevID: item.ProviderRevID,
			Kind: item.Kind, Service: item.Service, Action: integrationActionResolve,
		})
	}
	existingBase := []IntegrationPlan{}
	existingShared := []IntegrationPlan{}
	for _, item := range appPlan.Integrations {
		if item.Action == integrationActionVariableProvider {
			existingShared = append(existingShared, item)
		} else {
			existingBase = append(existingBase, item)
		}
	}
	if len(existingBase) != 0 && !sameIntegrationPlans(existingBase, planned) {
		return nil, currentPlanDriftError("target integration mapping changed")
	}
	// A reviewed plan keeps its provider revisions. A newer compatible
	// revision published since then is not a change to the migration.
	if len(existingBase) != 0 {
		planned = existingBase
	}
	appPlan.Integrations = append(existingShared, planned...)
	app.Integrations = prepared
	return findings, nil
}

func (c *TargetClient) prepareCIIntegration(ctx context.Context, app *PreparedAppMigration, target PlanTarget) (*PreparedIntegration, []ReviewItem, error) {
	if app == nil {
		return nil, nil, fmt.Errorf("prepared app is required")
	}
	configurationFindings := externalCIConfigurationFindings(*app)
	if target.CIIntegrationID > 0 {
		// Without this check a mistyped ID is first rejected when the app is
		// created, after the stack and integrations already exist.
		integration, err := c.GetIntegration(ctx, target.CIIntegrationID)
		var apiErr *rest.APIError
		switch {
		case errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 403):
			configurationFindings = append(configurationFindings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Subject: "CI integration",
				Message: fmt.Sprintf("--target-ci-integration-id %d is not an integration the target API key can use", target.CIIntegrationID),
			})
		case err != nil:
			return nil, nil, err
		case integration.OrgID != target.OrgID:
			configurationFindings = append(configurationFindings, ReviewItem{
				Severity: SeverityBlocking, App: app.App.App.Name, Subject: "CI integration",
				Message: fmt.Sprintf("--target-ci-integration-id %d belongs to another organization", target.CIIntegrationID),
			})
		}
		for index := range app.Instances {
			if app.Instances[index].Workspace != nil {
				continue
			}
			app.Instances[index].CIIntegrationID = target.CIIntegrationID
			app.Instances[index].UsesWodbyCI = false
			app.Instances[index].ExternalCIOnly = true
		}
		return nil, configurationFindings, nil
	}
	usesCustomCI := false
	for index := range app.Instances {
		if app.Instances[index].Workspace != nil {
			continue
		}
		if app.Instances[index].SkipCode {
			app.Instances[index].UsesWodbyCI = false
			continue
		}
		deploymentType := strings.ToLower(strings.TrimSpace(stringProperty(app.Instances[index].Source.Properties, "deployment_type")))
		if deploymentType == "ci" && target.WodbyCI {
			// The member chose Wodby CI over the third-party CI that built
			// this instance in Wodby 1; it builds from the linked repository.
			buildSource := app.Instances[index].BuildSource
			if buildSource == nil || buildSource.Input.BuildSourceType != TargetBuildSourceConnect {
				configurationFindings = append(configurationFindings, ReviewItem{
					Severity: SeverityBlocking, App: app.App.App.Name, Instance: app.Instances[index].Source.Name, Subject: "Wodby CI",
					Message: "Wodby CI builds from the app's repository: link it through a Wodby 2 Git integration with --target-git-integration-id (and --target-repository-name when the names differ), or keep third-party CI",
				})
				continue
			}
			app.Instances[index].UsesWodbyCI = true
			continue
		}
		if deploymentType == "ci" || app.Instances[index].BuildSource == nil ||
			strings.TrimSpace(app.Instances[index].BuildSource.Input.BuildSourceType) == "" {
			usesCustomCI = true
			app.Instances[index].CIIntegrationKey = "ci"
			app.Instances[index].UsesWodbyCI = false
			app.Instances[index].ExternalCIOnly = true
			app.Instances[index].ExternalCI = prepareExternalCIGuidance(app.App.App, app.Instances[index].Source)
		}
	}
	if !usesCustomCI {
		return nil, configurationFindings, nil
	}
	provider, err := c.GetProviderByName(ctx, "custom-ci")
	if err != nil {
		return nil, nil, err
	}
	// Wodby 2 always backs these with the custom-ci provider, but naming the
	// integration after the CI provider from Wodby 1's last successful build
	// keeps it recognizable in a target org that holds several of them.
	sourceProvider, sourceProviderLabel := commonWodby1CIProvider(*app)
	integrationMessage := "instances without a usable linked Git repository, or already using external deployment, will use a create-or-reuse Custom CI integration; linked Git deployments continue to use built-in Wodby CI unless --target-ci-integration-id overrides the app"
	namePrefix := "ci"
	title := "CI for " + firstNonEmpty(app.App.App.Title, app.App.App.Name)
	if sourceProvider != "" && wodby2SupportsCIProvider(sourceProvider) {
		namePrefix = "ci-" + sourceProvider
		title = sourceProviderLabel + " for " + firstNonEmpty(app.App.App.Title, app.App.App.Name)
		integrationMessage += "; the integration will be named after " + sourceProviderLabel +
			", the CI provider reported by Wodby 1's last successful build"
	}
	findings := append(configurationFindings, ReviewItem{
		Severity: SeverityMigration, App: app.App.App.Name, Subject: "CI integration",
		Message: integrationMessage,
	}, ReviewItem{
		Severity: SeverityManual, App: app.App.App.Name, Subject: "Custom CI bootstrap build",
		Message: "after the migration creates and configures the target app, run its third-party CI pipeline once, then rerun the same --apply command; the migration will adopt the completed build and continue deployment and data import",
	})
	return &PreparedIntegration{
		Key: "ci", ProviderName: provider.Name, ProviderID: provider.ID, ProviderRevID: provider.RevID,
		Name:  migrationResourceName(namePrefix, app.App.App.Name, app.App.App.UUID),
		Title: title, Kind: "ci",
	}, findings, nil
}

// commonWodby1CIProvider resolves the single CI provider shared by every
// external-CI instance of the app. Instances that report nothing recognizable
// do not veto a provider the others agree on, but a genuine disagreement falls
// back to generic naming rather than picking an arbitrary winner.
func commonWodby1CIProvider(app PreparedAppMigration) (provider, label string) {
	for _, instance := range app.Instances {
		if !instance.ExternalCIOnly {
			continue
		}
		current, currentLabel, _ := normalizedWodby1CIProvider(
			stringProperty(instance.Source.Properties, "ci_provider"),
		)
		if current == "" {
			continue
		}
		if provider == "" {
			provider, label = current, currentLabel
			continue
		}
		if provider != current {
			return "", ""
		}
	}
	return provider, label
}

// prepareExternalCIGuidance resolves, at plan time, everything the executor
// needs to explain the manual bootstrap build. The source app and instance are
// both in scope here and are not threaded into the executor.
func prepareExternalCIGuidance(app App, instance Instance) *PreparedExternalCI {
	provider, label, examplePath := normalizedWodby1CIProvider(
		stringProperty(instance.Properties, "ci_provider"),
	)
	return &PreparedExternalCI{
		ProviderKey:       provider,
		ProviderLabel:     label,
		ProviderSupported: wodby2SupportsCIProvider(provider),
		ExampleURL:        wodbyCIExampleURL(wodbyCIExampleStack(app, instance), examplePath),
	}
}

func externalCIConfigurationFindings(app PreparedAppMigration) []ReviewItem {
	findings := []ReviewItem{}
	for _, instance := range app.Instances {
		if !strings.EqualFold(strings.TrimSpace(stringProperty(instance.Source.Properties, "deployment_type")), "ci") {
			continue
		}
		reportedProvider := strings.TrimSpace(stringProperty(instance.Source.Properties, "ci_provider"))
		provider, providerLabel, providerPath := normalizedWodby1CIProvider(reportedProvider)
		link := wodbyCIExampleURL(wodbyCIExampleStack(app.App.App, instance.Source), providerPath)

		var message string
		switch {
		case wodby2SupportsCIProvider(provider):
			// Wodby 2 has this provider, but its API token is not in the Wodby 1
			// export and cannot be recreated, so the customer has to bring one.
			message = fmt.Sprintf(
				"Wodby 1 uses %s, which Wodby 2 supports. Its API token cannot be migrated:"+
					" create a %s integration in Wodby 2 and pass --target-ci-integration-id,"+
					" otherwise Custom CI is used.",
				providerLabel, providerLabel,
			)
		case provider != "":
			message = fmt.Sprintf(
				"Wodby 1 uses %s, which Wodby 2 does not support; Custom CI is used."+
					" Adapt one of %s and run `wodby ci init --provider %s`.",
				providerLabel, wodby2CIProviderLabels, provider,
			)
		case reportedProvider != "":
			message = fmt.Sprintf(
				"Wodby 1 reports CI provider %q, which Wodby 2 does not support; Custom CI is used."+
					" Adapt one of %s.",
				reportedProvider, wodby2CIProviderLabels,
			)
		default:
			message = "Wodby 1 uses third-party CI but its last successful build reports no provider;" +
				" Custom CI is used."
		}
		message += " Update the pipeline to Wodby CLI 2.x with WODBY_API_KEY and WODBY_APP_SERVICE_ID: " + link

		findings = append(findings, ReviewItem{
			Severity: SeverityServiceWarning,
			App:      app.App.App.Name,
			Instance: instance.Source.Name,
			Subject:  "third-party CI configuration",
			Message:  message,
		})
	}
	return findings
}

// normalizedWodby1CIProvider maps a Wodby 1 build provider to its Wodby 2
// identity, label, and pipeline example.
//
// The recognized values are exactly what Wodby 1's CLI writes to a build:
// "github", "gitlab", "circleci", "travisci", "bitbucket-pipelines", and
// "jenkins". Wodby 1 also stores the literal "Unknown" when it detected no CI
// environment, and `wodby ci init --provider` accepts free text, so the spaced
// spellings a person is likely to type are accepted too. Anything else is
// reported as unrecognized rather than guessed at.
//
// An empty examplePath means Wodby 2 ships no pipeline example for that
// provider. Wodby CI 1.0 had bitbucket and travis examples; the 2.0 repository
// covers GitHub Actions, GitLab CI, and CircleCI only, and Wodby 1 autodetects
// Jenkins without ever having had one. Callers must say so instead of linking a
// stack directory that holds nothing for the customer's provider.
func normalizedWodby1CIProvider(value string) (provider, label, examplePath string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "circle", "circleci", "circle ci":
		return "circleci", "CircleCI", "circleci/config.yml"
	case "gitlab", "gitlab-ci", "gitlab ci":
		return "gitlab", "GitLab CI", "gitlab-ci/.gitlab-ci.yml"
	case "github", "github-actions", "github actions":
		return "github", "GitHub Actions", "github-actions/wodby.yml"
	case "travis", "travisci", "travis-ci", "travis ci":
		return "travisci", "Travis CI", ""
	case "bitbucket", "bitbucket-pipelines", "bitbucket pipelines":
		return "bitbucket-pipelines", "Bitbucket Pipelines", ""
	case "jenkins":
		return "jenkins", "Jenkins", ""
	default:
		return "", "", ""
	}
}

// wodby2CIProviderLabels lists the providers Wodby 2 has a CI provider for.
// They are also the only ones it ships examples for and that Wodby CLI 2.x
// autodetects. A pipeline on anything else migrates as Custom CI, adapts one of
// these examples, and passes `wodby ci init --provider`.
const wodby2CIProviderLabels = "GitHub Actions, GitLab CI, or CircleCI"

// wodby2SupportsCIProvider reports whether Wodby 2 has a real CI provider for a
// Wodby 1 provider key. Bitbucket Pipelines, Travis CI, and Jenkins are
// recognized on the Wodby 1 side but have no Wodby 2 counterpart, so they
// migrate as Custom CI like an unidentified provider does.
func wodby2SupportsCIProvider(provider string) bool {
	switch provider {
	case "github", "gitlab", "circleci":
		return true
	default:
		return false
	}
}

func wodbyCIExampleStack(app App, instance Instance) string {
	candidates := []string{app.Type, instance.Stack.Type, instance.Stack.Name, instance.Stack.AncestorName}
	for _, candidate := range candidates {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		for _, stack := range []string{"wordpress", "drupal", "laravel", "matomo", "django", "rails", "nextjs", "python", "static", "node", "php", "go"} {
			if strings.Contains(normalized, stack) {
				return stack
			}
		}
	}
	return ""
}

// removeInstanceRelayVariables drops relay variables from an instance's
// service overrides: the SMTP integration supplies them, and a copied value
// would override it.
func removeInstanceRelayVariables(instance *PreparedInstance, targetService string) {
	for sourceName, mapping := range instance.Services {
		if mapping.Target.StackService.Name != targetService || len(mapping.InstanceEnvVars) == 0 {
			continue
		}
		kept := []EnvVar{}
		for _, variable := range mapping.InstanceEnvVars {
			if !smtpRelayEnvironmentNames[strings.ToUpper(strings.TrimSpace(variable.Name))] {
				kept = append(kept, variable)
			}
		}
		mapping.InstanceEnvVars = kept
		instance.Services[sourceName] = mapping
	}
}

// linkOwnSMTPIntegration relays the mail of every migrated instance that
// sends through OpenSMTPD via the member's own SMTP integration, instead of a
// relay copied from Wodby 1. It links the integration to their app
// environments, not the shared stack, which other app environments use too.
func (c *TargetClient) linkOwnSMTPIntegration(ctx context.Context, app *PreparedAppMigration, target PlanTarget) ([]ReviewItem, error) {
	integration, err := c.GetIntegration(ctx, target.SMTPIntegrationID)
	var apiErr *rest.APIError
	switch {
	case errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 403):
		return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "SMTP integration",
			fmt.Sprintf("--target-smtp-integration-id %d is not an integration the target API key can use", target.SMTPIntegrationID))}, nil
	case err != nil:
		return nil, err
	case integration.OrgID != target.OrgID:
		return []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "SMTP integration",
			fmt.Sprintf("--target-smtp-integration-id %d belongs to another organization", target.SMTPIntegrationID))}, nil
	}
	names := []string{}
	for index := range app.Instances {
		instance := &app.Instances[index]
		sends := false
		for _, source := range instance.Source.Services {
			mapping, ok := instance.Services[source.Name]
			if ok && source.Enabled && mapping.Target.StackService.Name == smtpRelayTargetService {
				sends = true
			}
		}
		if !sends {
			continue
		}
		instance.ServiceIntegrations = append(instance.ServiceIntegrations, PreparedAppServiceIntegration{
			ServiceName: smtpRelayTargetService, Name: "smtp", IntegrationID: target.SMTPIntegrationID,
		})
		removeInstanceRelayVariables(instance, smtpRelayTargetService)
		names = append(names, instance.Source.Name)
	}
	if len(names) == 0 {
		return []ReviewItem{{
			Severity: SeverityConfirmation, App: app.App.App.Name, Subject: "SMTP integration",
			Message: fmt.Sprintf("no migrated instance sends mail through OpenSMTPD, so SMTP integration ID %d is not used", target.SMTPIntegrationID),
		}}, nil
	}
	sort.Strings(names)
	return []ReviewItem{{
		Severity: SeverityMigration, App: app.App.App.Name, Subject: "SMTP integration",
		Message: fmt.Sprintf("mail of %s will be relayed through SMTP integration ID %d instead of a relay copied from Wodby 1", strings.Join(names, ", "), target.SMTPIntegrationID),
	}}, nil
}

// smtpRelayTargetService is the Wodby 2 service that relays mail.
const smtpRelayTargetService = "opensmtpd"

// prepareSMTPIntegration converts OpenSMTPD relays into one SMTP integration.
// When only some instances of the app, migrated or read for context, use a
// relay, it returns the migrated ones that do; the integration is then linked
// to their app environments instead of the shared stack.
func (c *TargetClient) prepareSMTPIntegration(ctx context.Context, app PreparedAppMigration) (*PreparedIntegration, map[string]bool, []ReviewItem, error) {
	observations := []smtpRelayObservation{}
	expected := []smtpRelayObservation{}
	contextWithoutRelay := false
	for _, instance := range app.ContextInstances {
		for _, source := range instance.Source.Services {
			mapping, ok := instance.Services[source.Name]
			if !ok || !source.Enabled || mapping.Target.StackService.Name != "opensmtpd" {
				continue
			}
			if !sourceRelayConfigured(source) {
				contextWithoutRelay = true
			}
		}
	}
	for _, instance := range app.Instances {
		for _, source := range instance.Source.Services {
			mapping, ok := instance.Services[source.Name]
			if !ok || !source.Enabled || mapping.Target.StackService.Name != "opensmtpd" {
				continue
			}
			observation := smtpRelayObservation{
				instanceID: instance.Source.UUID, instanceName: instance.Source.Name,
				envType: normalizedTargetEnvType(instance.TargetEnvType),
				values:  map[string]string{}, redacted: map[string]bool{},
			}
			for _, variable := range source.EnvVars {
				name := strings.ToUpper(strings.TrimSpace(variable.Name))
				if !variable.Enabled || !smtpRelayEnvironmentNames[name] {
					continue
				}
				if variable.IsRedacted() {
					observation.redacted[name] = true
					continue
				}
				observation.values[name] = strings.TrimSpace(variable.Value)
			}
			expected = append(expected, observation)
			if observation.values["RELAY_HOST"] != "" || observation.redacted["RELAY_HOST"] {
				observations = append(observations, observation)
			}
		}
	}
	if len(observations) == 0 {
		return nil, nil, nil, nil
	}
	findings := []ReviewItem{}
	var relayInstances map[string]bool
	if len(observations) != len(expected) || contextWithoutRelay {
		relayInstances = map[string]bool{}
		names := []string{}
		for _, observation := range observations {
			relayInstances[observation.instanceID] = true
			names = append(names, observation.instanceName)
		}
		sort.Strings(names)
		findings = append(findings, ReviewItem{
			Severity: SeverityMigration, App: app.App.App.Name, Subject: "SMTP relay integration",
			Message: fmt.Sprintf("only some instances of the app use an OpenSMTPD relay; the SMTP integration will be linked to the app environments of %s only, and the others keep delivering mail without a relay", strings.Join(names, ", ")),
		})
	}
	for _, observation := range observations {
		for name := range observation.redacted {
			findings = append(findings, stackConfigBlocker(app.App.App.Name, observation.instanceName, "SMTP relay "+name, "protected source value is redacted; create a fresh Wodby 1 export with secret access"))
		}
	}
	if containsBlockingReviewItems(findings) {
		return nil, nil, findings, nil
	}

	providerName := commonSMTPProvider(observations)
	provider, err := c.GetProviderByName(ctx, providerName)
	if err != nil {
		return nil, nil, nil, err
	}
	fieldNames := smtpProviderFields(providerName)
	fields, fieldFindings := aggregateIntegrationFields(app.App.App.Name, "SMTP relay", observations, fieldNames)
	findings = append(findings, fieldFindings...)
	if containsBlockingReviewItems(findings) {
		return nil, nil, findings, nil
	}
	var scope *string
	if providerName == "aws" {
		region := smtpSESRegion(observations[0].values["RELAY_HOST"])
		for _, observation := range observations[1:] {
			if smtpSESRegion(observation.values["RELAY_HOST"]) != region {
				findings = append(findings, stackConfigBlocker(app.App.App.Name, observation.instanceName, "SMTP relay region", "AWS SES regions differ across instances and cannot be represented by one integration"))
			}
		}
		if containsBlockingReviewItems(findings) {
			return nil, nil, findings, nil
		}
		scope = &region
	}
	findings = append(findings, ReviewItem{
		Severity: SeverityMigration, App: app.App.App.Name, Subject: "SMTP relay integration",
		Message: fmt.Sprintf("the %s SMTP credentials will be resolved server-side, reusing an existing matching integration or creating %q; relay secrets remain outside the plan and state files", providerName, "SMTP for "+app.App.App.Title),
	})
	kind := "smtp"
	if providerName == "aws" {
		kind = "ses"
	}
	return &PreparedIntegration{
		Key: "smtp", ProviderName: provider.Name, ProviderID: provider.ID, ProviderRevID: provider.RevID,
		Name: migrationResourceName("smtp", app.App.App.Name, app.App.App.UUID), Title: "SMTP for " + firstNonEmpty(app.App.App.Title, app.App.App.Name),
		Kind: kind, Service: "opensmtpd", Scope: scope, Fields: fields,
	}, relayInstances, findings, nil
}

// sourceRelayConfigured reports whether an OpenSMTPD service relays mail.
func sourceRelayConfigured(source Service) bool {
	for _, variable := range source.EnvVars {
		if variable.Enabled && strings.EqualFold(strings.TrimSpace(variable.Name), "RELAY_HOST") &&
			(strings.TrimSpace(variable.Value) != "" || variable.IsRedacted()) {
			return true
		}
	}
	return false
}

func commonSMTPProvider(observations []smtpRelayObservation) string {
	provider := ""
	for _, observation := range observations {
		current := smtpProvider(observation.values)
		if provider == "" {
			provider = current
		} else if provider != current {
			return "custom-smtp"
		}
	}
	if provider == "" {
		return "custom-smtp"
	}
	return provider
}

func smtpProvider(values map[string]string) string {
	host := strings.ToLower(strings.TrimSpace(values["RELAY_HOST"]))
	switch host {
	case "smtp-relay.brevo.com", "smtp-relay.sendinblue.com":
		if values["RELAY_USER"] != "" && values["RELAY_PASSWORD"] != "" {
			return "brevo"
		}
	}
	if smtpSESRegion(host) != "" && values["AWS_ACCESS_KEY_ID"] != "" && values["AWS_SECRET_ACCESS_KEY"] != "" {
		return "aws"
	}
	return "custom-smtp"
}

func smtpSESRegion(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	const prefix, suffix = "email-smtp.", ".amazonaws.com"
	if !strings.HasPrefix(host, prefix) || !strings.HasSuffix(host, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, prefix), suffix)
}

func smtpProviderFields(provider string) map[string]string {
	switch provider {
	case "brevo":
		return map[string]string{"login": "RELAY_USER", "key": "RELAY_PASSWORD"}
	case "aws":
		return map[string]string{"aws_access_key_id": "AWS_ACCESS_KEY_ID", "aws_secret_access_key": "AWS_SECRET_ACCESS_KEY"}
	default:
		return map[string]string{"host": "RELAY_HOST", "port": "RELAY_PORT", "protocol": "RELAY_PROTO", "username": "RELAY_USER", "password": "RELAY_PASSWORD"}
	}
}

func aggregateIntegrationFields(appName, subject string, observations []smtpRelayObservation, fields map[string]string) ([]TargetIntegrationFieldInput, []ReviewItem) {
	result := []TargetIntegrationFieldInput{}
	findings := []ReviewItem{}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, field := range names {
		envName := fields[field]
		byEnv := map[string][]string{}
		all := []string{}
		for _, observation := range observations {
			value := observation.values[envName]
			byEnv[observation.envType] = append(byEnv[observation.envType], value)
			all = append(all, value)
		}
		if allStringsEqual(all) {
			if all[0] != "" {
				result = append(result, TargetIntegrationFieldInput{Name: field, Value: all[0]})
			}
			continue
		}
		envTypes := make([]string, 0, len(byEnv))
		for envType := range byEnv {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		for _, envType := range envTypes {
			values := byEnv[envType]
			if !allStringsEqual(values) {
				findings = append(findings, stackConfigBlocker(appName, "", subject+" field "+field, fmt.Sprintf("instances with target environment type %s use different values", envType)))
				continue
			}
			if values[0] == "" {
				continue
			}
			scope := envType
			result = append(result, TargetIntegrationFieldInput{Name: field, Value: values[0], EnvType: &scope})
		}
	}
	return result, findings
}

func (c *TargetClient) prepareBackupIntegrations(ctx context.Context, app *PreparedAppMigration, target PlanTarget) ([]PreparedIntegration, []ReviewItem, error) {
	prepared := []PreparedIntegration{}
	findings := []ReviewItem{}
	timeZone := strings.TrimSpace(target.OrgDefaultTimeZone)
	if timeZone == "" {
		timeZone = "UTC"
	}
	autoBackups := target.OrgCapabilities != nil && target.OrgCapabilities.AutoBackups
	window, windowStart, windowEnd, windowMinutes := "02:00-05:00", "", "", 0
	if strings.TrimSpace(target.BackupWindow) != "" {
		var err error
		windowStart, windowEnd, windowMinutes, err = ParseBackupWindow(target.BackupWindow)
		if err != nil {
			return nil, nil, err
		}
		window = windowStart + "-" + windowEnd
	}
	ownStorage := target.BackupIntegrationID > 0
	if ownStorage {
		// Without this check a mistyped ID is first rejected when the presets
		// are created, after the app already exists.
		integration, err := c.GetIntegration(ctx, target.BackupIntegrationID)
		var apiErr *rest.APIError
		switch {
		case errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 403):
			return nil, []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "backup destination",
				fmt.Sprintf("--target-backup-integration-id %d is not an integration the target API key can use", target.BackupIntegrationID))}, nil
		case err != nil:
			return nil, nil, err
		case integration.OrgID != target.OrgID:
			return nil, []ReviewItem{stackConfigBlocker(app.App.App.Name, "", "backup destination",
				fmt.Sprintf("--target-backup-integration-id %d belongs to another organization", target.BackupIntegrationID))}, nil
		}
	}
	for index := range app.Instances {
		instance := &app.Instances[index]
		config := instance.Source.BackupConfig
		configReported := config != nil
		if config == nil {
			config = &BackupConfig{}
		}
		providerName := strings.ToLower(strings.TrimSpace(config.Provider))
		destination := &PreparedBackupDestination{
			Auto: config.Enabled, TimeZone: timeZone,
			WindowStart: windowStart, WindowEnd: windowEnd, WindowMinutes: windowMinutes,
		}
		if ownStorage {
			// The member's own storage replaces Wodby Blob and a copy of the
			// Wodby 1 mirror alike.
			if !autoBackups {
				findings = append(findings, stackConfigBlocker(app.App.App.Name, instance.Source.Name, "backup destination", "backups to your own storage integration need the automatic-backups feature; upgrade the target subscription or keep Wodby Blob storage"))
				continue
			}
			destination.IntegrationID = target.BackupIntegrationID
			destination.Bucket = strings.TrimSpace(target.BackupBucket)
			if !configReported {
				destination.Auto, destination.Disabled = true, true
			}
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "backup destination",
				Message: fmt.Sprintf("backup presets will use storage integration ID %d, bucket %q, with the daily %s window", target.BackupIntegrationID, destination.Bucket, window),
			})
			findings = append(findings, applyBackupComponentSchedules(app.App.App.Name, instance.Source.Name, *config, destination)...)
			instance.BackupDestination = destination
			continue
		}
		switch providerName {
		case "", "wodby", "wodby_cloud", "wodby-cloud":
			usesDefaultBlob := providerName == ""
			destination.IntegrationID = wodbyBlobIntegrationID
			destinationDescription := "Wodby 1 uses Wodby Cloud storage, so target backup presets will use Wodby Blob storage"
			if usesDefaultBlob {
				destinationDescription = "no Wodby 1 backup mirror is configured, so target backup presets will use Wodby Blob storage"
			}
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "backup destination",
				Message: destinationDescription,
			})
			if !configReported {
				destination.Auto = true
				destination.Disabled = true
				findings = append(findings, ReviewItem{
					Severity: SeverityConfirmation, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "automatic backups",
					Message: "the source export does not report whether automatic backups are enabled, so the target preset will be created disabled and must be reviewed",
				})
			} else if config.Enabled && !autoBackups {
				destination.Disabled = true
				findings = append(findings, ReviewItem{
					Severity: SeverityConfirmation, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "automatic backups",
					Message: "Wodby 1 automatic backups are enabled, but the target subscription does not allow them, so the automatic-backup presets will be created disabled and can be enabled after upgrading",
				})
			} else if !config.Enabled && !autoBackups {
				// Free organizations may persist only disabled Wodby Blob presets.
				// Preserve the destination for a later upgrade without claiming
				// that Wodby 1 automatic backups were enabled.
				destination.Auto = true
				destination.Disabled = true
				findings = append(findings, ReviewItem{
					Severity: SeverityConfirmation, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "automatic backups",
					Message: "Wodby 1 automatic backups are not enabled, and the target subscription permits only disabled Wodby Blob presets, so the presets will be staged disabled for later review",
				})
			} else {
				message := "Wodby 1 automatic backups are not enabled, so manual backup presets will be created"
				if config.Enabled {
					message = "Wodby 1 automatic backups are enabled, so enabled automatic-backup presets will be created with the daily " + window + " window"
				}
				findings = append(findings, ReviewItem{
					Severity: SeverityMigration, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "automatic backups",
					Message: message,
				})
			}
			findings = append(findings, applyBackupComponentSchedules(app.App.App.Name, instance.Source.Name, *config, destination)...)
		case "aws_s3", "aws-s3", "s3":
			if !autoBackups {
				findings = append(findings, stackConfigBlocker(app.App.App.Name, instance.Source.Name, "backup destination", "the source uses an S3 backup destination, but target backup presets require the automatic-backups feature; upgrade the target subscription or migrate this preset manually"))
				continue
			}
			if config.SecretRedacted || strings.TrimSpace(config.SecretAccessKey) == "" {
				findings = append(findings, stackConfigBlocker(app.App.App.Name, instance.Source.Name, "backup S3 credentials", "the protected S3 secret access key is redacted or empty; create a fresh Wodby 1 export with secret access"))
				continue
			}
			if strings.TrimSpace(config.AccessKeyID) == "" || strings.TrimSpace(config.Region) == "" || strings.TrimSpace(config.Bucket) == "" {
				findings = append(findings, stackConfigBlocker(app.App.App.Name, instance.Source.Name, "backup S3 destination", "S3 region, bucket, and access key ID are required"))
				continue
			}
			provider, err := c.GetProviderByName(ctx, "aws")
			if err != nil {
				return nil, nil, err
			}
			key := "backup-" + shortDigest(app.App.App.UUID, instance.Source.UUID)
			region := strings.TrimSpace(config.Region)
			resourceName := app.App.App.Name
			resourceTitle := firstNonEmpty(app.App.App.Title, app.App.App.Name)
			if len(app.Instances) > 1 {
				resourceName += "-" + instance.Source.Name
				resourceTitle += " (" + firstNonEmpty(instance.Source.Title, instance.Source.Name) + ")"
			}
			prepared = append(prepared, PreparedIntegration{
				Key: key, ProviderName: provider.Name, ProviderID: provider.ID, ProviderRevID: provider.RevID,
				Name:  migrationResourceName("backup", resourceName, app.App.App.UUID, instance.Source.UUID),
				Title: "Backup storage for " + resourceTitle,
				Kind:  "s3", Scope: &region,
				Fields: []TargetIntegrationFieldInput{
					{Name: "aws_access_key_id", Value: config.AccessKeyID},
					{Name: "aws_secret_access_key", Value: config.SecretAccessKey},
				},
			})
			destination.IntegrationKey = key
			destination.Bucket = strings.TrimSpace(config.Bucket)
			findings = append(findings, applyBackupComponentSchedules(app.App.App.Name, instance.Source.Name, *config, destination)...)
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: app.App.App.Name, Instance: instance.Source.Name, Subject: "backup destination",
				Message: "the AWS S3 credentials will be resolved server-side and reused when they match an existing integration; the backup preset will use the daily " + window + " window",
			})
		default:
			findings = append(findings, stackConfigBlocker(app.App.App.Name, instance.Source.Name, "backup destination", fmt.Sprintf("source backup provider %q is not supported automatically", config.Provider)))
			continue
		}
		instance.BackupDestination = destination
	}
	return prepared, findings, nil
}

// backupComponentCapabilities maps a Wodby 1 backup component to the backup
// capability names Wodby 2 services use for the same data.
var backupComponentCapabilities = map[string][]string{
	"db":    {"database", "db"},
	"files": {"files"},
}

// applyBackupComponentSchedules carries Wodby 1's per-component automatic
// backup schedules over where Wodby 2 can represent them. A component whose
// schedule is off gets a disabled preset. Wodby 2 runs an automatic preset
// once a day in its time window, so a schedule that runs more often or at
// another time is reported for review instead.
func applyBackupComponentSchedules(appName, instanceName string, config BackupConfig, destination *PreparedBackupDestination) []ReviewItem {
	if !config.Enabled || !destination.Auto || len(config.Schedules) == 0 {
		return nil
	}
	findings := []ReviewItem{}
	components := make([]string, 0, len(config.Schedules))
	for component := range config.Schedules {
		components = append(components, component)
	}
	sort.Strings(components)
	for _, component := range components {
		schedule := config.Schedules[component]
		label := component
		if component == "db" {
			label = "database"
		}
		if !schedule.Enabled {
			if destination.DisabledBackups == nil {
				destination.DisabledBackups = map[string]bool{}
			}
			for _, capability := range backupComponentCapabilities[component] {
				destination.DisabledBackups[capability] = true
			}
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration, App: appName, Instance: instanceName, Subject: "automatic backups",
				Message: fmt.Sprintf("Wodby 1 does not back up %s automatically for this instance, so its target preset will be created disabled", label),
			})
			continue
		}
		crontab := strings.Join(strings.Fields(schedule.Crontab), " ")
		if crontab == "" {
			continue
		}
		fields := strings.Fields(crontab)
		daily := len(fields) == 5 && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" &&
			isPlainNumber(fields[0]) && isPlainNumber(fields[1])
		if !daily {
			findings = append(findings, ReviewItem{
				Severity: SeverityServiceWarning, App: appName, Instance: instanceName, Subject: "automatic backups",
				Message: fmt.Sprintf("Wodby 1 backs up %s on schedule %q; Wodby 2 runs automatic backups once a day in the %s window, so review the target preset and create additional backups if you need them more often", label, crontab, destination.window()),
			})
		}
	}
	return findings
}

func isPlainNumber(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sameIntegrationPlans compares integration choices. Provider revisions are not
// compared: Wodby 2 moves integrations to newer compatible revisions itself.
func sameIntegrationPlans(left, right []IntegrationPlan) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]IntegrationPlan(nil), left...)
	right = append([]IntegrationPlan(nil), right...)
	sort.SliceStable(left, func(i, j int) bool { return left[i].Key < left[j].Key })
	sort.SliceStable(right, func(i, j int) bool { return right[i].Key < right[j].Key })
	for index := range left {
		if left[index].Key != right[index].Key || left[index].ProviderName != right[index].ProviderName ||
			left[index].ProviderID != right[index].ProviderID || left[index].Kind != right[index].Kind ||
			left[index].Service != right[index].Service || left[index].Action != right[index].Action ||
			!sameStringSet(left[index].Variables, right[index].Variables) {
			return false
		}
	}
	return true
}

func allStringsEqual(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return false
		}
	}
	return true
}

func containsBlockingReviewItems(items []ReviewItem) bool {
	for _, item := range items {
		if item.Severity == SeverityBlocking {
			return true
		}
	}
	return false
}

func migrationResourceName(prefix, label string, identity ...string) string {
	name := strings.ToLower(strings.TrimSpace(prefix + "-" + label))
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if len(identity) != 0 {
		name += "-" + shortDigest(identity...)
	}
	if len(name) > 50 {
		name = strings.TrimRight(name[:33], "-") + "-" + shortDigest(prefix, label, strings.Join(identity, ":"))
	}
	return name
}
