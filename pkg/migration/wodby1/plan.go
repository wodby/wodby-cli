package wodby1

import (
	"fmt"
	"sort"
	"strings"
)

const (
	SeverityBlocking     = "blocking"
	SeverityConfirmation = "requires_confirmation"
	SeverityManual       = "manual_follow_up"
	SeveritySkipped      = "intentionally_skipped"
)

type PlanOptions struct {
	SourceKind          string
	SourceID            string
	TargetOrg           string
	TargetProject       string
	TargetCluster       string
	TargetEnvMap        map[string]string
	AllowMissingSecrets bool
	AssumeEnvoyGateway  bool
}

type Plan struct {
	Schema  string       `json:"schema"`
	Source  PlanSource   `json:"source"`
	Target  PlanTarget   `json:"target"`
	Summary PlanSummary  `json:"summary"`
	Apps    []AppPlan    `json:"apps"`
	Review  []ReviewItem `json:"review"`
	Status  string       `json:"status"`
}

type PlanSource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type PlanTarget struct {
	Org     string `json:"org,omitempty"`
	Project string `json:"project,omitempty"`
	Cluster string `json:"cluster,omitempty"`
}

type PlanSummary struct {
	Apps          int `json:"apps"`
	Instances     int `json:"instances"`
	Services      int `json:"services"`
	Routes        int `json:"routes"`
	EnvVars       int `json:"envVars"`
	CronJobs      int `json:"cronJobs"`
	Imports       int `json:"imports"`
	Blocking      int `json:"blocking"`
	Confirmation  int `json:"requiresConfirmation"`
	Manual        int `json:"manualFollowUp"`
	Intentionally int `json:"intentionallySkipped"`
}

type AppPlan struct {
	SourceUUID string         `json:"sourceUuid"`
	Name       string         `json:"name"`
	Title      string         `json:"title"`
	Type       string         `json:"type"`
	Instances  []InstancePlan `json:"instances"`
}

type InstancePlan struct {
	SourceUUID      string        `json:"sourceUuid"`
	Name            string        `json:"name"`
	Title           string        `json:"title"`
	SourceType      string        `json:"sourceType"`
	TargetEnv       string        `json:"targetEnv,omitempty"`
	TargetEnvType   string        `json:"targetEnvType,omitempty"`
	Stack           StackPlan     `json:"stack"`
	Services        []ServicePlan `json:"services"`
	Routes          []RoutePlan   `json:"routes"`
	BasicAuth       BasicAuthPlan `json:"basicAuth"`
	CronJobs        int           `json:"cronJobs"`
	EnvVars         int           `json:"envVars"`
	Imports         int           `json:"imports"`
	TechnicalDomain string        `json:"technicalDomain"`
}

type StackPlan struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Custom  bool   `json:"custom"`
}

type ServicePlan struct {
	SourceName string `json:"sourceName"`
	TargetName string `json:"targetName,omitempty"`
	Enabled    bool   `json:"enabled"`
	Action     string `json:"action"`
	EnvVars    int    `json:"envVars"`
	CronJobs   int    `json:"cronJobs"`
}

type RoutePlan struct {
	Host           string   `json:"host"`
	Primary        bool     `json:"primary"`
	Service        string   `json:"service,omitempty"`
	PortNumber     *int     `json:"portNumber,omitempty"`
	NeedsPortID    bool     `json:"needsPortId"`
	BasicAuth      bool     `json:"basicAuth"`
	Settings       []string `json:"settings,omitempty"`
	Redirect       bool     `json:"redirect"`
	ReviewRequired bool     `json:"reviewRequired"`
}

type BasicAuthPlan struct {
	Enabled        bool `json:"enabled"`
	SecretRedacted bool `json:"secretRedacted"`
}

