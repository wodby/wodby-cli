package wodby1

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MigrationPlanSchema = "wodby1-migration-plan/v14"

	SeverityBlocking       = "blocking"
	SeverityMigration      = "migration"
	SeverityConfirmation   = "requires_confirmation"
	SeverityServiceWarning = "service_warning"
	SeverityManual         = "manual_follow_up"
	SeveritySkipped        = "intentionally_skipped"
)

type PlanOptions struct {
	SourceKind                    string
	SourceID                      string
	TargetOrg                     string
	TargetProject                 string
	TargetCluster                 string
	TargetEnvMap                  map[string]string
	TargetOrgOwnerOrAdminVerified bool
	TargetScope                   *TargetScopeDiscovery
	TargetEnvs                    map[string]TargetEnv
	TargetStackID                 int
	TargetStackMap                map[string]string
	TargetServiceMap              map[string]string
	TargetVersionMap              map[string]string
	TargetImportMap               map[string]string
	TargetCIIntegrationID         int
	Repository                    RepositoryTargetPlan
	RepositoryByApp               map[string]RepositoryTargetPlan
	SkipCode                      bool
	SkipData                      bool
	RequireData                   bool
	AllowUnsupportedDrupal        bool
	Selection                     *SourceSelection
}

type RepositoryTargetPlan struct {
	GitIntegrationID int    `json:"gitIntegrationId,omitempty"`
	RepositoryName   string `json:"repositoryName,omitempty"`
	Service          string `json:"service,omitempty"`
}

type Plan struct {
	Schema    string          `json:"schema"`
	PlanHash  string          `json:"planHash"`
	Source    PlanSource      `json:"source"`
	Selection SourceSelection `json:"selection"`
	Target    PlanTarget      `json:"target"`
	Summary   PlanSummary     `json:"summary"`
	Apps      []AppPlan       `json:"apps"`
	Review    []ReviewItem    `json:"review"`
	Status    string          `json:"status"`
}

type PlanSource struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	Schema         string `json:"schema"`
	GeneratedAt    int64  `json:"generatedAt,omitempty"`
	ExportDigest   string `json:"exportDigest"`
	ConfigDigest   string `json:"configDigest"`
	BackupDigest   string `json:"backupDigest"`
	ResponseDigest string `json:"responseDigest,omitempty"`
}

type PlanTarget struct {
	Org                     string                     `json:"org,omitempty"`
	OrgID                   int                        `json:"orgId,omitempty"`
	OrgName                 string                     `json:"orgName,omitempty"`
	OrgRole                 string                     `json:"orgRole,omitempty"`
	OrgDefaultTimeZone      string                     `json:"orgDefaultTimeZone,omitempty"`
	Project                 string                     `json:"project,omitempty"`
	ProjectID               int                        `json:"projectId,omitempty"`
	ProjectName             string                     `json:"projectName,omitempty"`
	Cluster                 string                     `json:"cluster,omitempty"`
	ClusterID               int                        `json:"clusterId,omitempty"`
	ClusterName             string                     `json:"clusterName,omitempty"`
	ClusterStatus           string                     `json:"clusterStatus,omitempty"`
	CIIntegrationID         int                        `json:"ciIntegrationId"`
	OrgOwnerOrAdminVerified bool                       `json:"orgOwnerOrAdminVerified"`
	DiscoveryVerified       bool                       `json:"discoveryVerified"`
	Capabilities            *TargetClusterCapabilities `json:"capabilities,omitempty"`
	OrgCapabilities         *TargetOrgCapabilities     `json:"orgCapabilities,omitempty"`
	Subscription            *TargetOrgSubscription     `json:"subscription,omitempty"`
}

type PlanSummary struct {
	Apps            int `json:"apps"`
	Instances       int `json:"instances"`
	Services        int `json:"services"`
	Routes          int `json:"routes"`
	EnvVars         int `json:"envVars"`
	CronJobs        int `json:"cronJobs"`
	Imports         int `json:"imports"`
	Migrations      int `json:"migrations"`
	ServiceWarnings int `json:"serviceWarnings"`
	Blocking        int `json:"blocking"`
	Confirmation    int `json:"requiresConfirmation"`
	Manual          int `json:"manualFollowUp"`
	Intentionally   int `json:"intentionallySkipped"`
}

type AppPlan struct {
	SourceUUID    string            `json:"sourceUuid"`
	Name          string            `json:"name"`
	Title         string            `json:"title"`
	Type          string            `json:"type"`
	SourceStatus  string            `json:"sourceStatus,omitempty"`
	SourceCreated int64             `json:"sourceCreated,omitempty"`
	SourceUpdated int64             `json:"sourceUpdated,omitempty"`
	Repository    *RepositoryPlan   `json:"repository,omitempty"`
	Integrations  []IntegrationPlan `json:"integrations,omitempty"`
	Instances     []InstancePlan    `json:"instances"`
}

type IntegrationPlan struct {
	Key           string   `json:"key"`
	ProviderName  string   `json:"providerName"`
	ProviderID    int      `json:"providerId"`
	ProviderRevID int      `json:"providerRevId"`
	Kind          string   `json:"kind"`
	Service       string   `json:"service,omitempty"`
	Action        string   `json:"action"`
	Variables     []string `json:"variables,omitempty"`
}

type RepositoryPlan struct {
	SourceUUID          string `json:"sourceUuid"`
	Title               string `json:"title"`
	URL                 string `json:"url,omitempty"`
	CredentialsRedacted bool   `json:"credentialsRedacted,omitempty"`
	SourceStatus        string `json:"sourceStatus,omitempty"`
	Action              string `json:"action"`
	TargetService       string `json:"targetService,omitempty"`
	GitIntegrationID    int    `json:"gitIntegrationId,omitempty"`
	RepositoryName      string `json:"repositoryName,omitempty"`
	RemoteGitRepoID     string `json:"remoteGitRepoId,omitempty"`
}

type InstancePlan struct {
	SourceUUID        string        `json:"sourceUuid"`
	Name              string        `json:"name"`
	Title             string        `json:"title"`
	SourceType        string        `json:"sourceType"`
	SourceStatus      string        `json:"sourceStatus,omitempty"`
	SourceUpdated     int64         `json:"sourceUpdated,omitempty"`
	TargetEnv         string        `json:"targetEnv,omitempty"`
	TargetEnvID       int           `json:"targetEnvId,omitempty"`
	TargetEnvType     string        `json:"targetEnvType,omitempty"`
	BuildServiceID    int           `json:"buildServiceId,omitempty"`
	BuildServiceRevID int           `json:"buildServiceRevId,omitempty"`
	Stack             StackPlan     `json:"stack"`
	Services          []ServicePlan `json:"services"`
	Routes            []RoutePlan   `json:"routes"`
	BasicAuth         BasicAuthPlan `json:"basicAuth"`
	CronJobs          int           `json:"cronJobs"`
	EnvVars           int           `json:"envVars"`
	Imports           []ImportPlan  `json:"imports"`
}

