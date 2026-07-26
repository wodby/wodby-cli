package wodby1

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
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
	TargetAdminVerified bool
	TargetScope         *TargetScopeDiscovery
	TargetEnvs          map[string]TargetEnv
}

type Plan struct {
	Schema   string       `json:"schema"`
	PlanHash string       `json:"planHash"`
	Source   PlanSource   `json:"source"`
	Target   PlanTarget   `json:"target"`
	Summary  PlanSummary  `json:"summary"`
	Apps     []AppPlan    `json:"apps"`
	Review   []ReviewItem `json:"review"`
	Status   string       `json:"status"`
}

type PlanSource struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	Schema         string `json:"schema"`
	GeneratedAt    int64  `json:"generatedAt,omitempty"`
	ExportDigest   string `json:"exportDigest"`
	ResponseDigest string `json:"responseDigest,omitempty"`
}

type PlanTarget struct {
	Org               string                     `json:"org,omitempty"`
	OrgID             int                        `json:"orgId,omitempty"`
	OrgName           string                     `json:"orgName,omitempty"`
	Project           string                     `json:"project,omitempty"`
	ProjectID         int                        `json:"projectId,omitempty"`
	ProjectName       string                     `json:"projectName,omitempty"`
	Cluster           string                     `json:"cluster,omitempty"`
	ClusterID         int                        `json:"clusterId,omitempty"`
	ClusterName       string                     `json:"clusterName,omitempty"`
	ClusterStatus     string                     `json:"clusterStatus,omitempty"`
	AdminVerified     bool                       `json:"adminVerified"`
	DiscoveryVerified bool                       `json:"discoveryVerified"`
	Capabilities      *TargetClusterCapabilities `json:"capabilities,omitempty"`
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
	SourceUUID    string          `json:"sourceUuid"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Type          string          `json:"type"`
	SourceStatus  string          `json:"sourceStatus,omitempty"`
	SourceCreated int64           `json:"sourceCreated,omitempty"`
	SourceUpdated int64           `json:"sourceUpdated,omitempty"`
	Repository    *RepositoryPlan `json:"repository,omitempty"`
	Instances     []InstancePlan  `json:"instances"`
}

type RepositoryPlan struct {
	SourceUUID          string `json:"sourceUuid"`
	Title               string `json:"title"`
	URL                 string `json:"url,omitempty"`
	CredentialsRedacted bool   `json:"credentialsRedacted,omitempty"`
	SourceStatus        string `json:"sourceStatus,omitempty"`
}

type InstancePlan struct {
	SourceUUID    string        `json:"sourceUuid"`
	Name          string        `json:"name"`
	Title         string        `json:"title"`
	SourceType    string        `json:"sourceType"`
	SourceStatus  string        `json:"sourceStatus,omitempty"`
	SourceUpdated int64         `json:"sourceUpdated,omitempty"`
	TargetEnv     string        `json:"targetEnv,omitempty"`
	TargetEnvID   int           `json:"targetEnvId,omitempty"`
	TargetEnvType string        `json:"targetEnvType,omitempty"`
	Stack         StackPlan     `json:"stack"`
	Services      []ServicePlan `json:"services"`
	Routes        []RoutePlan   `json:"routes"`
	BasicAuth     BasicAuthPlan `json:"basicAuth"`
	CronJobs      int           `json:"cronJobs"`
	EnvVars       int           `json:"envVars"`
	Imports       int           `json:"imports"`
}

type StackPlan struct {
	UUID         string `json:"uuid,omitempty"`
	Name         string `json:"name"`
	Version      string `json:"version,omitempty"`
	Custom       bool   `json:"custom"`
	AncestorUUID string `json:"ancestorUuid,omitempty"`
	AncestorName string `json:"ancestorName,omitempty"`
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
	SourceUUID      string             `json:"sourceUuid,omitempty"`
	Host            string             `json:"host"`
	Type            string             `json:"type,omitempty"`
	Status          string             `json:"status,omitempty"`
	Enabled         bool               `json:"enabled"`
	Action          string             `json:"action"`
	Primary         bool               `json:"primary"`
	Indexed         *bool              `json:"indexed,omitempty"`
	SSL             bool               `json:"ssl"`
	SSLRequired     *bool              `json:"sslRequired,omitempty"`
	SSLCustom       bool               `json:"sslCustom"`
	HSTS            bool               `json:"hsts"`
	HSTSSubdomains  bool               `json:"hstsSubdomains"`
	Protected       bool               `json:"protected"`
	Service         string             `json:"service,omitempty"`
	ServiceProtocol string             `json:"serviceProtocol,omitempty"`
	PortNumber      *int               `json:"portNumber,omitempty"`
	NeedsPortID     bool               `json:"needsPortId"`
	BasicAuth       bool               `json:"basicAuth"`
	Settings        []RouteSettingPlan `json:"settings,omitempty"`
	Redirect        bool               `json:"redirect"`
	RedirectToWWW   bool               `json:"redirectToWww"`
	RedirectNonWWW  bool               `json:"redirectNonWww"`
	RedirectTarget  string             `json:"redirectTarget,omitempty"`
	ReviewRequired  bool               `json:"reviewRequired"`
}

type RouteSettingPlan struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type BasicAuthPlan struct {
	Enabled        bool   `json:"enabled"`
	Login          string `json:"login,omitempty"`
	SecretRedacted bool   `json:"secretRedacted"`
}

type ReviewItem struct {
	Severity string                 `json:"severity"`
	App      string                 `json:"app,omitempty"`
	Instance string                 `json:"instance,omitempty"`
	Code     string                 `json:"code,omitempty"`
	Path     string                 `json:"path,omitempty"`
	Subject  string                 `json:"subject"`
	Message  string                 `json:"message"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

func BuildPlan(export Export, opts PlanOptions) (Plan, error) {
	if err := export.Validate(); err != nil {
		return Plan{}, err
	}
	if export.Schema == ExportSchemaV2 {
		if export.Source.Kind != opts.SourceKind || export.Source.UUID != opts.SourceID {
			return Plan{}, fmt.Errorf(
				"source export identifies %s %q but plan requested %s %q",
				export.Source.Kind,
				export.Source.UUID,
				opts.SourceKind,
				opts.SourceID,
			)
		}
	}
	exportDigest, err := export.ContentDigest()
	if err != nil {
		return Plan{}, fmt.Errorf("compute source export digest: %w", err)
	}

	plan := Plan{
		Schema: "wodby1-migration-plan/v2",
		Source: PlanSource{
			Kind:           opts.SourceKind,
			ID:             opts.SourceID,
			Schema:         export.Schema,
			GeneratedAt:    export.GeneratedAt,
			ExportDigest:   exportDigest,
			ResponseDigest: export.ResponseDigest,
		},
		Target: PlanTarget{
			Org:           opts.TargetOrg,
			Project:       opts.TargetProject,
			Cluster:       opts.TargetCluster,
			AdminVerified: opts.TargetAdminVerified,
		},
		Apps:   []AppPlan{},
		Review: []ReviewItem{},
	}
	if opts.TargetScope != nil {
		capabilities := opts.TargetScope.Cluster.Capabilities
		plan.Target.OrgID = opts.TargetScope.Org.ID
		plan.Target.OrgName = opts.TargetScope.Org.Name
		plan.Target.ProjectID = opts.TargetScope.Project.ID
		plan.Target.ProjectName = opts.TargetScope.Project.Name
		plan.Target.ClusterID = opts.TargetScope.Cluster.ID
		plan.Target.ClusterName = opts.TargetScope.Cluster.Name
		plan.Target.ClusterStatus = opts.TargetScope.Cluster.Status
		plan.Target.AdminVerified = opts.TargetScope.User.IsAdmin
		plan.Target.DiscoveryVerified = true
		plan.Target.Capabilities = &capabilities
		if !opts.TargetScope.User.IsAdmin {
			plan.addReview(SeverityBlocking, "", "", "target authorization", "target discovery did not verify a Wodby 2 global administrator")
		}
		if opts.TargetScope.Org.ID <= 0 || opts.TargetScope.Project.ID <= 0 || opts.TargetScope.Cluster.ID <= 0 {
			plan.addReview(SeverityBlocking, "", "", "target scope", "target discovery returned an invalid organization, project, or cluster ID")
		}
		if opts.TargetScope.Project.OrgID != opts.TargetScope.Org.ID ||
			opts.TargetScope.Cluster.OrgID != opts.TargetScope.Org.ID {
			plan.addReview(SeverityBlocking, "", "", "target scope", "target project or cluster does not belong to the selected organization")
		}
		clusterStatus := strings.ToUpper(strings.TrimSpace(opts.TargetScope.Cluster.Status))
		if clusterStatus == "" {
			plan.addReview(SeverityBlocking, "", "", "target cluster status", "target discovery did not return cluster status")
		} else if clusterStatus != "OK" {
			plan.addReview(SeverityBlocking, "", "", "target cluster status", fmt.Sprintf("selected target cluster status %q cannot accept a migration", opts.TargetScope.Cluster.Status))
		}
	}

	for _, issue := range export.Issues {
		severity := SeverityBlocking
		if issue.Severity == SeveritySkipped {
			severity = SeveritySkipped
		}
		subject := firstNonEmpty(issue.Path, issue.Code, "source export")
		message := firstNonEmpty(issue.Message, issue.Code, "source export reported an issue")
		plan.Review = append(plan.Review, ReviewItem{
			Severity: severity,
			Code:     issue.Code,
			Path:     issue.Path,
			Subject:  subject,
			Message:  message,
			Details:  issue.Details,
		})
	}

	appExports := append([]AppExport(nil), export.AppExports()...)
	sort.SliceStable(appExports, func(i, j int) bool {
		return compareApp(appExports[i].App, appExports[j].App) < 0
	})
	for _, appExport := range appExports {
		appPlan := AppPlan{
			SourceUUID:    appExport.App.UUID,
			Name:          appExport.App.Name,
			Title:         appExport.App.Title,
			Type:          appExport.App.Type,
			SourceStatus:  appExport.App.Status,
			SourceCreated: appExport.App.Created,
			SourceUpdated: appExport.App.Updated,
		}
		if appExport.App.Repository != nil {
			repositoryURL, credentialsRedacted := sanitizeRepositoryURL(appExport.App.Repository.URL)
			appPlan.Repository = &RepositoryPlan{
				SourceUUID:          appExport.App.Repository.UUID,
				Title:               appExport.App.Repository.Title,
				URL:                 repositoryURL,
				CredentialsRedacted: credentialsRedacted,
				SourceStatus:        appExport.App.Repository.Status,
			}
			plan.addReview(SeverityBlocking, appPlan.Name, "", "repository", "source repository requires an explicit Wodby 2 repository/CI mapping")
			if credentialsRedacted {
				plan.addReview(SeverityManual, appPlan.Name, "", "repository URL", "credentials or query data were removed from the repository URL before writing the plan")
			}
		}
		if strings.TrimSpace(appPlan.Name) == "" {
			plan.addReview(SeverityBlocking, appPlan.Name, "", "app", "source app name is required")
		}
		appStatus := strings.ToLower(strings.TrimSpace(appPlan.SourceStatus))
		if export.Schema == ExportSchemaV2 && appStatus == "" {
			plan.addReview(SeverityBlocking, appPlan.Name, "", "app status", "Wodby 1 migration/v2 export is missing app status")
		} else if appStatus != "" && appStatus != "ok" {
			plan.addReview(SeverityBlocking, appPlan.Name, "", "app status", fmt.Sprintf("source app status %q is not stable for migration", appPlan.SourceStatus))
		}

		instances := append([]Instance(nil), appExport.Instances...)
		sort.SliceStable(instances, func(i, j int) bool {
			return compareInstance(instances[i], instances[j]) < 0
		})
		for _, instance := range instances {
			instancePlan := buildInstancePlan(&plan, appExport.App, instance, opts, export.Schema == ExportSchemaV2)
			appPlan.Instances = append(appPlan.Instances, instancePlan)
		}
		plan.Apps = append(plan.Apps, appPlan)
	}

	sortReview(plan.Review)
	plan.computeSummary()
	plan.PlanHash, err = plan.contentDigest()
	if err != nil {
		return Plan{}, fmt.Errorf("compute migration plan digest: %w", err)
	}
	return plan, nil
}

func (p Plan) contentDigest() (string, error) {
	p.PlanHash = ""
	p.Source.GeneratedAt = 0
	p.Source.ResponseDigest = ""
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func buildInstancePlan(plan *Plan, app App, instance Instance, opts PlanOptions, requireStatus bool) InstancePlan {
	targetEnv, targetEnvType, ok := resolveTargetEnv(instance.Type, opts.TargetEnvMap)
	if !ok {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "env", fmt.Sprintf("source instance type %q has no default Wodby 2 env mapping; pass --target-env-map %s=TARGET_ENV", instance.Type, instance.Type))
	}

	instancePlan := InstancePlan{
		SourceUUID:    instance.UUID,
		Name:          instance.Name,
		Title:         instance.Title,
		SourceType:    instance.Type,
		SourceStatus:  instance.Status,
		SourceUpdated: instance.Updated,
		TargetEnv:     targetEnv,
		TargetEnvType: targetEnvType,
		Stack: StackPlan{
			UUID:         instance.Stack.UUID,
			Name:         instance.Stack.Name,
			Version:      instance.Stack.Version,
			Custom:       instance.Stack.Custom,
			AncestorUUID: instance.Stack.AncestorUUID,
			AncestorName: instance.Stack.AncestorName,
		},
	}
	if ok && opts.TargetScope != nil {
		resolved, found := opts.TargetEnvs[targetEnv]
		if !found {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "target env", fmt.Sprintf("target environment selector %q was not included in verified target discovery", targetEnv))
		} else {
			instancePlan.TargetEnv = resolved.Name
			instancePlan.TargetEnvID = resolved.ID
			instancePlan.TargetEnvType = strings.ToUpper(resolved.Type)
			if resolved.ID <= 0 {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "target env", fmt.Sprintf("target environment %q has an invalid ID", resolved.Name))
			}
			if resolved.OrgID != opts.TargetScope.Org.ID {
				plan.addReview(
					SeverityBlocking,
					app.Name,
					instance.Name,
					"target env",
					fmt.Sprintf(
						"target environment %q belongs to organization ID %d, expected organization ID %d",
						resolved.Name,
						resolved.OrgID,
						opts.TargetScope.Org.ID,
					),
				)
			}
			if targetEnvType != "" && !strings.EqualFold(resolved.Type, targetEnvType) {
				plan.addReview(
					SeverityBlocking,
					app.Name,
					instance.Name,
					"target env type",
					fmt.Sprintf(
						"target environment %q has type %q, expected %q for source instance type %q",
						resolved.Name,
						resolved.Type,
						targetEnvType,
						instance.Type,
					),
				)
			}
		}
	}

	if instance.Stack.Custom {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "custom stack", "custom stack requires an explicit target stack mapping")
	}

	status := strings.ToLower(strings.TrimSpace(instance.Status))
	if requireStatus && status == "" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance status", "Wodby 1 migration/v2 export is missing instance status")
	} else if status != "" && status != "ok" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance status", fmt.Sprintf("source instance status %q is not stable for migration", instance.Status))
	}

	if instance.BasicAuth != nil && instance.BasicAuth.Enabled {
		instancePlan.BasicAuth.Enabled = true
		instancePlan.BasicAuth.Login = instance.BasicAuth.Login
		if strings.TrimSpace(instance.BasicAuth.Login) == "" {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "basic auth", "basic-auth login is empty")
		}
		if instance.BasicAuth.IsPasswordRedacted() {
			instancePlan.BasicAuth.SecretRedacted = true
			severity := SeverityBlocking
			if opts.AllowMissingSecrets {
				severity = SeverityManual
			}
			plan.addReview(severity, app.Name, instance.Name, "basic auth", "basic-auth password is redacted; rerun with --include-secrets or accept a manual protection gap")
		} else if instance.BasicAuth.Password == "" {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "basic auth", "basic-auth password is empty or unavailable")
		} else {
			plan.addReview(SeverityManual, app.Name, instance.Name, "basic auth", "basic-auth credentials are intentionally omitted from the plan and require a secure apply-time transfer")
		}
	}

	services := append([]Service(nil), instance.Services...)
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
	for _, service := range services {
		servicePlan := buildServicePlan(plan, app, instance, service, opts)
		instancePlan.Services = append(instancePlan.Services, servicePlan)
		instancePlan.EnvVars += servicePlan.EnvVars
		instancePlan.CronJobs += servicePlan.CronJobs
	}

	domains := append([]Domain(nil), instance.Domains...)
	sort.SliceStable(domains, func(i, j int) bool {
		if domains[i].UUID != domains[j].UUID {
			return domains[i].UUID < domains[j].UUID
		}
		return domains[i].Name < domains[j].Name
	})
	for _, domain := range domains {
		routePlan := buildRoutePlan(plan, app, instance, domain, instancePlan.BasicAuth.Enabled, opts, requireStatus)
		instancePlan.Routes = append(instancePlan.Routes, routePlan)
	}

	for _, backup := range instance.Backups {
		instancePlan.Imports++
		validateBackupPlan(plan, app, instance, backup)
	}
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
		return servicePlan
	}

	reportedRedacted := map[string]bool{}
	for _, envVar := range service.EnvVars {
		if !envVar.Enabled || envVar.Origin == "default" || envVar.Origin == "computed" {
			continue
		}
		servicePlan.EnvVars++
		if envVar.IsRedacted() {
			reportedRedacted[envVar.Name] = true
			severity := SeverityBlocking
			if opts.AllowMissingSecrets {
				severity = SeverityManual
			}
			plan.addReview(severity, app.Name, instance.Name, "env var "+envVar.Name, "secret or protected env var value is redacted")
		}
	}

	secretsRedacted := append([]string(nil), service.SecretsRedacted...)
	sort.Strings(secretsRedacted)
	for _, redacted := range secretsRedacted {
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
		if cron.Classification == "source_only_infrastructure" {
			continue
		}
		if cron.Enabled {
			servicePlan.CronJobs++
			if strings.TrimSpace(cron.Crontab) == "" || strings.TrimSpace(cron.Command) == "" {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "cron "+firstNonEmpty(cron.Title, cron.Crontab), "enabled source cron requires both a schedule and command")
			}
		}
	}
	if len(service.Configuration) != 0 {
		plan.addReview(SeverityManual, app.Name, instance.Name, "service "+service.Name, "source service configuration requires explicit reconciliation against the target stack")
	}
	if servicePlan.EnvVars > 0 {
		plan.addReview(SeverityManual, app.Name, instance.Name, "service "+service.Name, fmt.Sprintf("%d custom environment variable(s) require target service reconciliation", servicePlan.EnvVars))
	}
	if servicePlan.CronJobs > 0 {
		plan.addReview(SeverityManual, app.Name, instance.Name, "service "+service.Name, fmt.Sprintf("%d application cron job(s) require target service reconciliation", servicePlan.CronJobs))
	}
	return servicePlan
}