type ReviewItem struct {
	Severity string `json:"severity"`
	App      string `json:"app,omitempty"`
	Instance string `json:"instance,omitempty"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

func BuildPlan(export Export, opts PlanOptions) (Plan, error) {
	if err := export.Validate(); err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Schema: "wodby1-migration-plan/v1",
		Source: PlanSource{
			Kind: opts.SourceKind,
			ID:   opts.SourceID,
		},
		Target: PlanTarget{
			Org:     opts.TargetOrg,
			Project: opts.TargetProject,
			Cluster: opts.TargetCluster,
		},
	}

	for _, appExport := range export.AppExports() {
		appPlan := AppPlan{
			SourceUUID: appExport.App.UUID,
			Name:       appExport.App.Name,
			Title:      appExport.App.Title,
			Type:       appExport.App.Type,
		}
		if strings.TrimSpace(appPlan.Name) == "" {
			plan.addReview(SeverityBlocking, appPlan.Name, "", "app", "source app name is required")
		}

		for _, instance := range appExport.Instances {
			instancePlan := buildInstancePlan(&plan, appExport.App, instance, opts)
			appPlan.Instances = append(appPlan.Instances, instancePlan)
		}
		plan.Apps = append(plan.Apps, appPlan)
	}

	plan.computeSummary()
	return plan, nil
}

func buildInstancePlan(plan *Plan, app App, instance Instance, opts PlanOptions) InstancePlan {
	targetEnv, targetEnvType, ok := resolveTargetEnv(instance.Type, opts.TargetEnvMap)
	if !ok {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "env", fmt.Sprintf("source instance type %q has no default Wodby 2 env mapping; pass --target-env-map %s=TARGET_ENV", instance.Type, instance.Type))
	}

	instancePlan := InstancePlan{
		SourceUUID:      instance.UUID,
		Name:            instance.Name,
		Title:           instance.Title,
		SourceType:      instance.Type,
		TargetEnv:       targetEnv,
		TargetEnvType:   targetEnvType,
		Stack:           StackPlan{Name: instance.Stack.Name, Version: instance.Stack.Version, Custom: instance.Stack.Custom},
		TechnicalDomain: technicalDomain(app.Name, instance.Name),
	}

	if instance.Stack.Custom {
		plan.addReview(SeverityConfirmation, app.Name, instance.Name, "custom stack", "custom stack migration is best-effort and requires review before execution")
	}

	if instance.BasicAuth != nil && instance.BasicAuth.Enabled {
		instancePlan.BasicAuth.Enabled = true
		if instance.BasicAuth.Secret && instance.BasicAuth.Password == "" {
			instancePlan.BasicAuth.SecretRedacted = true
			severity := SeverityBlocking
			if opts.AllowMissingSecrets {
				severity = SeverityManual
			}
			plan.addReview(severity, app.Name, instance.Name, "basic auth", "basic-auth password is redacted; rerun with --include-secrets or accept a manual protection gap")
		}
	}

	for _, service := range instance.Services {
		servicePlan := buildServicePlan(plan, app, instance, service, opts)
		instancePlan.Services = append(instancePlan.Services, servicePlan)
		instancePlan.EnvVars += servicePlan.EnvVars
		instancePlan.CronJobs += servicePlan.CronJobs
	}

	for _, domain := range instance.Domains {
		routePlan := buildRoutePlan(plan, app, instance, domain, instancePlan.BasicAuth.Enabled, opts)
		instancePlan.Routes = append(instancePlan.Routes, routePlan)
	}

	instancePlan.Imports = len(instance.Backups)
	return instancePlan
}

func buildServicePlan(plan *Plan, app App, instance Instance, service Service, opts PlanOptions) ServicePlan {
	servicePlan := ServicePlan{
		SourceName: service.Name,
		TargetName: service.Name,
		Enabled:    service.Enabled,
		Action:     "migrate",
	}
	if !service.Enabled {
		servicePlan.Action = "skip_disabled"
		return servicePlan
	}

	switch service.Name {
	case "athenapdf":
		servicePlan.TargetName = "gotenberg"
		servicePlan.Action = "substitute"
		plan.addReview(SeverityConfirmation, app.Name, instance.Name, "service athenapdf", "athenapdf will be substituted with gotenberg when the target stack supports it")
	case "rsyslog":
		servicePlan.TargetName = ""
		servicePlan.Action = "skip"
		plan.addReview(SeveritySkipped, app.Name, instance.Name, "service rsyslog", "rsyslog is intentionally not migrated")
	}

	reportedRedacted := map[string]bool{}
	for _, envVar := range service.EnvVars {
		if !envVar.Enabled || envVar.Origin == "default" || envVar.Origin == "computed" {
			continue
		}
		servicePlan.EnvVars++
		if (envVar.Secret || envVar.Protected) && envVar.Value == "" {
			reportedRedacted[envVar.Name] = true
			severity := SeverityBlocking
			if opts.AllowMissingSecrets {
				severity = SeverityManual
			}
			plan.addReview(severity, app.Name, instance.Name, "env var "+envVar.Name, "secret or protected env var value is redacted")
		}
	}

	for _, redacted := range service.SecretsRedacted {
		if reportedRedacted[redacted] {
			continue
		}
		severity := SeverityBlocking
		if opts.AllowMissingSecrets {
			severity = SeverityManual
		}
		plan.addReview(severity, app.Name, instance.Name, "secret "+redacted, "source export reports a redacted service secret")
	}

	for _, cron := range service.CronJobs {
		if cron.Enabled {
			servicePlan.CronJobs++
		}
	}
	return servicePlan
}

func buildRoutePlan(plan *Plan, app App, instance Instance, domain Domain, basicAuth bool, opts PlanOptions) RoutePlan {
	routePlan := RoutePlan{
		Host:        domain.Name,
		Primary:     domain.Primary,
		Service:     domain.Service,
		PortNumber:  domain.PortNumber,
		NeedsPortID: domain.Service != "" && domain.PortNumber != nil,
		BasicAuth:   basicAuth || domain.Protected,
	}

	if domain.Service == "" || domain.PortNumber == nil {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityConfirmation, app.Name, instance.Name, "route "+domain.Name, "route target service or port number is missing; target app-port ID cannot be resolved")
	}

	if domain.SSLRequired != nil {
		routePlan.Settings = append(routePlan.Settings, "HTTPS_REDIRECT")
		routePlan.ReviewRequired = true
		plan.addReview(SeverityManual, app.Name, instance.Name, "route "+domain.Name, "HTTPS_REDIRECT is a route setting and cannot be set through current REST endpoints")
	}
	if domain.Indexed != nil && !*domain.Indexed {
		routePlan.Settings = append(routePlan.Settings, "NO_INDEX")
		routePlan.ReviewRequired = true
		plan.addReview(SeverityManual, app.Name, instance.Name, "route "+domain.Name, "NO_INDEX is a route setting and cannot be set through current REST endpoints")
	}

	routePlan.Redirect = domain.RedirectToWWW || domain.RedirectNonWWW || domain.RedirectTarget != ""
	if routePlan.Redirect && !opts.AssumeEnvoyGateway {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "redirect routes require an Envoy Gateway target cluster")
	}

	sort.Strings(routePlan.Settings)
	return routePlan
}

func resolveTargetEnv(sourceType string, explicit map[string]string) (string, string, bool) {
	if target, ok := explicit[sourceType]; ok && strings.TrimSpace(target) != "" {
		return target, defaultEnvType(sourceType), true
	}
	envType := defaultEnvType(sourceType)
	if envType == "" {
		return "", "", false
	}
	return strings.ToLower(envType), envType, true
}

func defaultEnvType(sourceType string) string {
	switch strings.ToLower(sourceType) {
	case "prod":
		return "PROD"
	case "stage":
		return "STAGING"
	case "dev":
		return "DEV"
	default:
		return ""
	}
}

func technicalDomain(appName string, instanceName string) string {
	appName = strings.TrimSpace(appName)
	instanceName = strings.TrimSpace(instanceName)
	if appName == "" {
		appName = "app"
	}
	if instanceName == "" || instanceName == "prod" {
		return appName + ".wodby.local"
	}
	return appName + "-" + instanceName + ".wodby.local"
}

func (p *Plan) addReview(severity string, app string, instance string, subject string, message string) {
	p.Review = append(p.Review, ReviewItem{
		Severity: severity,
		App:      app,
		Instance: instance,
		Subject:  subject,
		Message:  message,
	})
}

func (p *Plan) computeSummary() {
	p.Summary.Apps = len(p.Apps)
	for _, app := range p.Apps {
		for _, instance := range app.Instances {
			p.Summary.Instances++
			p.Summary.Services += len(instance.Services)
			p.Summary.Routes += len(instance.Routes)
			p.Summary.EnvVars += instance.EnvVars
			p.Summary.CronJobs += instance.CronJobs
			p.Summary.Imports += instance.Imports
		}
	}
	for _, item := range p.Review {
		switch item.Severity {
		case SeverityBlocking:
			p.Summary.Blocking++
		case SeverityConfirmation:
			p.Summary.Confirmation++
		case SeverityManual:
			p.Summary.Manual++
		case SeveritySkipped:
			p.Summary.Intentionally++
		}
	}
	switch {
	case p.Summary.Blocking > 0:
		p.Status = "blocked"
	case p.Summary.Confirmation > 0 || p.Summary.Manual > 0:
		p.Status = "requires_review"
	default:
		p.Status = "clean"
	}
}