type StackPlan struct {
	UUID            string `json:"uuid,omitempty"`
	Name            string `json:"name"`
	Type            string `json:"type,omitempty"`
	Version         string `json:"version,omitempty"`
	Custom          bool   `json:"custom"`
	AncestorUUID    string `json:"ancestorUuid,omitempty"`
	AncestorName    string `json:"ancestorName,omitempty"`
	Target          string `json:"target"`
	CreateTarget    bool   `json:"createTarget,omitempty"`
	CatalogName     string `json:"catalogName,omitempty"`
	ExplicitMapping bool   `json:"explicitMapping,omitempty"`
	TargetID        int    `json:"targetId,omitempty"`
	TargetRevID     int    `json:"targetRevId,omitempty"`
	TargetVersion   string `json:"targetVersion,omitempty"`
}

type ImportPlan struct {
	SourceUUID         string `json:"sourceUuid"`
	Component          string `json:"component"`
	BackupUUID         string `json:"backupUuid,omitempty"`
	BackupCreated      int64  `json:"backupCreated,omitempty"`
	Size               int64  `json:"size,omitempty"`
	Action             string `json:"action"`
	TargetService      string `json:"targetService,omitempty"`
	TargetImport       string `json:"targetImport,omitempty"`
	TargetServiceID    int    `json:"targetServiceId,omitempty"`
	TargetServiceRevID int    `json:"targetServiceRevId,omitempty"`
}

type ServicePlan struct {
	SourceName          string             `json:"sourceName"`
	SourceVersion       string             `json:"sourceVersion,omitempty"`
	TargetName          string             `json:"targetName,omitempty"`
	TargetVersion       string             `json:"targetVersion,omitempty"`
	VersionAction       string             `json:"versionAction,omitempty"`
	VersionExplicit     bool               `json:"versionExplicit,omitempty"`
	TargetID            int                `json:"targetId,omitempty"`
	TargetServiceRevID  int                `json:"targetServiceRevId,omitempty"`
	AddToStack          bool               `json:"addToStack,omitempty"`
	CatalogServiceID    int                `json:"catalogServiceId,omitempty"`
	CatalogServiceRevID int                `json:"catalogServiceRevId,omitempty"`
	Enabled             bool               `json:"enabled"`
	Action              string             `json:"action"`
	EnvVars             int                `json:"envVars"`
	CronJobs            int                `json:"cronJobs"`
	Settings            int                `json:"settings"`
	CronSchedules       []CronSchedulePlan `json:"cronSchedules,omitempty"`
}

type CronSchedulePlan struct {
	Title       string `json:"title"`
	Schedule    string `json:"schedule"`
	Command     string `json:"command"`
	TargetState string `json:"targetState"`
}