func buildRoutePlan(plan *Plan, app App, instance Instance, domain Domain, basicAuth bool, opts PlanOptions, requireStatus bool) RoutePlan {
	enabled := domain.Enabled == nil || *domain.Enabled
	routePlan := RoutePlan{
		SourceUUID:      domain.UUID,
		Host:            domain.Name,
		Type:            domain.Type,
		Status:          domain.Status,
		Enabled:         enabled,
		Action:          "unvalidated",
		Primary:         domain.Primary,
		Indexed:         domain.Indexed,
		SSL:             domain.SSL,
		SSLRequired:     domain.SSLRequired,
		SSLCustom:       domain.SSLCustom,
		HSTS:            domain.HSTS,
		HSTSSubdomains:  domain.HSTSSubdomains,
		Protected:       domain.Protected,
		Service:         domain.Service,
		ServiceProtocol: domain.ServiceProtocol,
		PortNumber:      domain.PortNumber,
		NeedsPortID:     domain.Service != "" && domain.PortNumber != nil,
		BasicAuth:       basicAuth && domain.Protected,
		Redirect:        domain.RedirectToWWW || domain.RedirectNonWWW || domain.RedirectTarget != "",
		RedirectToWWW:   domain.RedirectToWWW,
		RedirectNonWWW:  domain.RedirectNonWWW,
		RedirectTarget:  domain.RedirectTarget,
	}

	if !enabled {
		routePlan.Action = "skip_disabled"
		routePlan.NeedsPortID = false
		plan.addReview(SeveritySkipped, app.Name, instance.Name, "route "+domain.Name, "disabled source route is intentionally excluded from the migration plan")
		return routePlan
	}
	if strings.EqualFold(strings.TrimSpace(domain.Type), "technical") {
		routePlan.Action = "skip_technical"
		routePlan.NeedsPortID = false
		plan.addReview(SeveritySkipped, app.Name, instance.Name, "route "+domain.Name, "technical source route is intentionally excluded from the migration plan")
		return routePlan
	}
	if strings.TrimSpace(domain.Name) == "" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route", "source route host is empty")
	}
	status := strings.ToLower(strings.TrimSpace(domain.Status))
	if requireStatus && status == "" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "Wodby 1 migration/v2 export is missing route status")
	} else if status != "" && status != "ok" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, fmt.Sprintf("source route status %q is not stable for migration", domain.Status))
	}
	if domain.PortNumber != nil && *domain.PortNumber <= 0 {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "source route port number must be positive")
	}

	if domain.SSLCustom {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "custom TLS material requires an explicit secure migration path")
	}
	if domain.HSTS || domain.HSTSSubdomains {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityManual, app.Name, instance.Name, "route "+domain.Name, "HSTS settings require explicit target route-setting support")
	}
	if protocol := strings.ToLower(strings.TrimSpace(domain.ServiceProtocol)); protocol != "" && protocol != "http" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, fmt.Sprintf("source route protocol %q is not supported by Wodby 2 app routes", domain.ServiceProtocol))
	}

	if domain.Service == "" || domain.PortNumber == nil {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityConfirmation, app.Name, instance.Name, "route "+domain.Name, "route target service or port number is missing; a target app-port ID will need to be resolved")
	}

	if domain.SSLRequired != nil {
		value := strconv.FormatBool(*domain.SSLRequired)
		routePlan.Settings = append(routePlan.Settings, RouteSettingPlan{Name: "HTTPS_REDIRECT", Value: value})
	}
	if domain.Indexed != nil {
		routePlan.Settings = append(
			routePlan.Settings,
			RouteSettingPlan{Name: "NO_INDEX", Value: strconv.FormatBool(!*domain.Indexed)},
		)
	}

	if routePlan.Redirect {
		switch {
		case opts.TargetScope == nil:
			routePlan.ReviewRequired = true
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "redirect route capability must be verified through target discovery")
		case !opts.TargetScope.Cluster.Capabilities.RedirectRoutes:
			routePlan.ReviewRequired = true
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "selected target cluster does not support redirect routes")
		case !routePlan.ReviewRequired && domain.Service != "" && domain.PortNumber != nil:
			routePlan.Action = "create_redirect"
		}
	} else if !routePlan.ReviewRequired && domain.Service != "" && domain.PortNumber != nil {
		routePlan.Action = "create_backend"
	}

	sort.SliceStable(routePlan.Settings, func(i, j int) bool {
		if routePlan.Settings[i].Name != routePlan.Settings[j].Name {
			return routePlan.Settings[i].Name < routePlan.Settings[j].Name
		}
		return routePlan.Settings[i].Value < routePlan.Settings[j].Value
	})
	return routePlan
}