type RoutePlan struct {
	SourceUUID         string             `json:"sourceUuid,omitempty"`
	Host               string             `json:"host"`
	Type               string             `json:"type,omitempty"`
	Status             string             `json:"status,omitempty"`
	Enabled            bool               `json:"enabled"`
	Action             string             `json:"action"`
	Primary            bool               `json:"primary"`
	Indexed            *bool              `json:"indexed,omitempty"`
	SSL                bool               `json:"ssl"`
	SSLRequired        *bool              `json:"sslRequired,omitempty"`
	SSLCustom          bool               `json:"sslCustom"`
	TargetCertID       int                `json:"targetCertId,omitempty"`
	TargetCertDNSNames []string           `json:"targetCertDnsNames,omitempty"`
	HSTS               bool               `json:"hsts"`
	HSTSSubdomains     bool               `json:"hstsSubdomains"`
	Protected          bool               `json:"protected"`
	Service            string             `json:"service,omitempty"`
	ServiceProtocol    string             `json:"serviceProtocol,omitempty"`
	PortNumber         *int               `json:"portNumber,omitempty"`
	NeedsPortID        bool               `json:"needsPortId"`
	BasicAuth          bool               `json:"basicAuth"`
	Settings           []RouteSettingPlan `json:"settings,omitempty"`
	Redirect           bool               `json:"redirect"`
	RedirectToWWW      bool               `json:"redirectToWww"`
	RedirectNonWWW     bool               `json:"redirectNonWww"`
	RedirectTarget     string             `json:"redirectTarget,omitempty"`
	ReviewRequired     bool               `json:"reviewRequired"`
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
	if opts.TargetCIIntegrationID < 0 {
		return Plan{}, fmt.Errorf("target CI integration ID must not be negative")
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
	configDigest, err := export.MigrationConfigDigest()
	if err != nil {
		return Plan{}, fmt.Errorf("compute source configuration digest: %w", err)
	}
	backupDigest, err := export.BackupDigest()
	if err != nil {
		return Plan{}, fmt.Errorf("compute source backup digest: %w", err)
	}
	if opts.SkipData {
		backupDigest = ""
	}

	plan := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{
			Kind:         opts.SourceKind,
			ID:           opts.SourceID,
			Schema:       export.Schema,
			GeneratedAt:  export.GeneratedAt,
			ExportDigest: exportDigest,
			ConfigDigest: configDigest,
			BackupDigest: backupDigest,
		},
		Target: PlanTarget{
			Org:                     opts.TargetOrg,
			Project:                 opts.TargetProject,
			Cluster:                 opts.TargetCluster,
			CIIntegrationID:         opts.TargetCIIntegrationID,
			OrgOwnerOrAdminVerified: opts.TargetOrgOwnerOrAdminVerified,
		},
		Selection: sourceSelectionForPlan(export, opts.Selection),
		Apps:      []AppPlan{},
		Review:    []ReviewItem{},
	}
	for _, app := range plan.Selection.ExcludedApps {
		plan.addReview(SeveritySkipped, app.Name, "", "source selection", fmt.Sprintf("app %q (%s) is excluded from this migration", app.Name, app.UUID))
	}
	for _, instance := range plan.Selection.ExcludedInstances {
		plan.addReview(SeveritySkipped, instance.AppName, instance.Name, "source selection", fmt.Sprintf("instance %q (%s) is excluded from this migration", instance.Name, instance.UUID))
	}
	if opts.TargetScope != nil {
		capabilities := opts.TargetScope.Cluster.Capabilities
		plan.Target.OrgID = opts.TargetScope.Org.ID
		plan.Target.OrgName = opts.TargetScope.Org.Name
		if opts.TargetScope.Project.ID > 0 {
			plan.Target.ProjectID = opts.TargetScope.Project.ID
			plan.Target.ProjectName = opts.TargetScope.Project.Name
		}
		plan.Target.ClusterID = opts.TargetScope.Cluster.ID
		plan.Target.ClusterName = opts.TargetScope.Cluster.Name
		plan.Target.ClusterStatus = opts.TargetScope.Cluster.Status
		plan.Target.OrgRole = strings.ToLower(strings.TrimSpace(opts.TargetScope.Membership.Role))
		plan.Target.OrgOwnerOrAdminVerified = opts.TargetScope.Membership.Status == "ok" &&
			(plan.Target.OrgRole == "owner" || plan.Target.OrgRole == "admin")
		plan.Target.DiscoveryVerified = true
		plan.Target.Capabilities = &capabilities
		plan.Target.OrgCapabilities = opts.TargetScope.Org.Capabilities
		plan.Target.Subscription = opts.TargetScope.Org.Subscription
		plan.Target.OrgDefaultTimeZone = opts.TargetScope.Org.DefaultTimeZone
		if !plan.Target.OrgOwnerOrAdminVerified {
			plan.addReview(SeverityBlocking, "", "", "target authorization", "target discovery did not verify an active Wodby 2 organization owner or administrator")
		}
		if opts.TargetScope.Org.ID <= 0 || opts.TargetScope.Project.ID < 0 || opts.TargetScope.Cluster.ID <= 0 {
			plan.addReview(SeverityBlocking, "", "", "target scope", "target discovery returned an invalid organization, project, or cluster ID")
		}
		if strings.TrimSpace(opts.TargetProject) != "" && opts.TargetScope.Project.ID <= 0 {
			plan.addReview(SeverityBlocking, "", "", "target scope", "target project selector was not resolved")
		}
		if (opts.TargetScope.Project.ID > 0 && opts.TargetScope.Project.OrgID != opts.TargetScope.Org.ID) ||
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
		if handledLegacyCapacityIssue(issue) {
			continue
		}
		severity := issue.Severity
		switch severity {
		case SeverityBlocking, SeverityMigration, SeverityConfirmation, SeverityServiceWarning, SeverityManual, SeveritySkipped:
		default:
			// Source exporters must fail closed when they introduce a severity
			// this CLI version does not understand.
			severity = SeverityBlocking
		}
		subject := firstNonEmpty(issue.Path, issue.Code, "source export")
		message := firstNonEmpty(issue.Message, issue.Code, "source export reported an issue")
		plan.Review = append(plan.Review, ReviewItem{
			Severity: severity,
			Code:     issue.Code,
			Path:     issue.Path,
			Subject:  subject,
			Message:  message,
		})
	}

	appExports := append([]AppExport(nil), export.AppExports()...)
	sort.SliceStable(appExports, func(i, j int) bool {
		return compareApp(appExports[i].App, appExports[j].App) < 0
	})
	if opts.SourceKind == "server" && len(appExports) == 0 {
		plan.addReview(SeverityBlocking, "", "", "source server", "source server does not contain any applications")
	}
	if err := validateRepositoryTargets(appExports, opts.RepositoryByApp); err != nil {
		return Plan{}, err
	}
	if opts.SourceKind == "server" {
		appNames := map[string]int{}
		for _, appExport := range appExports {
			appNames[strings.ToLower(strings.TrimSpace(appExport.App.Name))]++
		}
		for name, count := range appNames {
			if name != "" && count > 1 {
				plan.addReview(
					SeverityBlocking,
					"",
					"",
					"duplicate source app name",
					fmt.Sprintf("%d source apps are named %q and cannot be created in one target organization", count, name),
				)
			}
		}
	}
	for _, appExport := range appExports {
		repositoryTarget := repositoryTargetForApp(appExport.App, opts)
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
			repositoryName := strings.TrimSpace(repositoryTarget.RepositoryName)
			if repositoryName == "" {
				repositoryName = repositoryNameFromURL(repositoryURL)
			}
			appPlan.Repository = &RepositoryPlan{
				SourceUUID:          appExport.App.Repository.UUID,
				Title:               appExport.App.Repository.Title,
				URL:                 repositoryURL,
				CredentialsRedacted: credentialsRedacted,
				SourceStatus:        appExport.App.Repository.Status,
				Action:              "connect",
				TargetService:       strings.TrimSpace(repositoryTarget.Service),
				GitIntegrationID:    repositoryTarget.GitIntegrationID,
				RepositoryName:      repositoryName,
			}
			switch {
			case opts.SkipCode:
				appPlan.Repository.Action = "skip"
				plan.addReview(SeveritySkipped, appPlan.Name, "", "repository", "source repository code is intentionally excluded from this migration")
			case repositoryTarget.GitIntegrationID <= 0:
				appPlan.Repository.Action = "unlinked"
				plan.addReview(SeverityMigration, appPlan.Name, "", "repository", "no target Git integration was selected; the repository will remain unlinked and application code will use Custom CI")
			case repositoryName == "":
				appPlan.Repository.Action = "unlinked"
				plan.addReview(SeverityServiceWarning, appPlan.Name, "", "repository", "source repository name could not be derived; the repository will remain unlinked and application code will use Custom CI")
			}
			if credentialsRedacted {
				plan.addReview(SeverityMigration, appPlan.Name, "", "repository URL", "credentials or query data were removed from the repository URL before writing the plan")
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
		validateAppStackStrategy(&plan, &appPlan)
		plan.Apps = append(plan.Apps, appPlan)
	}
	validateTargetOrgFeatures(&plan, opts.TargetScope)

	sortReview(plan.Review)
	plan.computeSummary()
	plan.PlanHash, err = plan.contentDigest()
	if err != nil {
		return Plan{}, fmt.Errorf("compute migration plan digest: %w", err)
	}
	return plan, nil
}

func validateAppStackStrategy(plan *Plan, app *AppPlan) {
	if plan == nil || app == nil || len(app.Instances) < 2 {
		return
	}
	strategy := stackStrategyKey(app.Instances[0].Stack)
	for _, instance := range app.Instances[1:] {
		if stackStrategyKey(instance.Stack) == strategy {
			continue
		}
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"target stack strategy",
			"app instances resolve to different target stacks; one stack must be reused by every instance in an app, so provide one compatible --target-stack-id or consistent scoped mappings",
		)
	}
}

func stackStrategyKey(stack StackPlan) string {
	if stack.CreateTarget {
		return "catalog:" + stack.CatalogName
	}
	return fmt.Sprintf("existing:%d", stack.TargetID)
}

func (p Plan) contentDigest() (string, error) {
	p.PlanHash = ""
	p.Source.GeneratedAt = 0
	p.Source.ResponseDigest = ""
	// Owner and admin are the same authorization class for migration. Keep the
	// observed role in the reviewed artifact, but hash both authorized roles as
	// one class so an authorized role transition cannot strand a resume.
	role := strings.ToLower(strings.TrimSpace(p.Target.OrgRole))
	if p.Target.OrgOwnerOrAdminVerified && (role == "owner" || role == "admin") {
		p.Target.OrgRole = "owner_or_admin"
	} else {
		p.Target.OrgRole = role
	}
	// Transport response metadata is deliberately excluded, while backup UUIDs,
	// timestamps and component mappings remain binding. An applied plan always
	// resumes with the exact snapshots the customer reviewed.
	p.Source.ExportDigest = ""
	if p.Target.Subscription != nil && p.Target.Subscription.Plan != nil {
		subscription := *p.Target.Subscription
		plan := *p.Target.Subscription.Plan
		plan.Usage = 0
		subscription.Plan = &plan
		p.Target.Subscription = &subscription
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	var canonical Plan
	if err := json.Unmarshal(data, &canonical); err != nil {
		return "", err
	}
	for appIndex := range canonical.Apps {
		canonical.Apps[appIndex].SourceUpdated = 0
		for instanceIndex := range canonical.Apps[appIndex].Instances {
			canonical.Apps[appIndex].Instances[instanceIndex].SourceUpdated = 0
			for importIndex := range canonical.Apps[appIndex].Instances[instanceIndex].Imports {
				item := &canonical.Apps[appIndex].Instances[instanceIndex].Imports[importIndex]
				if item.Action == "skip" {
					item.SourceUUID = ""
					item.BackupUUID = ""
					item.BackupCreated = 0
					item.Size = 0
				}
			}
		}
	}
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func validateTargetOrgFeatures(plan *Plan, scope *TargetScopeDiscovery) {
	if plan == nil || scope == nil {
		return
	}
	hasCustomRoutes := false
	hasCronSchedules := false
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, route := range instance.Routes {
				if route.Action == "create_backend" || route.Action == "create_redirect" {
					hasCustomRoutes = true
				}
			}
			for _, service := range instance.Services {
				if service.CronJobs > 0 && service.Enabled && service.Action != "skip" {
					hasCronSchedules = true
				}
			}
		}
	}
	if !hasCustomRoutes && !hasCronSchedules {
		return
	}
	capabilities := scope.Org.Capabilities
	if capabilities == nil {
		plan.addReview(
			SeverityBlocking, "", "", "target subscription capabilities",
			"target Wodby 2 API did not return organization capabilities; update the backend before migrating custom domains or cron schedules",
		)
		return
	}
	if hasCustomRoutes && !capabilities.CustomDomains {
		plan.addReview(
			SeverityConfirmation, "", "", "custom domains",
			"the target subscription does not allow active custom domains; migrated domains will be created disabled and can be enabled after upgrading the target plan",
		)
	}
	if hasCronSchedules && !capabilities.CronSchedules {
		plan.addReview(
			SeverityConfirmation, "", "", "cron schedules",
			"the target subscription does not allow cron execution; migrated schedules will be created disabled and can be enabled after upgrading the target plan",
		)
	}
}

func validateRepositoryTargets(apps []AppExport, targets map[string]RepositoryTargetPlan) error {
	if len(targets) == 0 {
		return nil
	}
	matched := make(map[string]int, len(targets))
	for _, appExport := range apps {
		app := appExport.App
		keys := []string{
			strings.ToLower(strings.TrimSpace(app.UUID)),
			strings.ToLower(strings.TrimSpace(app.Name)),
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			if _, exists := targets[key]; exists {
				matched[key]++
			}
		}
		uuidTarget, byUUID := targets[keys[0]]
		nameTarget, byName := targets[keys[1]]
		if byUUID && byName && keys[0] != keys[1] && uuidTarget != nameTarget {
			return fmt.Errorf("repository mappings for app %q conflict between its UUID and name", app.Name)
		}
	}
	for key := range targets {
		switch matched[key] {
		case 0:
			return fmt.Errorf("repository mapping source app %q was not found in the export", key)
		case 1:
		default:
			return fmt.Errorf("repository mapping source app %q is ambiguous; use the app UUID", key)
		}
	}
	return nil
}

func repositoryTargetForApp(app App, opts PlanOptions) RepositoryTargetPlan {
	if target, exists := opts.RepositoryByApp[strings.ToLower(strings.TrimSpace(app.UUID))]; exists {
		return target
	}
	if target, exists := opts.RepositoryByApp[strings.ToLower(strings.TrimSpace(app.Name))]; exists {
		return target
	}
	return opts.Repository
}

// AddReviewItems merges target preflight findings into a plan and recomputes
// its summary, status, and approval hash. Findings must contain metadata only;
// callers must never place secret values or backup download URLs in Details.
func (p *Plan) AddReviewItems(items ...ReviewItem) error {
	if p == nil {
		return fmt.Errorf("migration plan is required")
	}
	for _, item := range items {
		switch item.Severity {
		case SeverityBlocking, SeverityMigration, SeverityConfirmation, SeverityServiceWarning, SeverityManual, SeveritySkipped:
		default:
			return fmt.Errorf("unsupported migration review severity %q", item.Severity)
		}
		p.Review = append(p.Review, item)
	}
	sortReview(p.Review)
	p.Summary = PlanSummary{}
	p.computeSummary()
	digest, err := p.contentDigest()
	if err != nil {
		return fmt.Errorf("compute migration plan digest: %w", err)
	}
	p.PlanHash = digest
	return nil
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
			UUID:            instance.Stack.UUID,
			Name:            instance.Stack.Name,
			Type:            instance.Stack.Type,
			Version:         instance.Stack.Version,
			Custom:          instance.Stack.Custom,
			AncestorUUID:    instance.Stack.AncestorUUID,
			AncestorName:    instance.Stack.AncestorName,
			TargetID:        opts.TargetStackID,
			ExplicitMapping: opts.TargetStackID > 0,
		},
	}
	addSourceStackCompatibilityReview(plan, app, instance, opts.AllowUnsupportedDrupal)
	mappedStack, hasExplicitStack := scopedMapping(
		opts.TargetStackMap,
		app,
		instance.UUID,
		instance.Name,
		instance.Stack.Name,
	)
	if hasExplicitStack {
		targetID, err := strconv.Atoi(mappedStack)
		if err != nil || targetID <= 0 {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "target stack", fmt.Sprintf("target stack mapping %q must be a positive stack ID", mappedStack))
			instancePlan.Stack.TargetID = 0
		} else {
			instancePlan.Stack.TargetID = targetID
			instancePlan.Stack.ExplicitMapping = true
		}
	}
	if instancePlan.Stack.TargetID <= 0 {
		instancePlan.Stack.CatalogName = targetCatalogStackName(instance.Stack)
		instancePlan.Stack.CreateTarget = instancePlan.Stack.CatalogName != ""
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

	if opts.TargetScope != nil && instancePlan.Stack.TargetID <= 0 && !instancePlan.Stack.CreateTarget {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "target stack", "this custom stack requires --target-stack-id or a scoped --target-stack-map entry; managed Drupal and WordPress stacks use a new catalog stack by default")
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
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "basic auth", "basic-auth password is unexpectedly redacted in the protected source export")
		} else if instance.BasicAuth.Password == "" {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "basic auth", "basic-auth password is empty or unavailable")
		} else {
			plan.addReview(SeverityMigration, app.Name, instance.Name, "basic auth", "Wodby 2 route auths will be created for protected custom and technical domains; the password will be transferred only in memory and remain omitted from the plan and state files")
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
	serviceTargets := serviceTargetNamesFromPlans(instancePlan.Services)
	validateInstanceProperties(plan, app, instance, serviceTargets)
	for _, service := range services {
		if message, ok := smtpEndpointMigrationReview(service, instance.Properties, serviceTargets); ok {
			plan.addReview(SeverityMigration, app.Name, instance.Name, "service "+service.Name+" environment", message)
		}
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
	validateInstanceRoutes(plan, app, instance, instancePlan.Routes)

	for _, backup := range instance.Backups {
		instancePlan.Imports = append(instancePlan.Imports, buildImportPlan(plan, app, instance, backup, opts))
	}
	if len(instance.Backups) == 0 && opts.RequireData && !opts.SkipData {
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"source backup",
			"at least one successful Wodby 1 backup is required to map data components; create a backup or use --skip-data",
		)
	} else if opts.RequireData && !opts.SkipData {
		plan.addReview(
			SeverityConfirmation,
			app.Name,
			instance.Name,
			"selected source backup",
			selectedBackupWarning(instance.Backups),
		)
		if selectedBackupUUIDCount(instance.Backups) > 1 {
			plan.addReview(
				SeverityServiceWarning,
				app.Name,
				instance.Name,
				"mixed backup components",
				"database and files were selected from different successful Wodby 1 backups; review the component completion times because the imported data may represent different application moments",
			)
		}
	}
	return instancePlan
}