func resolveTargetEnv(sourceType string, explicit map[string]string) (string, string, bool) {
	sourceType = strings.TrimSpace(sourceType)
	target, explicitMapping := explicit[sourceType]
	if !explicitMapping {
		target, explicitMapping = explicit[strings.ToLower(sourceType)]
	}
	if explicitMapping && strings.TrimSpace(target) != "" {
		target = strings.TrimSpace(target)
		envType := defaultEnvType(sourceType)
		if envType == "" {
			envType = defaultEnvType(target)
		}
		return target, envType, true
	}
	envType := defaultEnvType(sourceType)
	if envType == "" {
		return "", "", false
	}
	return strings.ToLower(envType), envType, true
}

// TargetEnvironmentSelectors returns the exact Wodby 2 environment selectors
// required by the source export. Source types without a safe default remain
// blocking plan items and are intentionally omitted from discovery.
func TargetEnvironmentSelectors(export Export, explicit map[string]string) ([]string, error) {
	if err := export.Validate(); err != nil {
		return nil, err
	}
	selectors := map[string]bool{}
	for _, app := range export.AppExports() {
		for _, instance := range app.Instances {
			selector, _, ok := resolveTargetEnv(instance.Type, explicit)
			if ok {
				selectors[selector] = true
			}
		}
	}
	result := make([]string, 0, len(selectors))
	for selector := range selectors {
		result = append(result, selector)
	}
	sort.Strings(result)
	return result, nil
}

func defaultEnvType(sourceType string) string {
	switch strings.ToLower(sourceType) {
	case "prod", "production":
		return "PROD"
	case "stage", "staging":
		return "STAGING"
	case "dev", "development":
		return "DEV"
	case "test":
		return "TEST"
	case "feature":
		return "FEATURE"
	default:
		return ""
	}
}

func compareApp(a App, b App) int {
	return strings.Compare(
		strings.Join([]string{a.UUID, a.Name, a.Type, a.Title}, "\x00"),
		strings.Join([]string{b.UUID, b.Name, b.Type, b.Title}, "\x00"),
	)
}

func compareInstance(a Instance, b Instance) int {
	return strings.Compare(
		strings.Join([]string{a.UUID, a.Name, a.Type, a.Title}, "\x00"),
		strings.Join([]string{b.UUID, b.Name, b.Type, b.Title}, "\x00"),
	)
}

func sortReview(review []ReviewItem) {
	sort.SliceStable(review, func(i, j int) bool {
		a := review[i]
		b := review[j]
		aKey := strings.Join([]string{a.App, a.Instance, severitySortKey(a.Severity), a.Code, a.Path, a.Subject, a.Message, canonicalJSON(a.Details)}, "\x00")
		bKey := strings.Join([]string{b.App, b.Instance, severitySortKey(b.Severity), b.Code, b.Path, b.Subject, b.Message, canonicalJSON(b.Details)}, "\x00")
		return aKey < bKey
	})
}