func selectedBackupWarning(backups []Backup) string {
	backupUUID := ""
	backupUUIDs := map[string]bool{}
	completed := int64(0)
	for _, backup := range backups {
		if backupUUID == "" {
			backupUUID = strings.TrimSpace(backup.BackupUUID)
		}
		if strings.TrimSpace(backup.BackupUUID) != "" {
			backupUUIDs[strings.TrimSpace(backup.BackupUUID)] = true
		}
		candidate := backup.BackupUpdated
		if candidate <= 0 {
			candidate = backup.Updated
		}
		if candidate <= 0 {
			candidate = backup.BackupCreated
		}
		if candidate <= 0 {
			candidate = backup.Created
		}
		if candidate > completed {
			completed = candidate
		}
	}
	when := "an unknown time"
	if completed > 0 {
		when = time.Unix(completed, 0).UTC().Format(time.RFC3339)
	}
	if len(backupUUIDs) > 1 {
		return fmt.Sprintf(
			"%d data components were selected and pinned independently from %d successful backups; changes after each component completion time shown below will not be migrated",
			len(backups), len(backupUUIDs),
		)
	}
	return fmt.Sprintf(
		"backup %s completed at %s; changes made in Wodby 1 after this snapshot will not be migrated. To minimize the gap, optionally enable maintenance mode, create a backup manually, and rerun the preview",
		firstNonEmpty(backupUUID, "(unknown UUID)"), when,
	)
}

func selectedBackupUUIDCount(backups []Backup) int {
	ids := map[string]bool{}
	for _, backup := range backups {
		if id := strings.TrimSpace(backup.BackupUUID); id != "" {
			ids[id] = true
		}
	}
	return len(ids)
}