func sanitizeRepositoryURL(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}

	if !strings.Contains(value, "://") {
		sanitized := value
		redacted := false
		if index := strings.IndexAny(sanitized, "?#"); index >= 0 {
			sanitized = sanitized[:index]
			redacted = true
		}
		if at := strings.LastIndex(sanitized, "@"); at > 0 && strings.Contains(sanitized[:at], ":") {
			sanitized = "[credentials-redacted]@" + sanitized[at+1:]
			redacted = true
		}
		if strings.HasPrefix(sanitized, "[credentials-redacted]@") {
			redacted = true
		}
		if !isSCPRepositoryURL(sanitized) {
			return "", true
		}
		return sanitized, redacted
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", true
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh", "git", "git+ssh":
	default:
		return "", true
	}
	if parsed.Hostname() == "" {
		return "", true
	}
	redacted := false
	if parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		preserveSSHUsername := (strings.EqualFold(parsed.Scheme, "ssh") ||
			strings.EqualFold(parsed.Scheme, "git+ssh")) && !hasPassword
		if !preserveSSHUsername {
			parsed.User = nil
			redacted = true
		}
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		redacted = true
	}
	if parsed.Fragment != "" {
		parsed.Fragment = ""
		redacted = true
	}
	return parsed.String(), redacted
}