func buildServicePlan(plan *Plan, app App, instance Instance, service Service, opts PlanOptions) ServicePlan {
	servicePlan := ServicePlan{
		SourceName:    service.Name,
		SourceVersion: strings.TrimSpace(service.Version),
		TargetName:    service.Name,
		Enabled:       targetServiceEnabled(instance, service),
		Action:        "migrate",
		Settings:      migratableServiceSettingCount(service.Configuration),
	}
	if version, found := scopedMapping(opts.TargetVersionMap, app, instance.UUID, instance.Name, service.Name); found {
		servicePlan.TargetVersion = strings.TrimSpace(version)
		servicePlan.VersionExplicit = true
	}
	mapped, explicitlyMapped := scopedMapping(opts.TargetServiceMap, app, instance.UUID, instance.Name, service.Name)
	confirmation := ""
	confirmationSeverity := SeverityMigration
	if explicitlyMapped {
		servicePlan.TargetName = mapped
		servicePlan.Action = "map"
		confirmation = fmt.Sprintf("source service will use explicitly selected Wodby 2 service %q", mapped)
	} else if customStackRequiresExplicitServiceMapping(instance.Stack) {
		servicePlan.TargetName = ""
		if !service.Enabled {
			servicePlan.Action = "skip_disabled"
			return servicePlan
		}
		servicePlan.Action = "requires_mapping"
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"service "+service.Name,
			fmt.Sprintf(
				"fully custom Wodby 1 stack service requires an explicit target; pass --target-service-map %s=%s (service images, ports, workloads, and volumes are not inferred)",
				service.Name,
				"TARGET_SERVICE",
			),
		)
	} else {
		switch service.Name {
		case "apache":
			if usesNginxInsteadOfApache(app, instance, opts) {
				servicePlan.TargetName = ""
				servicePlan.Action = "skip"
				plan.addReview(skippedServiceSeverity(service), app.Name, instance.Name, "service apache", "Apache is intentionally not migrated because the Wodby 2 app will use nginx")
				return servicePlan
			}
		case "athenapdf":
			servicePlan.TargetName = "gotenberg"
			servicePlan.Action = "substitute"
			confirmationSeverity = SeverityConfirmation
			confirmation = "Gotenberg is not API-compatible with AthenaPDF; see https://wodby.com/docs/2.0/stacks/catalog/gotenberg/#migrate-from-athenapdf"
		case "mailhog":
			servicePlan.TargetName = "mailpit"
			servicePlan.Action = "substitute"
			confirmation = "mailhog will be substituted with mailpit"
		case "memcache":
			servicePlan.TargetName = "memcached"
			servicePlan.Action = "substitute"
			confirmation = "memcache will be substituted with memcached"
		case "pma":
			servicePlan.TargetName = "phpmyadmin"
			servicePlan.Action = "substitute"
			confirmation = "pma will be substituted with the managed phpmyadmin service"
		case "redis":
			servicePlan.TargetName = "valkey"
			servicePlan.Action = "substitute"
			confirmation = "redis will be substituted with valkey"
		case "crond":
			servicePlan.TargetName = ""
			servicePlan.Action = "skip"
			plan.addReview(skippedServiceSeverity(service), app.Name, instance.Name, "service crond", "the Wodby 1 crond container is not migrated; application cron jobs are migrated as Wodby 2 service cron schedules")
			return servicePlan
		case "rsyslog", "xhprof":
			servicePlan.TargetName = ""
			servicePlan.Action = "skip"
			plan.addReview(skippedServiceSeverity(service), app.Name, instance.Name, "service "+service.Name, service.Name+" is intentionally not migrated")
			return servicePlan
		case "sshd":
			servicePlan.TargetName = "sshd"
			servicePlan.Action = "substitute"
			confirmation = "sshd will be mapped to the target PHP SSH derivative service"
		case "varnish":
			servicePlan.TargetName = "vinyl"
			servicePlan.Action = "substitute"
			confirmation = "varnish will be substituted with vinyl"
		}
	}

	if !service.Enabled {
		servicePlan.Action = "skip_disabled"
		return servicePlan
	}
	if confirmation != "" {
		plan.addReview(
			confirmationSeverity,
			app.Name,
			instance.Name,
			"service "+service.Name,
			confirmation,
		)
	}

	reportedRedacted := map[string]bool{}
	for _, envVar := range service.EnvVars {
		if sourceEnvVarBlockedByTargetReservation(instance.Properties, envVar) {
			plan.addReview(
				SeverityBlocking,
				app.Name,
				instance.Name,
				"service "+service.Name+" env var "+envVar.Name,
				fmt.Sprintf(
					"source custom environment variable %q uses the Wodby 2 reserved WODBY namespace and will not be migrated; rename it to a non-reserved name in Wodby 1, update every application and command reference, then rerun the migration",
					envVar.Name,
				),
			)
			continue
		}
		if !sourceEnvVarRequiresMigration(instance.Properties, envVar) {
			continue
		}
		servicePlan.EnvVars++
		if envVar.IsRedacted() {
			reportedRedacted[envVar.Name] = true
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "env var "+envVar.Name, "secret or protected env var value is unexpectedly redacted in the protected source export")
		}
	}

	secretsRedacted := append([]string(nil), service.SecretsRedacted...)
	sort.Strings(secretsRedacted)
	for _, redacted := range secretsRedacted {
		if reportedRedacted[redacted] {
			continue
		}
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "secret "+redacted, "protected source export reports a redacted service secret")
	}

	for _, cron := range service.CronJobs {
		if cron.Classification == "source_only_infrastructure" {
			continue
		}
		if cron.Enabled {
			servicePlan.CronJobs++
			if strings.TrimSpace(cron.Crontab) == "" || strings.TrimSpace(cron.Command) == "" {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "cron "+firstNonEmpty(cron.Title, cron.Crontab), "enabled source cron requires both a schedule and command")
				continue
			}
			targetState := "enabled"
			if opts.TargetScope == nil || opts.TargetScope.Org.Capabilities == nil {
				targetState = "pending subscription capability check"
			} else if !opts.TargetScope.Org.Capabilities.CronSchedules {
				targetState = "disabled by target subscription"
			}
			servicePlan.CronSchedules = append(servicePlan.CronSchedules, CronSchedulePlan{
				Title:       firstNonEmpty(strings.TrimSpace(cron.Title), "Migrated Wodby 1 cron"),
				Schedule:    strings.TrimSpace(cron.Crontab),
				Command:     migratedEnvironmentReferences(strings.TrimSpace(cron.Command)),
				TargetState: targetState,
			})
		}
	}
	if len(service.Configuration) != 0 {
		plan.addReview(SeverityMigration, app.Name, instance.Name, "service "+service.Name, fmt.Sprintf("%d service setting override(s) will be applied to the shared target stack", len(service.Configuration)))
	}
	if servicePlan.EnvVars > 0 {
		plan.addReview(SeverityMigration, app.Name, instance.Name, "service "+service.Name, fmt.Sprintf("%d custom environment variable(s) will be reconciled on the mapped target service", servicePlan.EnvVars))
	}
	if servicePlan.CronJobs > 0 {
		noun := "cron schedules"
		if servicePlan.CronJobs == 1 {
			noun = "cron schedule"
		}
		message := fmt.Sprintf(
			"%d Wodby 2 %s will be added to target service %q",
			servicePlan.CronJobs,
			noun,
			servicePlan.TargetName,
		)
		if opts.TargetScope != nil && opts.TargetScope.Org.Capabilities != nil {
			if opts.TargetScope.Org.Capabilities.CronSchedules {
				message += " in enabled state"
			} else {
				message += " in disabled state because the target subscription does not allow cron execution"
			}
		}
		plan.addReview(SeverityMigration, app.Name, instance.Name, "service "+service.Name, message)
	}
	return servicePlan
}

func skippedServiceSeverity(service Service) string {
	if service.Enabled {
		return SeverityServiceWarning
	}
	return SeveritySkipped
}

func customStackRequiresExplicitServiceMapping(stack Stack) bool {
	if !stack.Custom {
		return false
	}
	return sourceStackFamily(stack) == ""
}

// sourceStackFamily identifies the managed application family independently
// of a customer fork's display name. New Wodby 1 exports provide metadata.type;
// ancestor and canonical names remain fallbacks for older exports.
func sourceStackFamily(stack Stack) string {
	if strings.TrimSpace(stack.Type) != "" {
		// Metadata is authoritative when present. An unknown type must use
		// explicit custom mappings rather than falling through to an ancestor.
		return canonicalStackFamily(stack.Type)
	}
	if family := canonicalStackFamily(stack.AncestorName); family != "" {
		return family
	}
	if !stack.Custom {
		return canonicalStackFamily(stack.Name)
	}
	// Older exports did not include stack.type. Accept only a canonical managed
	// name here so an arbitrary customer display name cannot unlock managed rules.
	return canonicalStackFamily(stack.Name)
}

func canonicalStackFamily(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	switch compact {
	case "drupal7":
		return "drupal7"
	case "drupal8":
		return "drupal8"
	case "drupal9":
		return "drupal9"
	case "drupal", "drupal10", "drupal11":
		return "drupal"
	case "wordpress":
		return "wordpress"
	default:
		return ""
	}
}

func targetCatalogStackName(stack Stack) string {
	switch sourceStackFamily(stack) {
	case "drupal", "drupal7", "drupal8", "drupal9":
		return "drupal11"
	case "wordpress":
		return "wordpress"
	default:
		return ""
	}
}

func addSourceStackCompatibilityReview(plan *Plan, app App, instance Instance, allowUnsupported bool) {
	family := sourceStackFamily(instance.Stack)
	switch family {
	case "drupal7":
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"source stack compatibility",
			"Drupal 7 cannot be migrated into a Wodby 2 managed Drupal stack; upgrade it through a supported path or migrate it manually",
		)
	case "drupal8", "drupal9":
		major := strings.TrimPrefix(family, "drupal")
		if !allowUnsupported {
			plan.addReview(
				SeverityBlocking,
				app.Name,
				instance.Name,
				"source stack compatibility",
				fmt.Sprintf("Wodby 2 does not support Drupal %s stack metadata; confirm the application code already runs Drupal 10 or newer, then rerun with --allow-unsupported-drupal", major),
			)
			return
		}
		plan.addReview(
			SeverityConfirmation,
			app.Name,
			instance.Name,
			"IMPORTANT: source Drupal version",
			fmt.Sprintf("Drupal %s stack metadata is being overridden; the CLI does not inspect application code, so verify the application already runs Drupal 10 or newer before applying", major),
		)
	}
}