func isSCPRepositoryURL(value string) bool {
	at := strings.Index(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	user := value[:at]
	remainder := value[at+1:]
	colon := strings.Index(remainder, ":")
	if colon <= 0 || colon == len(remainder)-1 {
		return false
	}
	host := remainder[:colon]
	path := remainder[colon+1:]
	return !strings.ContainsAny(user, "/@: \t\r\n") &&
		!strings.ContainsAny(host, "/@: \t\r\n") &&
		strings.TrimSpace(path) != ""
}

func validateBackupPlan(plan *Plan, app App, instance Instance, backup Backup) {
	subject := "backup " + firstNonEmpty(backup.Component, backup.UUID)
	if strings.TrimSpace(backup.UUID) == "" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup UUID is required")
	}
	if strings.TrimSpace(backup.Component) == "" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup component is required")
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(backup.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup URL must be an HTTPS URL without embedded credentials")
	}
	if status := strings.ToLower(strings.TrimSpace(backup.Status)); status != "" && status != "ok" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, fmt.Sprintf("source backup status %q is not usable for migration", backup.Status))
	}
	if backup.Size < 0 {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup size cannot be negative")
	}
	plan.addReview(SeverityManual, app.Name, instance.Name, subject, "backup import requires explicit target component mapping and task verification")
}

func canonicalJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}

func severitySortKey(severity string) string {
	switch severity {
	case SeverityBlocking:
		return "0"
	case SeverityConfirmation:
		return "1"
	case SeverityManual:
		return "2"
	case SeveritySkipped:
		return "3"
	default:
		return "4:" + severity
	}
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
	case p.Target.DiscoveryVerified:
		p.Status = "target_scope_validated"
	default:
		p.Status = "source_inventory_unvalidated"
	}
}