func sourceEnvVarRequiresMigration(properties map[string]interface{}, envVar EnvVar) bool {
	return sourceEnvVarWouldOtherwiseMigrate(properties, envVar) &&
		!isWodby2ReservedEnvironmentName(envVar.Name)
}

func sourceEnvVarWouldOtherwiseMigrate(properties map[string]interface{}, envVar EnvVar) bool {
	if !envVar.Enabled {
		return false
	}
	if isWodby1GeneratedEnvironmentName(envVar.Name) {
		return false
	}
	origin := strings.ToLower(strings.TrimSpace(envVar.Origin))
	if origin != "default" && origin != "computed" {
		return true
	}
	switch envVar.Name {
	case "PHP_OPCACHE_ENABLE":
		enabled, ok := properties["php_opcache"].(bool)
		return ok && !enabled
	default:
		enabled, ok := properties["php_xdebug"].(bool)
		return ok && enabled && strings.HasPrefix(envVar.Name, "PHP_XDEBUG")
	}
}

func validateInstanceProperties(plan *Plan, app App, instance Instance, serviceTargets map[string]string) {
	for _, item := range []struct {
		name         string
		defaultValue bool
		onNonDefault func(bool)
	}{
		{
			name:         "git_autopull",
			defaultValue: false,
			onNonDefault: func(bool) {
				plan.addReview(SeverityMigration, app.Name, instance.Name, "Git auto-pull", "the connected Wodby 2 build will fetch the approved Git ref instead of copying the Wodby 1 auto-pull flag")
			},
		},
		{
			name:         "php_opcache",
			defaultValue: true,
			onNonDefault: func(bool) {
				plan.addReview(SeverityMigration, app.Name, instance.Name, "PHP OPcache", "the non-default OPcache setting will be applied through the mapped PHP service environment")
			},
		},
		{
			name:         "php_xdebug",
			defaultValue: false,
			onNonDefault: func(bool) {
				plan.addReview(SeverityMigration, app.Name, instance.Name, "PHP Xdebug", "the enabled Xdebug environment will be applied through the mapped PHP service")
			},
		},
	} {
		value, found := instance.Properties[item.name]
		if !found {
			continue
		}
		enabled, ok := value.(bool)
		if !ok {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance property "+item.name, "source property must be a boolean")
			continue
		}
		if enabled != item.defaultValue {
			item.onNonDefault(enabled)
		}
	}

	validateCacheServiceToggleProperty(plan, app, instance, "cache_redis", "redis", serviceTargets)
	validateCacheServiceToggleProperty(plan, app, instance, "cache_valkey", "valkey", serviceTargets)

	if raw, found := instance.Properties["mail_service"]; found {
		service, ok := raw.(string)
		if !ok {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance property mail_service", "source property must be a service name")
		} else if service = strings.TrimSpace(service); service != "" {
			if !sourceServiceEnabled(instance.Services, service) {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "mail service", fmt.Sprintf("source mail service %q is not present as an enabled exported service", service))
			} else if targetService := strings.TrimSpace(serviceTargets[strings.ToLower(service)]); targetService == "" {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "mail service", fmt.Sprintf("source mail service %q does not resolve to a target Wodby 2 service", service))
			} else {
				plan.addReview(SeverityMigration, app.Name, instance.Name, "mail service", fmt.Sprintf("source mail service %q will map to Wodby 2 service %q", service, targetService))
			}
		}
	} else if raw, found := instance.Properties["php_mail_catcher"]; found {
		enabled, ok := raw.(bool)
		if !ok {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance property php_mail_catcher", "source property must be a boolean")
		} else {
			service := "opensmtpd"
			if enabled {
				service = "mailhog"
			}
			if !sourceServiceEnabled(instance.Services, service) {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "mail service", fmt.Sprintf("legacy php_mail_catcher=%t selects source service %q, but that service is not enabled", enabled, service))
			} else if targetService := strings.TrimSpace(serviceTargets[service]); targetService == "" {
				plan.addReview(SeverityBlocking, app.Name, instance.Name, "mail service", fmt.Sprintf("legacy php_mail_catcher=%t selects source service %q, which has no target Wodby 2 mapping", enabled, service))
			} else {
				plan.addReview(SeverityMigration, app.Name, instance.Name, "mail service", fmt.Sprintf("legacy php_mail_catcher=%t selects source service %q, which will map to Wodby 2 service %q", enabled, service, targetService))
			}
		}
	}
}

func targetServiceEnabled(instance Instance, service Service) bool {
	if !service.Enabled {
		return false
	}
	property := ""
	switch strings.ToLower(strings.TrimSpace(service.Name)) {
	case "redis":
		property = "cache_redis"
	case "valkey":
		property = "cache_valkey"
	default:
		return true
	}
	raw, found := instance.Properties[property]
	if !found {
		return true
	}
	enabled, valid := raw.(bool)
	if !valid {
		// Validation adds a blocker. Preserve the source service state here so
		// malformed input cannot silently change the reviewed target state.
		return true
	}
	return enabled
}

func validateCacheServiceToggleProperty(
	plan *Plan,
	app App,
	instance Instance,
	property string,
	service string,
	serviceTargets map[string]string,
) {
	if !sourceServiceEnabled(instance.Services, service) {
		return
	}
	raw, found := instance.Properties[property]
	if !found {
		return
	}
	enabled, ok := raw.(bool)
	if !ok {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "instance property "+property, "source property must be a boolean")
		return
	}
	target := strings.TrimSpace(serviceTargets[strings.ToLower(service)])
	if target == "" {
		target = service
	}
	if !enabled {
		plan.addReview(
			SeverityMigration,
			app.Name,
			instance.Name,
			"instance property "+property,
			fmt.Sprintf("source %s=false; mapped Wodby 2 service %q will be disabled", property, target),
		)
		return
	}
	plan.addReview(
		SeverityMigration,
		app.Name,
		instance.Name,
		"instance property "+property,
		fmt.Sprintf("enabled source cache integration will be preserved through mapped service %q", target),
	)
}

func sourceServiceEnabled(services []Service, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return service.Enabled
		}
	}
	return false
}

func usesNginxInsteadOfApache(app App, instance Instance, opts PlanOptions) bool {
	if !sourceServiceEnabled(instance.Services, "nginx") {
		return false
	}
	_, explicitlyMapped := scopedMapping(
		opts.TargetServiceMap,
		app,
		instance.UUID,
		instance.Name,
		"apache",
	)
	return !explicitlyMapped
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
		SSL:             domain.SSL || domain.SSLCustom,
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
	if domain.Service == "apache" && usesNginxInsteadOfApache(app, instance, opts) {
		routePlan.Service = "nginx"
	}
	if strings.EqualFold(strings.TrimSpace(domain.Type), "technical") {
		routePlan.Action = "skip_technical"
		routePlan.NeedsPortID = false
		plan.addReview(SeveritySkipped, app.Name, instance.Name, "route "+domain.Name, "technical source route is intentionally excluded from the migration plan")
		if routePlan.BasicAuth && (strings.TrimSpace(routePlan.Service) == "" || routePlan.PortNumber == nil) {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name+" basic auth", "protected technical route is missing its service or port and cannot be mapped to a Wodby 2 route auth")
		}
		return routePlan
	}
	if domain.Service == "apache" && routePlan.Service == "nginx" {
		plan.addReview(
			SeverityMigration,
			app.Name,
			instance.Name,
			"route "+domain.Name,
			"Apache-backed source route will use the migrated Wodby 2 nginx service",
		)
	}
	host := strings.TrimSpace(domain.Name)
	if host == "" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route", "source route host is empty")
	} else if strings.HasPrefix(strings.ToLower(host), "*.") {
		routePlan.ReviewRequired = true
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"route "+domain.Name,
			"wildcard source routes cannot pass the automated DNS and certificate cutover checks",
		)
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

	if protocol := strings.ToLower(strings.TrimSpace(domain.ServiceProtocol)); protocol != "" && protocol != "http" {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, fmt.Sprintf("source route protocol %q is not supported by Wodby 2 app routes", domain.ServiceProtocol))
	}

	if domain.Service == "" || domain.PortNumber == nil {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "route target service and port number are required for automated migration")
	}
	if domain.Protected && !basicAuth {
		routePlan.ReviewRequired = true
		plan.addReview(SeverityBlocking, app.Name, instance.Name, "route "+domain.Name, "protected source route has no transferable instance basic-auth credentials")
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
	if domain.HSTS {
		value := TargetRouteSettingHSTSEnabled
		if domain.HSTSSubdomains {
			value = TargetRouteSettingHSTSIncludeSubdomains
		}
		routePlan.Settings = append(routePlan.Settings, RouteSettingPlan{Name: TargetRouteSettingHSTS, Value: value})
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

func validateInstanceRoutes(plan *Plan, app App, instance Instance, routes []RoutePlan) {
	hosts := map[string]int{}
	primary := 0
	for _, route := range routes {
		if route.Action != "create_backend" && route.Action != "create_redirect" {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(route.Host))
		hosts[host]++
		if route.Primary {
			primary++
		}
	}
	for host, count := range hosts {
		if count > 1 {
			plan.addReview(
				SeverityBlocking,
				app.Name,
				instance.Name,
				"route "+host,
				fmt.Sprintf("%d enabled source routes use the same hostname and root path", count),
			)
		}
	}
	if primary > 1 {
		plan.addReview(
			SeverityBlocking,
			app.Name,
			instance.Name,
			"primary route",
			fmt.Sprintf("%d enabled custom source routes are marked primary", primary),
		)
	}
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

func repositoryNameFromURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	repositoryPath := ""
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		repositoryPath = parsed.Path
	} else if separator := strings.Index(value, ":"); separator >= 0 {
		repositoryPath = value[separator+1:]
	}
	repositoryPath = strings.Trim(strings.TrimSpace(repositoryPath), "/")
	if repositoryPath == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(repositoryPath); err == nil {
		repositoryPath = decoded
	}
	if len(repositoryPath) > len(".git") && strings.EqualFold(repositoryPath[len(repositoryPath)-len(".git"):], ".git") {
		repositoryPath = repositoryPath[:len(repositoryPath)-len(".git")]
	}
	return strings.Trim(strings.TrimSpace(repositoryPath), "/")
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

func buildImportPlan(plan *Plan, app App, instance Instance, backup Backup, opts PlanOptions) ImportPlan {
	subject := "backup " + firstNonEmpty(backup.Component, backup.UUID)
	importPlan := ImportPlan{
		SourceUUID:    backup.UUID,
		Component:     backup.Component,
		BackupUUID:    backup.BackupUUID,
		BackupCreated: backup.BackupCreated,
		Size:          backup.Size,
		Action:        "import",
	}
	if opts.SkipData {
		importPlan.Action = "skip"
		plan.addReview(SeveritySkipped, app.Name, instance.Name, subject, "source backup data is intentionally excluded from this migration")
		return importPlan
	}
	if mapped, found := scopedMapping(opts.TargetImportMap, app, instance.UUID, instance.Name, backup.Component); found {
		service, importName, ok := strings.Cut(mapped, ":")
		service = strings.TrimSpace(service)
		importName = strings.TrimSpace(importName)
		if !ok || service == "" || importName == "" {
			plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "target import mapping must use TARGET_SERVICE:TARGET_IMPORT format")
		} else {
			importPlan.TargetService = service
			importPlan.TargetImport = importName
		}
	}
	if strings.TrimSpace(backup.UUID) == "" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup UUID is required")
	}
	if strings.TrimSpace(backup.BackupUUID) == "" {
		plan.addReview(SeverityBlocking, app.Name, instance.Name, subject, "source backup snapshot UUID is required")
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
	return importPlan
}

func scopedMapping(mapping map[string]string, app App, instanceUUID string, instanceName string, source string) (string, bool) {
	if len(mapping) == 0 {
		return "", false
	}
	keys := []string{
		strings.ToLower(strings.TrimSpace(app.UUID + "/" + instanceUUID + "/" + source)),
		strings.ToLower(strings.TrimSpace(app.UUID + "/" + instanceName + "/" + source)),
		strings.ToLower(strings.TrimSpace(app.Name + "/" + instanceUUID + "/" + source)),
		strings.ToLower(strings.TrimSpace(app.Name + "/" + instanceName + "/" + source)),
		strings.ToLower(strings.TrimSpace(app.UUID + "/" + source)),
		strings.ToLower(strings.TrimSpace(app.Name + "/" + source)),
		strings.ToLower(strings.TrimSpace(instanceUUID + "/" + source)),
		strings.ToLower(strings.TrimSpace(instanceName + "/" + source)),
		strings.ToLower(strings.TrimSpace(instanceUUID)),
		strings.ToLower(strings.TrimSpace(instanceName)),
		strings.ToLower(strings.TrimSpace(source)),
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if target, found := mapping[key]; found && strings.TrimSpace(target) != "" {
			return strings.TrimSpace(target), true
		}
	}
	return "", false
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
	case SeverityMigration:
		return "1"
	case SeverityConfirmation:
		return "2"
	case SeverityServiceWarning:
		return "3"
	case SeverityManual:
		return "4"
	case SeveritySkipped:
		return "5"
	default:
		return "6:" + severity
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
			p.Summary.Imports += len(instance.Imports)
		}
	}
	for _, item := range p.Review {
		switch item.Severity {
		case SeverityBlocking:
			p.Summary.Blocking++
		case SeverityMigration:
			p.Summary.Migrations++
		case SeverityServiceWarning:
			p.Summary.ServiceWarnings++
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
