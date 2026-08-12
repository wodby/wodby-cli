package wodby1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	TargetBuildSourcePublic  = "PUBLIC"
	TargetBuildSourceClone   = "CLONE"
	TargetBuildSourceConnect = "CONNECT"

	TargetGitRefBranch = "BRANCH"
	TargetGitRefTag    = "TAG"
	TargetGitRefCommit = "COMMIT"

	TargetRouteActionBackend  = "BACKEND"
	TargetRouteActionRedirect = "REDIRECT"
	TargetRoutePathPrefix     = "PREFIX"
	TargetRoutePathExact      = "EXACT"

	TargetRouteSettingHTTPSRedirect   = "HTTPS_REDIRECT"
	TargetRouteSettingNoIndex         = "NO_INDEX"
	TargetRouteSettingRequestBodySize = "REQUEST_BODY_SIZE"
	TargetRouteSettingSessionAffinity = "SESSION_AFFINITY"
	TargetRouteSettingPathRewrite     = "PATH_REWRITE"
	TargetRouteSettingHSTS            = "HSTS"

	TargetRouteSettingHSTSEnabled           = "enabled"
	TargetRouteSettingHSTSIncludeSubdomains = "include_subdomains"
)

// TargetAmbiguousMatchError indicates that a natural-key lookup returned more
// than one exact match. Mutation orchestration must stop instead of adopting an
// arbitrary resource.
type TargetAmbiguousMatchError struct {
	Resource string
	Name     string
	Count    int
}

func (e *TargetAmbiguousMatchError) Error() string {
	return fmt.Sprintf("target Wodby 2 %s name %q matched %d resources", e.Resource, e.Name, e.Count)
}

// TargetStack is the subset of the Wodby 2 stack response needed to resolve
// the immutable revision used for a migrated instance.
type TargetStack struct {
	ID                 int       `json:"id"`
	Name               string    `json:"name"`
	Title              string    `json:"title"`
	Status             string    `json:"status"`
	Public             bool      `json:"public"`
	RevID              int       `json:"revId"`
	DraftRevID         *int      `json:"draftRevId,omitempty"`
	LatestRevNumber    int       `json:"latestRevNumber"`
	OriginStackRevName *string   `json:"originStackRevName,omitempty"`
	OriginStackRevID   *int      `json:"originStackRevId,omitempty"`
	OrgID              int       `json:"orgId"`
	RevisionManifest   string    `json:"-"`
	CreatedAt          time.Time `json:"createdAt"`
}

type TargetDuplicateStackInput struct {
	OrgID       int  `json:"orgId"`
	ProjectID   *int `json:"projectId,omitempty"`
	SourceRevID int  `json:"sourceRevId"`
}

type TargetCatalogService struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Title           string    `json:"title"`
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	External        bool      `json:"external"`
	Public          bool      `json:"public"`
	RevID           int       `json:"revId"`
	LatestRevNumber int       `json:"latestRevNumber"`
	OrgID           int       `json:"orgId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type TargetCreateStackServiceInput struct {
	StackID          int    `json:"stackId"`
	ServiceID        int    `json:"serviceId"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Required         bool   `json:"required"`
	Replicas         int    `json:"replicas"`
	ServiceRevPinned *bool  `json:"serviceRevPinned,omitempty"`
}

// TargetStackRevision is the immutable stack revision read back by ID before a
// reviewed migration plan is allowed to mutate the target.
type TargetStackRevision struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Number   int    `json:"number"`
	Draft    bool   `json:"draft"`
	Version  string `json:"version"`
	StackID  int    `json:"stackId"`
	Manifest string `json:"manifest,omitempty"`
}

type TargetStacksResponse struct {
	Items      []TargetStack `json:"items"`
	TotalCount int           `json:"totalCount"`
	NextPage   *int          `json:"nextPage,omitempty"`
}

// ResolvePublicStackExact selects an immutable catalog blueprint by its exact
// public machine name. Public catalog ownership is intentionally independent
// of the customer's target organization.
func (c *TargetClient) ResolvePublicStackExact(ctx context.Context, name string) (TargetStack, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TargetStack{}, errors.New("public catalog stack name is required")
	}
	items := []TargetStack{}
	if err := c.client.Get(ctx, "/catalog/stacks", nil, &items); err != nil {
		return TargetStack{}, errors.Wrap(err, "list public Wodby 2 catalog stacks")
	}
	matches := make([]TargetStack, 0, 1)
	for _, item := range items {
		if err := validateTargetStack(item); err != nil {
			return TargetStack{}, err
		}
		if !item.Public {
			return TargetStack{}, errors.Errorf("catalog endpoint returned non-public stack %q", item.Name)
		}
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetStack{}, errors.Errorf("public Wodby 2 catalog stack %q was not found", name)
	case 1:
		return matches[0], nil
	default:
		return TargetStack{}, &TargetAmbiguousMatchError{Resource: "public catalog stack", Name: name, Count: len(matches)}
	}
}

func (c *TargetClient) DuplicateStack(ctx context.Context, stackID int, input TargetDuplicateStackInput) (TargetStack, error) {
	if err := targetRequirePositiveID("catalog stack", stackID); err != nil {
		return TargetStack{}, err
	}
	if err := targetRequirePositiveID("organization", input.OrgID); err != nil {
		return TargetStack{}, err
	}
	if err := targetRequirePositiveID("source stack revision", input.SourceRevID); err != nil {
		return TargetStack{}, err
	}
	if err := targetValidateOptionalPositiveID("project", input.ProjectID); err != nil {
		return TargetStack{}, err
	}
	var item TargetStack
	if err := c.client.Post(ctx, "/stacks/"+strconv.Itoa(stackID)+"/actions/duplicate", nil, input, &item); err != nil {
		return TargetStack{}, errors.Wrap(err, "duplicate public Wodby 2 catalog stack")
	}
	if err := validateTargetStack(item); err != nil {
		return TargetStack{}, err
	}
	if item.OrgID != input.OrgID || item.Public {
		return TargetStack{}, errors.New("duplicated target stack ownership does not match the migration target")
	}
	if item.OriginStackRevID == nil || *item.OriginStackRevID != input.SourceRevID {
		return TargetStack{}, errors.Errorf("duplicated target stack does not identify catalog revision ID %d as its origin", input.SourceRevID)
	}
	return item, nil
}

func (c *TargetClient) ResolveServiceExact(ctx context.Context, targetOrgID int, name string) (TargetCatalogService, TargetServiceRevision, error) {
	if err := targetRequirePositiveID("organization", targetOrgID); err != nil {
		return TargetCatalogService{}, TargetServiceRevision{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return TargetCatalogService{}, TargetServiceRevision{}, errors.New("target service name is required")
	}
	var service TargetCatalogService
	if err := c.client.Get(ctx, "/services/by-name/"+url.PathEscape(name), nil, &service); err != nil {
		return TargetCatalogService{}, TargetServiceRevision{}, errors.Wrapf(err, "resolve target Wodby 2 service %q", name)
	}
	if err := validateTargetCatalogService(service, targetOrgID, name); err != nil {
		return TargetCatalogService{}, TargetServiceRevision{}, err
	}
	revision, err := c.GetServiceRevision(ctx, service.RevID)
	if err != nil {
		return TargetCatalogService{}, TargetServiceRevision{}, err
	}
	if revision.ServiceID != service.ID {
		return TargetCatalogService{}, TargetServiceRevision{}, errors.New("target service revision belongs to a different service")
	}
	return service, revision, nil
}

func (c *TargetClient) CreateStackService(ctx context.Context, input TargetCreateStackServiceInput) (TargetStackService, error) {
	if err := targetRequirePositiveID("stack", input.StackID); err != nil {
		return TargetStackService{}, err
	}
	if err := targetRequirePositiveID("service", input.ServiceID); err != nil {
		return TargetStackService{}, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Title) == "" || input.Replicas < 0 {
		return TargetStackService{}, errors.New("target stack service name, title, and non-negative replicas are required")
	}
	var item TargetStackService
	if err := c.client.Post(ctx, "/stack-services", nil, input, &item); err != nil {
		return TargetStackService{}, errors.Wrap(err, "add target Wodby 2 stack service")
	}
	if err := validateTargetStackService(item); err != nil {
		return TargetStackService{}, err
	}
	if item.Name != input.Name {
		return TargetStackService{}, errors.Errorf("created target stack service name %q does not match %q", item.Name, input.Name)
	}
	return item, nil
}

func (c *TargetClient) PublishStackDraft(ctx context.Context, stackID int) (TargetStack, error) {
	if err := targetRequirePositiveID("stack", stackID); err != nil {
		return TargetStack{}, err
	}
	var item TargetStack
	if err := c.client.Post(ctx, "/stacks/"+strconv.Itoa(stackID)+"/actions/publish-draft", nil, nil, &item); err != nil {
		return TargetStack{}, errors.Wrap(err, "publish target Wodby 2 stack draft")
	}
	if item.ID != stackID {
		return TargetStack{}, targetUnexpectedID("stack", item.ID, stackID)
	}
	if err := validateTargetStack(item); err != nil {
		return TargetStack{}, err
	}
	if item.DraftRevID != nil {
		return TargetStack{}, errors.New("published target stack still reports an unpublished draft")
	}
	return item, nil
}

func (c *TargetClient) SetStackServiceOptions(ctx context.Context, stackServiceID int, options []TargetStackServiceOptionInput) error {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return err
	}
	if len(options) == 0 {
		return errors.New("target stack service options are required")
	}
	seen := map[string]bool{}
	defaults := 0
	for _, option := range options {
		version := strings.TrimSpace(option.Version)
		if version == "" || seen[version] {
			return errors.New("target stack service option versions must be non-empty and unique")
		}
		seen[version] = true
		if option.Default {
			defaults++
			if option.Disabled {
				return errors.New("target stack service default option cannot be disabled")
			}
		}
	}
	if defaults != 1 {
		return errors.New("target stack service options require exactly one default")
	}
	var result TargetOperationResult
	body := struct {
		Options []TargetStackServiceOptionInput `json:"options"`
	}{Options: options}
	if err := c.client.Put(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/options", nil, body, &result); err != nil {
		return errors.Wrap(err, "set target Wodby 2 stack service options")
	}
	if !result.Success {
		return errors.New("target Wodby 2 stack service options update was not successful")
	}
	return nil
}

func (c *TargetClient) SetStackServiceSetting(ctx context.Context, stackServiceID int, name, value string) error {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return err
	}
	name, err := targetSafePathName("stack service setting", name)
	if err != nil {
		return err
	}
	var result TargetOperationResult
	body := struct {
		Value *string `json:"value,omitempty"`
	}{Value: &value}
	if err := c.client.Put(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/settings/"+name, nil, body, &result); err != nil {
		return errors.Wrap(err, "set target Wodby 2 stack service setting")
	}
	if !result.Success {
		return errors.New("target Wodby 2 stack service setting update was not successful")
	}
	return nil
}

func (c *TargetClient) ListStackServiceEnvVars(ctx context.Context, stackServiceID int) ([]TargetStackServiceEnvVar, error) {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return nil, err
	}
	items := []TargetStackServiceEnvVar{}
	if err := c.client.Get(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/env-vars", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 stack service environment variables")
	}
	for _, item := range items {
		if err := validateTargetStackServiceEnvVar(item, 0); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateStackServiceEnvVar(ctx context.Context, stackServiceID int, input TargetCreateStackServiceEnvVarInput) (TargetStackServiceEnvVar, error) {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return TargetStackServiceEnvVar{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return TargetStackServiceEnvVar{}, errors.New("target stack environment variable name is required")
	}
	var item TargetStackServiceEnvVar
	if err := c.client.Post(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/env-vars", nil, input, &item); err != nil {
		return TargetStackServiceEnvVar{}, errors.Wrap(err, "create target Wodby 2 stack service environment variable")
	}
	if err := validateTargetStackServiceEnvVar(item, 0); err != nil {
		return TargetStackServiceEnvVar{}, err
	}
	if item.Name != input.Name || !sameOptionalString(item.EnvType, input.EnvType) {
		return TargetStackServiceEnvVar{}, errors.New("created target stack environment variable identity does not match the request")
	}
	return item, nil
}

func (c *TargetClient) UpdateStackServiceEnvVar(ctx context.Context, envVarID int, input TargetUpdateStackServiceEnvVarInput) (TargetStackServiceEnvVar, error) {
	if err := targetRequirePositiveID("stack service environment variable", envVarID); err != nil {
		return TargetStackServiceEnvVar{}, err
	}
	var item TargetStackServiceEnvVar
	if err := c.client.Put(ctx, "/stack-service-env-vars/"+strconv.Itoa(envVarID), nil, input, &item); err != nil {
		return TargetStackServiceEnvVar{}, errors.Wrap(err, "update target Wodby 2 stack service environment variable")
	}
	if err := validateTargetStackServiceEnvVar(item, 0); err != nil {
		return TargetStackServiceEnvVar{}, err
	}
	return item, nil
}

func (c *TargetClient) ListStackServiceCronSchedules(ctx context.Context, stackServiceID int) ([]TargetStackServiceCronSchedule, error) {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return nil, err
	}
	items := []TargetStackServiceCronSchedule{}
	if err := c.client.Get(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/cron-schedules", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 stack service cron schedules")
	}
	for _, item := range items {
		if err := validateTargetStackServiceCronSchedule(item, 0); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateStackServiceCronSchedule(ctx context.Context, stackServiceID int, input TargetCreateStackServiceCronScheduleInput) (TargetStackServiceCronSchedule, error) {
	if err := targetRequirePositiveID("stack service", stackServiceID); err != nil {
		return TargetStackServiceCronSchedule{}, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Crontab) == "" || strings.TrimSpace(input.Command) == "" {
		return TargetStackServiceCronSchedule{}, errors.New("target stack cron name, title, crontab, and command are required")
	}
	var item TargetStackServiceCronSchedule
	if err := c.client.Post(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/cron-schedules", nil, input, &item); err != nil {
		return TargetStackServiceCronSchedule{}, errors.Wrap(err, "create target Wodby 2 stack service cron schedule")
	}
	if err := validateTargetStackServiceCronSchedule(item, 0); err != nil {
		return TargetStackServiceCronSchedule{}, err
	}
	if item.Name != input.Name || !sameOptionalString(item.EnvType, input.EnvType) {
		return TargetStackServiceCronSchedule{}, errors.New("created target stack cron schedule identity does not match the request")
	}
	return item, nil
}

func (c *TargetClient) UpdateStackServiceCronSchedule(ctx context.Context, scheduleID int, input TargetUpdateStackServiceCronScheduleInput) (TargetStackServiceCronSchedule, error) {
	if err := targetRequirePositiveID("stack service cron schedule", scheduleID); err != nil {
		return TargetStackServiceCronSchedule{}, err
	}
	var item TargetStackServiceCronSchedule
	if err := c.client.Put(ctx, "/stack-service-cron-schedules/"+strconv.Itoa(scheduleID), nil, input, &item); err != nil {
		return TargetStackServiceCronSchedule{}, errors.Wrap(err, "update target Wodby 2 stack service cron schedule")
	}
	if err := validateTargetStackServiceCronSchedule(item, 0); err != nil {
		return TargetStackServiceCronSchedule{}, err
	}
	return item, nil
}

// TargetStackService describes one service in an immutable stack revision.
// ID is the stack-service ID accepted by NewAppServiceInput.
type TargetStackService struct {
	ID                       int                         `json:"id"`
	Name                     string                      `json:"name"`
	Title                    string                      `json:"title"`
	Type                     string                      `json:"type"`
	Main                     bool                        `json:"main"`
	Disabled                 bool                        `json:"disabled"`
	Required                 bool                        `json:"required"`
	Replicas                 int                         `json:"replicas"`
	ServiceRevID             int                         `json:"serviceRevId"`
	ServiceRevName           string                      `json:"serviceRevName"`
	ServiceRevVersion        string                      `json:"serviceRevVersion"`
	BuildSourceIntegrationID *int                        `json:"buildSourceIntegrationId,omitempty"`
	BuildSourceRemoteRepoID  *string                     `json:"buildSourceRemoteRepoId,omitempty"`
	Options                  []TargetStackServiceOption  `json:"options,omitempty"`
	Settings                 []TargetStackServiceSetting `json:"settings,omitempty"`
}

type TargetStackServiceOption struct {
	ID             int    `json:"id"`
	StackServiceID int    `json:"stackServiceId"`
	Version        string `json:"version"`
	Default        bool   `json:"default"`
	Disabled       bool   `json:"disabled"`
}

type TargetStackServiceSetting struct {
	ID             int    `json:"id"`
	StackServiceID int    `json:"stackServiceId"`
	Name           string `json:"name"`
	Value          string `json:"value"`
}

type TargetServiceBuildCapability struct {
	Connect bool `json:"connect"`
}

type TargetServiceImportCapability struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Volume      string   `json:"volume"`
	Workload    string   `json:"workload,omitempty"`
	Destination string   `json:"destination,omitempty"`
	Extensions  []string `json:"extensions,omitempty"`
}

type TargetServiceCronSchedule struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type TargetServiceOption struct {
	Version  string    `json:"version"`
	Tag      string    `json:"tag,omitempty"`
	EOL      time.Time `json:"eol,omitempty"`
	Default  bool      `json:"default,omitempty"`
	Disabled bool      `json:"disabled,omitempty"`
}

type TargetStackServiceOptionInput struct {
	Version  string `json:"version"`
	Default  bool   `json:"default"`
	Disabled bool   `json:"disabled"`
}

type TargetStackServiceEnvVar struct {
	ID             int     `json:"id"`
	StackServiceID int     `json:"stackServiceId"`
	Workload       string  `json:"workload"`
	Container      string  `json:"container"`
	Name           string  `json:"name"`
	Value          *string `json:"value,omitempty"`
	ValueSecretID  *int    `json:"valueSecretId,omitempty"`
	EnvType        *string `json:"envType,omitempty"`
}

type TargetCreateStackServiceEnvVarInput struct {
	Workload  *string `json:"workload,omitempty"`
	Container *string `json:"container,omitempty"`
	Name      string  `json:"name"`
	Value     string  `json:"value"`
	Secret    bool    `json:"secret"`
	EnvType   *string `json:"envType,omitempty"`
}

type TargetUpdateStackServiceEnvVarInput struct {
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type TargetStackServiceCronSchedule struct {
	ID             int       `json:"id"`
	StackServiceID int       `json:"stackServiceId"`
	Name           string    `json:"name"`
	Title          string    `json:"title"`
	Crontab        string    `json:"crontab"`
	Command        string    `json:"command"`
	Workload       *string   `json:"workload,omitempty"`
	Disabled       bool      `json:"disabled"`
	EnvType        *string   `json:"envType,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TargetCreateStackServiceCronScheduleInput struct {
	Name     string  `json:"name"`
	Title    string  `json:"title"`
	Crontab  string  `json:"crontab"`
	Command  string  `json:"command"`
	Workload *string `json:"workload,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
	EnvType  *string `json:"envType,omitempty"`
}

type TargetUpdateStackServiceCronScheduleInput struct {
	Disabled *bool   `json:"disabled,omitempty"`
	Title    *string `json:"title,omitempty"`
	Crontab  *string `json:"crontab,omitempty"`
	Command  *string `json:"command,omitempty"`
	Workload *string `json:"workload,omitempty"`
	EnvType  *string `json:"envType,omitempty"`
}

type TargetServiceManifest struct {
	Name          string                               `json:"name"`
	Raw           string                               `json:"raw,omitempty"`
	Build         *TargetServiceBuildCapability        `json:"build,omitempty"`
	Imports       []TargetServiceImportCapability      `json:"imports,omitempty"`
	Backups       []TargetServiceBackupCapability      `json:"backups,omitempty"`
	Integrations  []TargetServiceIntegrationCapability `json:"integrations,omitempty"`
	CronSchedules []TargetServiceCronSchedule          `json:"cron,omitempty"`
	Options       []TargetServiceOption                `json:"options,omitempty"`
}

type TargetServiceBackupCapability struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

type TargetServiceIntegrationCapability struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TargetServiceRevision struct {
	ID        int                    `json:"id"`
	Name      string                 `json:"name"`
	Title     string                 `json:"title"`
	Type      string                 `json:"type"`
	External  bool                   `json:"external"`
	Number    int                    `json:"number"`
	Version   string                 `json:"version"`
	ServiceID int                    `json:"serviceId"`
	Manifest  *TargetServiceManifest `json:"manifest,omitempty"`
}

// TargetStackServiceInspection joins a stack-service identity with its service
// revision manifest, including build and import capabilities.
type TargetStackServiceInspection struct {
	StackService    TargetStackService
	ServiceRevision TargetServiceRevision
}

// TargetRemoteGitRepo is a repository exposed by a selected Wodby 2 Git
// integration. ID is provider-specific and is resolved by the CLI rather than
// accepted as customer input.
type TargetRemoteGitRepo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type targetRemoteGitRepoFilePresence struct {
	Exists bool `json:"exists"`
}

type TargetApp struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	ClusterApp bool      `json:"clusterApp"`
	OrgID      int       `json:"orgId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type TargetAppInstance struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	MainDomain     *string   `json:"mainDomain,omitempty"`
	AppID          int       `json:"appId"`
	ClusterID      int       `json:"clusterId"`
	EnvID          int       `json:"envId"`
	StackID        int       `json:"stackId"`
	StackRevID     int       `json:"stackRevId"`
	StackName      string    `json:"stackName"`
	StackRevNumber int       `json:"stackRevNumber"`
	StackVersion   string    `json:"stackVersion"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// TargetCreateAppInput deliberately has no domain or services field. The
// target API therefore generates the technical domain and expands the selected
// stack revision's current service defaults.
type TargetCreateAppInput struct {
	OrgID                  int    `json:"orgId"`
	Name                   string `json:"name"`
	Title                  string `json:"title,omitempty"`
	InstanceName           string `json:"instanceName"`
	InstanceTitle          string `json:"instanceTitle,omitempty"`
	ProjectID              *int   `json:"projectId,omitempty"`
	StackRevID             int    `json:"stackRevId"`
	ClusterID              int    `json:"clusterId"`
	EnvID                  int    `json:"envId"`
	CIIntegrationID        *int   `json:"ciIntegrationId,omitempty"`
	RegistryIntegrationID  *int   `json:"registryIntegrationId,omitempty"`
	DeferInitialDeployment bool   `json:"deferInitialDeployment,omitempty"`
}

// TargetCreateAppInstanceInput also omits domain and services so Wodby 2
// applies its server-side technical-domain and stack-service defaults.
type TargetCreateAppInstanceInput struct {
	AppID                  int    `json:"appId"`
	InstanceName           string `json:"instanceName"`
	InstanceTitle          string `json:"instanceTitle,omitempty"`
	StackRevID             int    `json:"stackRevId"`
	ClusterID              int    `json:"clusterId"`
	EnvID                  int    `json:"envId"`
	CIIntegrationID        *int   `json:"ciIntegrationId,omitempty"`
	RegistryIntegrationID  *int   `json:"registryIntegrationId,omitempty"`
	DeferInitialDeployment bool   `json:"deferInitialDeployment,omitempty"`
}

type TargetAppService struct {
	ID                 int       `json:"id"`
	Name               string    `json:"name"`
	Title              string    `json:"title"`
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Replicas           int       `json:"replicas"`
	Version            string    `json:"version"`
	Main               bool      `json:"main"`
	Disabled           bool      `json:"disabled"`
	External           bool      `json:"external"`
	Required           bool      `json:"required"`
	NeedsRebuild       bool      `json:"needsRebuild"`
	NeedsRedeploy      bool      `json:"needsRedeploy"`
	ConfigurationReady bool      `json:"configurationReady"`
	AppInstanceID      int       `json:"appInstanceId"`
	ServiceRevID       int       `json:"serviceRevId"`
	ParentAppServiceID *int      `json:"parentAppServiceId,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type TargetBuildSourceInput struct {
	BuildSourceType string  `json:"buildSourceType"`
	Boilerplate     *string `json:"boilerplate,omitempty"`
	NewRepoName     *string `json:"newRepoName,omitempty"`
	IntegrationID   *int    `json:"integrationId,omitempty"`
	RemoteGitRepoID *string `json:"remoteGitRepoId,omitempty"`
	GitRef          *string `json:"gitRef,omitempty"`
	GitRefType      *string `json:"gitRefType,omitempty"`
}

type TargetAppServiceUpdateInput struct {
	Replicas    *int                    `json:"replicas,omitempty"`
	Version     *string                 `json:"version,omitempty"`
	Disabled    *bool                   `json:"disabled,omitempty"`
	Main        *bool                   `json:"main,omitempty"`
	BuildSource *TargetBuildSourceInput `json:"buildSource,omitempty"`
}

type TargetAppServiceEnvVar struct {
	ID            int                           `json:"id"`
	AppServiceID  int                           `json:"appServiceId"`
	Workload      string                        `json:"workload"`
	Container     string                        `json:"container"`
	Name          string                        `json:"name"`
	Value         string                        `json:"value"`
	ValueSecretID *int                          `json:"valueSecretId,omitempty"`
	Runtime       bool                          `json:"runtime"`
	Build         bool                          `json:"build"`
	EnvType       *string                       `json:"envType,omitempty"`
	Source        *TargetAppServiceEnvVarSource `json:"source,omitempty"`
	CreatedAt     *time.Time                    `json:"createdAt,omitempty"`
}

type TargetAppServiceEnvVarSource struct {
	FromService bool    `json:"fromService"`
	FromStack   bool    `json:"fromStack"`
	FromWodby   bool    `json:"fromWodby"`
	Setting     *string `json:"setting,omitempty"`
	Link        *string `json:"link,omitempty"`
	Integration *string `json:"integration,omitempty"`
}

type TargetCreateAppServiceEnvVarInput struct {
	Workload  *string `json:"workload,omitempty"`
	Container *string `json:"container,omitempty"`
	Name      string  `json:"name"`
	Value     string  `json:"value"`
	Secret    bool    `json:"secret"`
	Runtime   *bool   `json:"runtime,omitempty"`
	Build     *bool   `json:"build,omitempty"`
}

type TargetUpdateAppServiceEnvVarInput struct {
	Value   *string `json:"value,omitempty"`
	Secret  bool    `json:"secret"`
	Runtime *bool   `json:"runtime,omitempty"`
	Build   *bool   `json:"build,omitempty"`
}

type TargetAppServiceSetting struct {
	ID            int    `json:"id"`
	AppServiceID  int    `json:"appServiceId"`
	Name          string `json:"name"`
	Value         string `json:"value"`
	Var           string `json:"var"`
	Runtime       bool   `json:"runtime"`
	Build         bool   `json:"build"`
	FromSettingID *int   `json:"fromSettingId,omitempty"`
}

type TargetAppServiceCronSchedule struct {
	ID           int       `json:"id"`
	AppServiceID int       `json:"appServiceId"`
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	Crontab      string    `json:"crontab"`
	Command      string    `json:"command"`
	Workload     *string   `json:"workload,omitempty"`
	Disabled     bool      `json:"disabled"`
	EnvType      *string   `json:"envType,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type TargetCreateAppServiceCronScheduleInput struct {
	Name     *string `json:"name,omitempty"`
	Title    string  `json:"title"`
	Crontab  string  `json:"crontab"`
	Command  string  `json:"command"`
	Workload *string `json:"workload,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

type TargetUpdateAppServiceCronScheduleInput struct {
	Disabled *bool   `json:"disabled,omitempty"`
	Title    *string `json:"title,omitempty"`
	Crontab  *string `json:"crontab,omitempty"`
	Command  *string `json:"command,omitempty"`
	Workload *string `json:"workload,omitempty"`
}

type TargetAppPort struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Number        int    `json:"number"`
	PublicPort    *int   `json:"publicPort,omitempty"`
	Private       bool   `json:"private"`
	AppEndpointID int    `json:"appEndpointId"`
	AppInstanceID int    `json:"appInstanceId"`
	AppServiceID  int    `json:"appServiceId"`
}

type TargetCert struct {
	ID            int        `json:"id"`
	Issuer        string     `json:"issuer"`
	KeyType       string     `json:"keyType"`
	KeyLength     int        `json:"keyLength"`
	Status        string     `json:"status"`
	AppInstanceID *int       `json:"appInstanceId,omitempty"`
	AppServiceID  *int       `json:"appServiceId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	IssuedAt      *time.Time `json:"issuedAt,omitempty"`
	RenewsAt      *time.Time `json:"renewsAt,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
}

type TargetAppRoute struct {
	ID                 int         `json:"id"`
	Host               string      `json:"host"`
	Path               string      `json:"path"`
	PathType           string      `json:"pathType"`
	Action             string      `json:"action"`
	RedirectScheme     *string     `json:"redirectScheme,omitempty"`
	RedirectHost       *string     `json:"redirectHost,omitempty"`
	RedirectPath       *string     `json:"redirectPath,omitempty"`
	RedirectStatusCode *int        `json:"redirectStatusCode,omitempty"`
	Status             string      `json:"status"`
	Disabled           bool        `json:"disabled"`
	Main               bool        `json:"main"`
	Primary            bool        `json:"primary"`
	Private            bool        `json:"private"`
	AppInstanceID      int         `json:"appInstanceId"`
	AppServiceID       int         `json:"appServiceId"`
	PortID             int         `json:"portId"`
	Cert               *TargetCert `json:"cert,omitempty"`
	CreatedAt          time.Time   `json:"createdAt"`
	UpdatedAt          time.Time   `json:"updatedAt"`
	LastSyncedAt       *time.Time  `json:"lastSyncedAt,omitempty"`
}

type TargetCreateAppRouteInput struct {
	AppServiceID       int     `json:"appServiceId"`
	Disabled           *bool   `json:"disabled,omitempty"`
	Main               bool    `json:"main"`
	Primary            bool    `json:"primary"`
	Port               int     `json:"port"`
	Host               string  `json:"host"`
	Path               *string `json:"path,omitempty"`
	PathType           *string `json:"pathType,omitempty"`
	Action             *string `json:"action,omitempty"`
	RedirectScheme     *string `json:"redirectScheme,omitempty"`
	RedirectHost       *string `json:"redirectHost,omitempty"`
	RedirectPath       *string `json:"redirectPath,omitempty"`
	RedirectStatusCode *int    `json:"redirectStatusCode,omitempty"`
	LetsEncrypt        *bool   `json:"letsencrypt,omitempty"`
}

type TargetUpdateAppRouteInput struct {
	Disabled           *bool   `json:"disabled,omitempty"`
	Main               *bool   `json:"main,omitempty"`
	Primary            *bool   `json:"primary,omitempty"`
	Path               *string `json:"path,omitempty"`
	PathType           *string `json:"pathType,omitempty"`
	Action             *string `json:"action,omitempty"`
	RedirectScheme     *string `json:"redirectScheme,omitempty"`
	RedirectHost       *string `json:"redirectHost,omitempty"`
	RedirectPath       *string `json:"redirectPath,omitempty"`
	RedirectStatusCode *int    `json:"redirectStatusCode,omitempty"`
}

type TargetAppRouteSetting struct {
	ID            int       `json:"id"`
	AppInstanceID int       `json:"appInstanceId"`
	RouteID       int       `json:"routeId"`
	Default       bool      `json:"default"`
	Name          string    `json:"name"`
	Value         string    `json:"value"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type TargetAppAuth struct {
	ID            int       `json:"id"`
	AppInstanceID int       `json:"appInstanceId"`
	AppServiceID  *int      `json:"appServiceId,omitempty"`
	AppRouteID    *int      `json:"appRouteId,omitempty"`
	Login         string    `json:"login"`
	Realm         string    `json:"realm"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type TargetCreateAppAuthInput struct {
	AppInstanceID int    `json:"appInstanceId"`
	AppServiceID  *int   `json:"appServiceId,omitempty"`
	AppRouteID    *int   `json:"appRouteId,omitempty"`
	Login         string `json:"login"`
	Password      string `json:"password"`
	Realm         string `json:"realm"`
}

type TargetUpdateAppAuthInput struct {
	AppServiceID *int    `json:"appServiceId,omitempty"`
	AppRouteID   *int    `json:"appRouteId,omitempty"`
	Login        string  `json:"login"`
	Password     *string `json:"password,omitempty"`
	Realm        string  `json:"realm"`
}

type TargetImport struct {
	ID                     int        `json:"id"`
	Name                   string     `json:"name"`
	Source                 string     `json:"source"`
	Status                 string     `json:"status"`
	AppInstanceID          *int       `json:"appInstanceId,omitempty"`
	AppServiceID           *int       `json:"appServiceId,omitempty"`
	DatabaseID             *int       `json:"databaseId,omitempty"`
	DatabaseDBID           *int       `json:"databaseDbId,omitempty"`
	AppServiceDeploymentID *int       `json:"appServiceDeploymentId,omitempty"`
	TaskID                 *int       `json:"taskId,omitempty"`
	BackupID               *int       `json:"backupId,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	StartedAt              *time.Time `json:"startedAt,omitempty"`
	EndedAt                *time.Time `json:"endedAt,omitempty"`
}

type TargetImportFilters struct {
	AppInstanceID *int
	AppServiceID  *int
	DatabaseID    *int
	DatabaseDBID  *int
}

type TargetStartURLImportInput struct {
	AppServiceID int
	ImportName   string
	URL          string
}

type TargetOperationResult struct {
	Success bool `json:"success"`
	TaskID  *int `json:"taskId,omitempty"`
}

type TargetTask struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Progress      int        `json:"progress"`
	UserID        int        `json:"userId"`
	OrgID         *int       `json:"orgId,omitempty"`
	AppID         *int       `json:"appId,omitempty"`
	AppInstanceID *int       `json:"appInstanceId,omitempty"`
	ClusterID     *int       `json:"clusterId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
}

type TargetAppServiceBuild struct {
	ID           int       `json:"id"`
	Status       string    `json:"status"`
	Image        string    `json:"image"`
	ImageDeleted bool      `json:"imageDeleted"`
	Size         int       `json:"size"`
	AppServiceID int       `json:"appServiceId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type TargetAppBuild struct {
	ID               int                     `json:"id"`
	Number           int                     `json:"number"`
	Status           string                  `json:"status"`
	AppInstanceID    int                     `json:"appInstanceId"`
	AppServiceID     int                     `json:"appServiceId"`
	TaskID           *int                    `json:"taskId,omitempty"`
	AppServiceBuilds []TargetAppServiceBuild `json:"appServiceBuilds"`
	GitRefType       string                  `json:"gitRefType"`
	GitRef           string                  `json:"gitRef"`
	CommitHash       string                  `json:"commitHash"`
	CommitMessage    string                  `json:"commitMessage"`
	CreatedAt        time.Time               `json:"createdAt"`
	UpdatedAt        time.Time               `json:"updatedAt"`
	StartedAt        *time.Time              `json:"startedAt,omitempty"`
	EndedAt          *time.Time              `json:"endedAt,omitempty"`
}

type TargetAppBuildsResponse struct {
	Items      []TargetAppBuild `json:"items"`
	TotalCount int              `json:"totalCount"`
	NextPage   *int             `json:"nextPage,omitempty"`
}

type TargetAppBuildsCreateResponse struct {
	Items  []TargetAppBuild `json:"items"`
	TaskID *int             `json:"taskId,omitempty"`
}

type TargetPageOptions struct {
	Page     int
	PageSize int
}

type TargetAppServiceDeploymentInput struct {
	AppServiceID       int   `json:"appServiceId"`
	AppServiceBuildID  *int  `json:"appServiceBuildId,omitempty"`
	SkipPostDeployment *bool `json:"skipPostDeployment,omitempty"`
	Force              bool  `json:"force"`
}

type TargetCreateAppDeploymentInput struct {
	Services     []TargetAppServiceDeploymentInput `json:"services"`
	SkipRollback *bool                             `json:"skipRollback,omitempty"`
}

type TargetAppServiceDeployment struct {
	ID                 int        `json:"id"`
	JobName            string     `json:"jobName"`
	Status             string     `json:"status"`
	AppServiceID       int        `json:"appServiceId"`
	AppServiceBuildID  *int       `json:"appServiceBuildId,omitempty"`
	SkipPostDeployment bool       `json:"skipPostDeployment"`
	Force              bool       `json:"force"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	EndedAt            *time.Time `json:"endedAt,omitempty"`
}

type TargetAppDeployment struct {
	ID                    int                          `json:"id"`
	Number                int                          `json:"number"`
	Status                string                       `json:"status"`
	RollbackStatus        string                       `json:"rollbackStatus"`
	PostDeploymentStatus  string                       `json:"postDeploymentStatus"`
	SkipRollback          bool                         `json:"skipRollback"`
	AppInstanceID         int                          `json:"appInstanceId"`
	Builds                []TargetAppBuild             `json:"builds"`
	TaskID                *int                         `json:"taskId,omitempty"`
	PostDeploymentTaskID  *int                         `json:"postDeploymentTaskId,omitempty"`
	AppServiceDeployments []TargetAppServiceDeployment `json:"appServiceDeployments"`
	CreatedAt             time.Time                    `json:"createdAt"`
	UpdatedAt             time.Time                    `json:"updatedAt"`
	StartedAt             *time.Time                   `json:"startedAt,omitempty"`
	EndedAt               *time.Time                   `json:"endedAt,omitempty"`
}

type TargetAppDeploymentsResponse struct {
	Items      []TargetAppDeployment `json:"items"`
	TotalCount int                   `json:"totalCount"`
	NextPage   *int                  `json:"nextPage,omitempty"`
}

// ResolveStackRevisionByName resolves an exact stack name from the selected
// organization or project catalog. The list endpoint is required because custom
// Wodby 2 stack names are namespace-qualified (for example "acme/drupal") and
// therefore cannot be represented safely by the API's single path-segment
// by-name route.
func (c *TargetClient) ResolveStackRevisionByName(
	ctx context.Context,
	orgID int,
	projectID int,
	name string,
) (TargetStack, error) {
	stacks, err := c.listStackRevisionCandidates(ctx, orgID, projectID, name)
	if err != nil {
		return TargetStack{}, err
	}
	matches := make([]TargetStack, 0, len(stacks))
	for _, stack := range stacks {
		if stack.Name == name {
			matches = append(matches, stack)
		}
	}
	if err := validateTargetStackMatches(matches, orgID); err != nil {
		return TargetStack{}, err
	}
	switch len(matches) {
	case 0:
		return TargetStack{}, errors.Errorf("target Wodby 2 stack name %q was not found in the selected target catalog", name)
	case 1:
		return matches[0], nil
	default:
		return TargetStack{}, &TargetAmbiguousMatchError{Resource: "stack", Name: name, Count: len(matches)}
	}
}

func (c *TargetClient) listStackRevisionCandidates(
	ctx context.Context,
	orgID int,
	projectID int,
	search string,
) ([]TargetStack, error) {
	if err := targetRequirePositiveID("organization", orgID); err != nil {
		return nil, err
	}
	if projectID < 0 {
		return nil, errors.New("target project ID cannot be negative")
	}
	if search == "" || search != strings.TrimSpace(search) {
		return nil, errors.New("target stack name must be non-empty without surrounding whitespace")
	}

	page := 1
	stacks := []TargetStack{}
	seenPages := map[int]bool{}
	for {
		if seenPages[page] {
			return nil, errors.New("target Wodby 2 stack response contains a pagination cycle")
		}
		seenPages[page] = true
		query := url.Values{
			"orgId":    []string{strconv.Itoa(orgID)},
			"search":   []string{search},
			"page":     []string{strconv.Itoa(page)},
			"pageSize": []string{"100"},
		}
		if projectID > 0 {
			query.Set("projectIds", strconv.Itoa(projectID))
		}
		var response TargetStacksResponse
		if err := c.client.Get(ctx, "/stacks", query, &response); err != nil {
			return nil, errors.Wrap(err, "resolve target Wodby 2 stack revision")
		}
		if response.TotalCount < 0 {
			return nil, errors.New("target Wodby 2 stack response has a negative total count")
		}
		if err := targetValidateOptionalPositiveID("next page", response.NextPage); err != nil {
			return nil, err
		}
		stacks = append(stacks, response.Items...)
		if response.NextPage == nil {
			break
		}
		if *response.NextPage <= page {
			return nil, errors.New("target Wodby 2 stack response has a non-increasing next page")
		}
		page = *response.NextPage
	}
	return stacks, nil
}

func (c *TargetClient) FindGeneratedStacksByOrigin(
	ctx context.Context,
	orgID int,
	projectID int,
	catalogName string,
	originRevisionID int,
) ([]TargetStack, error) {
	if err := targetRequirePositiveID("catalog stack revision", originRevisionID); err != nil {
		return nil, err
	}
	items, err := c.listStackRevisionCandidates(ctx, orgID, projectID, strings.TrimSpace(catalogName))
	if err != nil {
		return nil, err
	}
	matches := make([]TargetStack, 0, 1)
	for _, item := range items {
		if err := validateTargetStack(item); err != nil {
			return nil, err
		}
		if item.OrgID != orgID || item.Public || item.OriginStackRevID == nil || *item.OriginStackRevID != originRevisionID {
			continue
		}
		matches = append(matches, item)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	return matches, nil
}

func validateTargetStackMatches(matches []TargetStack, orgID int) error {
	for _, stack := range matches {
		if err := validateTargetStack(stack); err != nil {
			return err
		}
		if stack.OrgID != orgID {
			return errors.Errorf(
				"target Wodby 2 stack %q belongs to organization ID %d, expected organization ID %d",
				stack.Name,
				stack.OrgID,
				orgID,
			)
		}
	}
	return nil
}

func (c *TargetClient) GetStack(ctx context.Context, stackID int) (TargetStack, error) {
	if err := targetRequirePositiveID("stack", stackID); err != nil {
		return TargetStack{}, err
	}
	var item TargetStack
	if err := c.client.Get(ctx, "/stacks/"+strconv.Itoa(stackID), nil, &item); err != nil {
		return TargetStack{}, errors.Wrap(err, "get target Wodby 2 stack")
	}
	if item.ID != stackID {
		return TargetStack{}, targetUnexpectedID("stack", item.ID, stackID)
	}
	if err := validateTargetStack(item); err != nil {
		return TargetStack{}, err
	}
	return item, nil
}

func (c *TargetClient) GetStackRevision(
	ctx context.Context,
	stackRevID int,
) (TargetStackRevision, error) {
	return c.getStackRevision(ctx, stackRevID, false)
}

func (c *TargetClient) getStackRevision(
	ctx context.Context,
	stackRevID int,
	allowDraft bool,
) (TargetStackRevision, error) {
	if err := targetRequirePositiveID("stack revision", stackRevID); err != nil {
		return TargetStackRevision{}, err
	}
	var item TargetStackRevision
	if err := c.client.Get(ctx, "/stack-revisions/"+strconv.Itoa(stackRevID), nil, &item); err != nil {
		return TargetStackRevision{}, errors.Wrap(err, "get target Wodby 2 stack revision")
	}
	if item.ID != stackRevID {
		return TargetStackRevision{}, targetUnexpectedID("stack revision", item.ID, stackRevID)
	}
	if err := targetRequirePositiveID("stack", item.StackID); err != nil {
		return TargetStackRevision{}, err
	}
	if item.Number <= 0 {
		return TargetStackRevision{}, errors.Errorf(
			"target Wodby 2 stack revision ID %d returned an invalid revision number",
			stackRevID,
		)
	}
	if item.Draft && !allowDraft {
		return TargetStackRevision{}, errors.Errorf(
			"target Wodby 2 stack revision ID %d is a draft",
			stackRevID,
		)
	}
	return item, nil
}

func (c *TargetClient) ListStackRevisionServices(ctx context.Context, stackRevID int) ([]TargetStackService, error) {
	if err := targetRequirePositiveID("stack revision", stackRevID); err != nil {
		return nil, err
	}
	items := []TargetStackService{}
	if err := c.client.Get(ctx, "/stack-revisions/"+strconv.Itoa(stackRevID)+"/services", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 stack revision services")
	}
	for _, item := range items {
		if err := validateTargetStackService(item); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (c *TargetClient) GetServiceRevision(ctx context.Context, serviceRevID int) (TargetServiceRevision, error) {
	if err := targetRequirePositiveID("service revision", serviceRevID); err != nil {
		return TargetServiceRevision{}, err
	}
	var item TargetServiceRevision
	if err := c.client.Get(ctx, "/service-revisions/"+strconv.Itoa(serviceRevID), nil, &item); err != nil {
		return TargetServiceRevision{}, errors.Wrap(err, "get target Wodby 2 service revision")
	}
	if item.ID != serviceRevID {
		return TargetServiceRevision{}, targetUnexpectedID("service revision", item.ID, serviceRevID)
	}
	if err := targetRequirePositiveID("service", item.ServiceID); err != nil {
		return TargetServiceRevision{}, err
	}
	if item.Manifest != nil {
		manifest, err := decodeTargetServiceManifest(item.Manifest)
		if err != nil {
			return TargetServiceRevision{}, errors.Wrapf(
				err,
				"target Wodby 2 service revision ID %d returned an invalid raw manifest",
				serviceRevID,
			)
		}
		item.Manifest = manifest
		for _, capability := range item.Manifest.Imports {
			if strings.TrimSpace(capability.Name) == "" {
				return TargetServiceRevision{}, errors.Errorf("target Wodby 2 service revision ID %d returned an import capability without a name", serviceRevID)
			}
		}
		for _, capability := range item.Manifest.Backups {
			if strings.TrimSpace(capability.Name) == "" {
				return TargetServiceRevision{}, errors.Errorf("target Wodby 2 service revision ID %d returned a backup capability without a name", serviceRevID)
			}
		}
		for _, capability := range item.Manifest.Integrations {
			if strings.TrimSpace(capability.Name) == "" || strings.TrimSpace(capability.Type) == "" {
				return TargetServiceRevision{}, errors.Errorf("target Wodby 2 service revision ID %d returned an invalid integration capability", serviceRevID)
			}
		}
	}
	return item, nil
}

func decodeTargetServiceManifest(projected *TargetServiceManifest) (*TargetServiceManifest, error) {
	if projected == nil || strings.TrimSpace(projected.Raw) == "" {
		return projected, nil
	}

	manifest := &TargetServiceManifest{}
	if err := json.Unmarshal([]byte(projected.Raw), manifest); err != nil {
		return nil, err
	}
	manifest.Raw = projected.Raw
	return manifest, nil
}

func (c *TargetClient) InspectStackRevision(ctx context.Context, stackRevID int) ([]TargetStackServiceInspection, error) {
	services, err := c.ListStackRevisionServices(ctx, stackRevID)
	if err != nil {
		return nil, err
	}
	items := make([]TargetStackServiceInspection, 0, len(services))
	for _, service := range services {
		revision, err := c.GetServiceRevision(ctx, service.ServiceRevID)
		if err != nil {
			return nil, errors.Wrapf(err, "inspect target stack service %q", service.Name)
		}
		items = append(items, TargetStackServiceInspection{
			StackService:    service,
			ServiceRevision: revision,
		})
	}
	return items, nil
}

// ListRemoteGitRepos returns the repositories visible through a Wodby 2 Git
// integration, matching the repository selector used by the new-app form.
func (c *TargetClient) ListRemoteGitRepos(ctx context.Context, integrationID int) ([]TargetRemoteGitRepo, error) {
	if integrationID <= 0 {
		return nil, errors.New("target Git integration ID must be positive")
	}
	items := []TargetRemoteGitRepo{}
	path := fmt.Sprintf("/integrations/%d/options/remote-git-repos", integrationID)
	if err := c.client.Get(ctx, path, nil, &items); err != nil {
		return nil, errors.Wrapf(err, "list repositories from target Wodby 2 Git integration ID %d", integrationID)
	}
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" {
			return nil, errors.Errorf("target Wodby 2 Git integration ID %d returned a repository without an ID or name", integrationID)
		}
	}
	return items, nil
}

// RemoteGitRepoFileExists checks a path at an exact repository ref through a
// Wodby 2 Git integration, so private repositories never expose credentials to
// the migration client.
func (c *TargetClient) RemoteGitRepoFileExists(ctx context.Context, integrationID int, remoteGitRepoID, filePath, ref string) (bool, error) {
	if integrationID <= 0 {
		return false, errors.New("target Git integration ID must be positive")
	}
	remoteGitRepoID = strings.TrimSpace(remoteGitRepoID)
	if remoteGitRepoID == "" {
		return false, errors.New("target remote Git repository ID is required")
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return false, errors.New("target remote Git repository file path is required")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false, errors.New("target remote Git repository ref is required")
	}

	query := url.Values{}
	query.Set("remoteGitRepoId", remoteGitRepoID)
	query.Set("path", filePath)
	query.Set("ref", ref)
	path := fmt.Sprintf("/integrations/%d/options/remote-git-repo-file", integrationID)
	var result targetRemoteGitRepoFilePresence
	if err := c.client.Get(ctx, path, query, &result); err != nil {
		return false, errors.Wrapf(
			err,
			"check %q at ref %q in target Wodby 2 remote Git repository %q",
			filePath,
			ref,
			remoteGitRepoID,
		)
	}
	return result.Exists, nil
}

// FindStackServiceExact returns no match as found=false and refuses ambiguous
// exact names.
func FindStackServiceExact(items []TargetStackService, name string) (TargetStackService, bool, error) {
	if strings.TrimSpace(name) == "" {
		return TargetStackService{}, false, errors.New("target stack service name is required")
	}
	matches := make([]TargetStackService, 0, 1)
	for _, item := range items {
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetStackService{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return TargetStackService{}, false, &TargetAmbiguousMatchError{Resource: "stack service", Name: name, Count: len(matches)}
	}
}

func (c *TargetClient) CreateApp(ctx context.Context, input TargetCreateAppInput) (TargetApp, error) {
	if err := validateTargetCreateAppInput(input); err != nil {
		return TargetApp{}, err
	}
	var item TargetApp
	if err := c.client.Post(ctx, "/apps", nil, input, &item); err != nil {
		return TargetApp{}, errors.Wrap(err, "create target Wodby 2 app")
	}
	if err := validateTargetApp(item, input.OrgID); err != nil {
		return TargetApp{}, err
	}
	if item.Name != input.Name {
		return TargetApp{}, errors.Errorf("created target Wodby 2 app name %q does not exactly match %q", item.Name, input.Name)
	}
	return item, nil
}

// FindAppByID reads an app without turning a definitive 404 into a generic
// request failure. Migration resume uses this to distinguish a temporarily
// failing target API from a target app that the customer deleted.
func (c *TargetClient) FindAppByID(ctx context.Context, appID int) (TargetApp, bool, error) {
	if err := targetRequirePositiveID("app", appID); err != nil {
		return TargetApp{}, false, err
	}
	var item TargetApp
	if err := c.client.Get(ctx, "/apps/"+strconv.Itoa(appID), nil, &item); err != nil {
		if isTargetNotFound(err) {
			return TargetApp{}, false, nil
		}
		return TargetApp{}, false, errors.Wrap(err, "get target Wodby 2 app")
	}
	if item.ID != appID {
		return TargetApp{}, false, targetUnexpectedID("app", item.ID, appID)
	}
	if err := validateTargetApp(item, 0); err != nil {
		return TargetApp{}, false, err
	}
	return item, true, nil
}

func (c *TargetClient) FindAppExact(ctx context.Context, orgID int, name string) (TargetApp, bool, error) {
	if err := targetRequirePositiveID("organization", orgID); err != nil {
		return TargetApp{}, false, err
	}
	if strings.TrimSpace(name) == "" {
		return TargetApp{}, false, errors.New("target app name is required")
	}
	query := url.Values{"orgId": []string{strconv.Itoa(orgID)}}
	items := []TargetApp{}
	if err := c.client.Get(ctx, "/apps", query, &items); err != nil {
		return TargetApp{}, false, errors.Wrap(err, "list target Wodby 2 apps for exact lookup")
	}
	matches := make([]TargetApp, 0, 1)
	for _, item := range items {
		if err := validateTargetApp(item, orgID); err != nil {
			return TargetApp{}, false, err
		}
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetApp{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return TargetApp{}, false, &TargetAmbiguousMatchError{Resource: "app", Name: name, Count: len(matches)}
	}
}

func (c *TargetClient) CreateAppInstance(ctx context.Context, input TargetCreateAppInstanceInput) (TargetAppInstance, error) {
	if err := validateTargetCreateAppInstanceInput(input); err != nil {
		return TargetAppInstance{}, err
	}
	var item TargetAppInstance
	if err := c.client.Post(ctx, "/app-instances", nil, input, &item); err != nil {
		return TargetAppInstance{}, errors.Wrap(err, "create target Wodby 2 app instance")
	}
	if err := validateTargetAppInstance(item, input.AppID); err != nil {
		return TargetAppInstance{}, err
	}
	if item.Name != input.InstanceName {
		return TargetAppInstance{}, errors.Errorf("created target Wodby 2 app instance name %q does not exactly match %q", item.Name, input.InstanceName)
	}
	if item.ClusterID != input.ClusterID || item.EnvID != input.EnvID || item.StackRevID != input.StackRevID {
		return TargetAppInstance{}, errors.Errorf(
			"created target Wodby 2 app instance relationships do not match request (cluster=%d/%d env=%d/%d stackRev=%d/%d)",
			item.ClusterID, input.ClusterID, item.EnvID, input.EnvID, item.StackRevID, input.StackRevID,
		)
	}
	return item, nil
}

func (c *TargetClient) GetAppInstance(ctx context.Context, appInstanceID int) (TargetAppInstance, error) {
	if err := targetRequirePositiveID("app instance", appInstanceID); err != nil {
		return TargetAppInstance{}, err
	}
	var item TargetAppInstance
	if err := c.client.Get(ctx, "/app-instances/"+strconv.Itoa(appInstanceID), nil, &item); err != nil {
		return TargetAppInstance{}, errors.Wrap(err, "get target Wodby 2 app instance")
	}
	if item.ID != appInstanceID {
		return TargetAppInstance{}, targetUnexpectedID("app instance", item.ID, appInstanceID)
	}
	if err := validateTargetAppInstance(item, 0); err != nil {
		return TargetAppInstance{}, err
	}
	return item, nil
}

func (c *TargetClient) ListAppInstances(ctx context.Context, orgID, appID int) ([]TargetAppInstance, error) {
	if err := targetRequirePositiveID("organization", orgID); err != nil {
		return nil, err
	}
	if err := targetRequirePositiveID("app", appID); err != nil {
		return nil, err
	}
	query := url.Values{
		"appId": []string{strconv.Itoa(appID)},
		"orgId": []string{strconv.Itoa(orgID)},
	}
	items := []TargetAppInstance{}
	if err := c.client.Get(ctx, "/app-instances", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app instances")
	}
	for _, item := range items {
		if err := validateTargetAppInstance(item, appID); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// FindAppInstanceExact resolves both natural-key components in the selected
// organization and detects duplicate exact matches before resume adopts one.
func (c *TargetClient) FindAppInstanceExact(ctx context.Context, orgID int, appName, instanceName string) (TargetAppInstance, bool, error) {
	if strings.TrimSpace(instanceName) == "" {
		return TargetAppInstance{}, false, errors.New("target app instance name is required")
	}
	app, found, err := c.FindAppExact(ctx, orgID, appName)
	if err != nil || !found {
		return TargetAppInstance{}, false, err
	}
	items, err := c.ListAppInstances(ctx, orgID, app.ID)
	if err != nil {
		return TargetAppInstance{}, false, err
	}
	matches := make([]TargetAppInstance, 0, 1)
	for _, item := range items {
		if item.Name == instanceName {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetAppInstance{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return TargetAppInstance{}, false, &TargetAmbiguousMatchError{Resource: "app instance", Name: appName + "/" + instanceName, Count: len(matches)}
	}
}

func (c *TargetClient) ListAppServices(ctx context.Context, appInstanceID int) ([]TargetAppService, error) {
	query, err := targetRequiredQueryID("appInstanceId", "app instance", appInstanceID)
	if err != nil {
		return nil, err
	}
	items := []TargetAppService{}
	if err := c.client.Get(ctx, "/app-services", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app services")
	}
	for _, item := range items {
		if err := validateTargetAppService(item, appInstanceID); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (c *TargetClient) UpdateAppService(ctx context.Context, appServiceID int, input TargetAppServiceUpdateInput) (TargetAppService, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return TargetAppService{}, err
	}
	if err := validateTargetAppServiceUpdateInput(input); err != nil {
		return TargetAppService{}, err
	}
	var item TargetAppService
	if err := c.client.Put(ctx, "/app-services/"+strconv.Itoa(appServiceID), nil, input, &item); err != nil {
		return TargetAppService{}, errors.Wrap(err, "update target Wodby 2 app service")
	}
	if item.ID != appServiceID {
		return TargetAppService{}, targetUnexpectedID("app service", item.ID, appServiceID)
	}
	if err := validateTargetAppService(item, 0); err != nil {
		return TargetAppService{}, err
	}
	return item, nil
}

func (c *TargetClient) UpdateAppServiceBuildSource(ctx context.Context, appServiceID int, input TargetBuildSourceInput) (TargetAppService, error) {
	return c.UpdateAppService(ctx, appServiceID, TargetAppServiceUpdateInput{BuildSource: &input})
}

func (c *TargetClient) ListAppServiceEnvVars(ctx context.Context, appServiceID int) ([]TargetAppServiceEnvVar, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return nil, err
	}
	items := []TargetAppServiceEnvVar{}
	if err := c.client.Get(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/env-vars", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app service environment variables")
	}
	for _, item := range items {
		if err := validateTargetEnvVar(item, appServiceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateAppServiceEnvVar(ctx context.Context, appServiceID int, input TargetCreateAppServiceEnvVarInput) (TargetAppServiceEnvVar, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return TargetAppServiceEnvVar{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return TargetAppServiceEnvVar{}, errors.New("target environment variable name is required")
	}
	var item TargetAppServiceEnvVar
	if err := c.client.Post(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/env-vars", nil, input, &item); err != nil {
		return TargetAppServiceEnvVar{}, errors.Wrap(err, "create target Wodby 2 app service environment variable")
	}
	if err := validateTargetEnvVar(item, appServiceID); err != nil {
		return TargetAppServiceEnvVar{}, err
	}
	if item.Name != input.Name {
		return TargetAppServiceEnvVar{}, errors.Errorf("created target environment variable name %q does not exactly match %q", item.Name, input.Name)
	}
	return item, nil
}

func (c *TargetClient) UpdateAppServiceEnvVar(ctx context.Context, envVarID int, input TargetUpdateAppServiceEnvVarInput) (TargetAppServiceEnvVar, error) {
	if err := targetRequirePositiveID("app service environment variable", envVarID); err != nil {
		return TargetAppServiceEnvVar{}, err
	}
	var item TargetAppServiceEnvVar
	if err := c.client.Put(ctx, "/app-service-env-vars/"+strconv.Itoa(envVarID), nil, input, &item); err != nil {
		return TargetAppServiceEnvVar{}, errors.Wrap(err, "update target Wodby 2 app service environment variable")
	}
	if item.ID != envVarID {
		return TargetAppServiceEnvVar{}, targetUnexpectedID("app service environment variable", item.ID, envVarID)
	}
	if err := validateTargetEnvVar(item, 0); err != nil {
		return TargetAppServiceEnvVar{}, err
	}
	return item, nil
}

func (c *TargetClient) ListAppServiceSettings(ctx context.Context, appServiceID int) ([]TargetAppServiceSetting, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return nil, err
	}
	items := []TargetAppServiceSetting{}
	if err := c.client.Get(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/settings", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app service settings")
	}
	for _, item := range items {
		if err := validateTargetAppServiceSetting(item, appServiceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) SetAppServiceSetting(ctx context.Context, appServiceID int, name, value string) (TargetAppServiceSetting, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return TargetAppServiceSetting{}, err
	}
	name, err := targetSafePathName("app service setting", name)
	if err != nil {
		return TargetAppServiceSetting{}, err
	}
	body := struct {
		Value string `json:"value"`
	}{Value: value}
	var item TargetAppServiceSetting
	if err := c.client.Put(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/settings/"+name, nil, body, &item); err != nil {
		return TargetAppServiceSetting{}, errors.Wrap(err, "set target Wodby 2 app service setting")
	}
	if err := validateTargetAppServiceSetting(item, appServiceID); err != nil {
		return TargetAppServiceSetting{}, err
	}
	if item.Name != name {
		return TargetAppServiceSetting{}, errors.Errorf("target app service setting response name %q does not match %q", item.Name, name)
	}
	return item, nil
}

func (c *TargetClient) ListAppServiceCronSchedules(ctx context.Context, appServiceID int) ([]TargetAppServiceCronSchedule, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return nil, err
	}
	items := []TargetAppServiceCronSchedule{}
	if err := c.client.Get(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/cron-schedules", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app service cron schedules")
	}
	for _, item := range items {
		if err := validateTargetCronSchedule(item, appServiceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateAppServiceCronSchedule(ctx context.Context, appServiceID int, input TargetCreateAppServiceCronScheduleInput) (TargetAppServiceCronSchedule, error) {
	if err := targetRequirePositiveID("app service", appServiceID); err != nil {
		return TargetAppServiceCronSchedule{}, err
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Crontab) == "" || strings.TrimSpace(input.Command) == "" {
		return TargetAppServiceCronSchedule{}, errors.New("target cron schedule title, crontab, and command are required")
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		return TargetAppServiceCronSchedule{}, errors.New("target cron schedule name must not be blank when provided")
	}
	var item TargetAppServiceCronSchedule
	if err := c.client.Post(ctx, "/app-services/"+strconv.Itoa(appServiceID)+"/cron-schedules", nil, input, &item); err != nil {
		return TargetAppServiceCronSchedule{}, errors.Wrap(err, "create target Wodby 2 app service cron schedule")
	}
	if err := validateTargetCronSchedule(item, appServiceID); err != nil {
		return TargetAppServiceCronSchedule{}, err
	}
	if input.Name != nil && item.Name != *input.Name {
		return TargetAppServiceCronSchedule{}, errors.Errorf("created target cron schedule name %q does not exactly match %q", item.Name, *input.Name)
	}
	return item, nil
}

func (c *TargetClient) UpdateAppServiceCronSchedule(ctx context.Context, scheduleID int, input TargetUpdateAppServiceCronScheduleInput) (TargetAppServiceCronSchedule, error) {
	if err := targetRequirePositiveID("app service cron schedule", scheduleID); err != nil {
		return TargetAppServiceCronSchedule{}, err
	}
	if input.Disabled == nil && input.Title == nil && input.Crontab == nil && input.Command == nil && input.Workload == nil {
		return TargetAppServiceCronSchedule{}, errors.New("target cron schedule update must include at least one field")
	}
	var item TargetAppServiceCronSchedule
	if err := c.client.Put(ctx, "/app-service-cron-schedules/"+strconv.Itoa(scheduleID), nil, input, &item); err != nil {
		return TargetAppServiceCronSchedule{}, errors.Wrap(err, "update target Wodby 2 app service cron schedule")
	}
	if item.ID != scheduleID {
		return TargetAppServiceCronSchedule{}, targetUnexpectedID("app service cron schedule", item.ID, scheduleID)
	}
	if err := validateTargetCronSchedule(item, 0); err != nil {
		return TargetAppServiceCronSchedule{}, err
	}
	return item, nil
}

func (c *TargetClient) ListAppPorts(ctx context.Context, appInstanceID int) ([]TargetAppPort, error) {
	query, err := targetRequiredQueryID("appInstanceId", "app instance", appInstanceID)
	if err != nil {
		return nil, err
	}
	items := []TargetAppPort{}
	if err := c.client.Get(ctx, "/app-ports", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app ports")
	}
	for _, item := range items {
		if err := validateTargetAppPort(item, appInstanceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) ListAppRoutes(ctx context.Context, appInstanceID int) ([]TargetAppRoute, error) {
	query, err := targetRequiredQueryID("appInstanceId", "app instance", appInstanceID)
	if err != nil {
		return nil, err
	}
	items := []TargetAppRoute{}
	if err := c.client.Get(ctx, "/app-routes", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app routes")
	}
	for _, item := range items {
		if err := validateTargetAppRoute(item, appInstanceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateAppRoute(ctx context.Context, input TargetCreateAppRouteInput) (TargetAppRoute, error) {
	if err := validateTargetCreateRouteInput(input); err != nil {
		return TargetAppRoute{}, err
	}
	var item TargetAppRoute
	if err := c.client.Post(ctx, "/app-routes", nil, input, &item); err != nil {
		return TargetAppRoute{}, errors.Wrap(err, "create target Wodby 2 app route")
	}
	if err := validateTargetAppRoute(item, 0); err != nil {
		return TargetAppRoute{}, err
	}
	if item.AppServiceID != input.AppServiceID || item.PortID != input.Port || item.Host != input.Host {
		return TargetAppRoute{}, errors.Errorf("created target app route does not match requested service/port/host")
	}
	expectedDisabled := input.Disabled != nil && *input.Disabled
	if item.Disabled != expectedDisabled {
		return TargetAppRoute{}, errors.Errorf("created target app route does not match requested disabled state")
	}
	return item, nil
}

func (c *TargetClient) UpdateAppRoute(ctx context.Context, routeID int, input TargetUpdateAppRouteInput) (TargetAppRoute, error) {
	if err := targetRequirePositiveID("app route", routeID); err != nil {
		return TargetAppRoute{}, err
	}
	if err := validateTargetUpdateRouteInput(input); err != nil {
		return TargetAppRoute{}, err
	}
	var item TargetAppRoute
	if err := c.client.Put(ctx, "/app-routes/"+strconv.Itoa(routeID), nil, input, &item); err != nil {
		return TargetAppRoute{}, errors.Wrap(err, "update target Wodby 2 app route")
	}
	if item.ID != routeID {
		return TargetAppRoute{}, targetUnexpectedID("app route", item.ID, routeID)
	}
	if err := validateTargetAppRoute(item, 0); err != nil {
		return TargetAppRoute{}, err
	}
	return item, nil
}

func (c *TargetClient) ListAppRouteSettings(ctx context.Context, routeID int) ([]TargetAppRouteSetting, error) {
	if err := targetRequirePositiveID("app route", routeID); err != nil {
		return nil, err
	}
	items := []TargetAppRouteSetting{}
	if err := c.client.Get(ctx, "/app-routes/"+strconv.Itoa(routeID)+"/settings", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app route settings")
	}
	for _, item := range items {
		if err := validateTargetRouteSetting(item, routeID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) SetAppRouteSetting(ctx context.Context, routeID int, name, value string) (TargetAppRouteSetting, error) {
	if err := targetRequirePositiveID("app route", routeID); err != nil {
		return TargetAppRouteSetting{}, err
	}
	if !targetValidRouteSetting(name) {
		return TargetAppRouteSetting{}, errors.Errorf("unsupported target app route setting name %q", name)
	}
	body := struct {
		Value string `json:"value"`
	}{Value: value}
	var item TargetAppRouteSetting
	if err := c.client.Put(ctx, "/app-routes/"+strconv.Itoa(routeID)+"/settings/"+name, nil, body, &item); err != nil {
		return TargetAppRouteSetting{}, errors.Wrap(err, "set target Wodby 2 app route setting")
	}
	if err := validateTargetRouteSetting(item, routeID); err != nil {
		return TargetAppRouteSetting{}, err
	}
	if item.Name != name {
		return TargetAppRouteSetting{}, errors.Errorf("target app route setting response name %q does not match %q", item.Name, name)
	}
	return item, nil
}

func (c *TargetClient) ListAppAuths(ctx context.Context, appInstanceID int) ([]TargetAppAuth, error) {
	query, err := targetRequiredQueryID("appInstanceId", "app instance", appInstanceID)
	if err != nil {
		return nil, err
	}
	items := []TargetAppAuth{}
	if err := c.client.Get(ctx, "/app-auths", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 app authentication entries")
	}
	for _, item := range items {
		if err := validateTargetAppAuth(item, appInstanceID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) CreateAppAuth(ctx context.Context, input TargetCreateAppAuthInput) (TargetAppAuth, error) {
	if err := validateTargetCreateAuthInput(input); err != nil {
		return TargetAppAuth{}, err
	}
	var item TargetAppAuth
	if err := c.client.Post(ctx, "/app-auths", nil, input, &item); err != nil {
		return TargetAppAuth{}, errors.Wrap(err, "create target Wodby 2 app authentication entry")
	}
	if err := validateTargetAppAuth(item, input.AppInstanceID); err != nil {
		return TargetAppAuth{}, err
	}
	if !targetEqualOptionalID(item.AppServiceID, input.AppServiceID) ||
		!targetEqualOptionalID(item.AppRouteID, input.AppRouteID) {
		return TargetAppAuth{}, errors.New("created target app authentication scope does not match request")
	}
	if item.Login != input.Login || item.Realm != input.Realm {
		return TargetAppAuth{}, errors.New("created target app authentication entry does not match requested login/realm")
	}
	return item, nil
}

func (c *TargetClient) UpdateAppAuth(ctx context.Context, authID int, input TargetUpdateAppAuthInput) (TargetAppAuth, error) {
	if err := targetRequirePositiveID("app authentication entry", authID); err != nil {
		return TargetAppAuth{}, err
	}
	if strings.TrimSpace(input.Login) == "" || strings.TrimSpace(input.Realm) == "" {
		return TargetAppAuth{}, errors.New("target app authentication login and realm are required")
	}
	if err := validateTargetAuthScope(input.AppServiceID, input.AppRouteID); err != nil {
		return TargetAppAuth{}, err
	}
	var item TargetAppAuth
	if err := c.client.Put(ctx, "/app-auths/"+strconv.Itoa(authID), nil, input, &item); err != nil {
		return TargetAppAuth{}, errors.Wrap(err, "update target Wodby 2 app authentication entry")
	}
	if item.ID != authID {
		return TargetAppAuth{}, targetUnexpectedID("app authentication entry", item.ID, authID)
	}
	if err := validateTargetAppAuth(item, 0); err != nil {
		return TargetAppAuth{}, err
	}
	if input.AppServiceID != nil &&
		(!targetEqualOptionalID(item.AppServiceID, input.AppServiceID) ||
			!targetEqualOptionalID(item.AppRouteID, input.AppRouteID)) {
		return TargetAppAuth{}, errors.New("updated target app authentication scope does not match request")
	}
	return item, nil
}

func (c *TargetClient) ListImports(ctx context.Context, filters TargetImportFilters) ([]TargetImport, error) {
	query := url.Values{}
	count := 0
	for _, candidate := range []struct {
		queryName string
		label     string
		id        *int
	}{
		{"appInstanceId", "app instance", filters.AppInstanceID},
		{"appServiceId", "app service", filters.AppServiceID},
		{"databaseId", "database", filters.DatabaseID},
		{"databaseDbId", "database schema", filters.DatabaseDBID},
	} {
		if candidate.id == nil {
			continue
		}
		if err := targetRequirePositiveID(candidate.label, *candidate.id); err != nil {
			return nil, err
		}
		query.Set(candidate.queryName, strconv.Itoa(*candidate.id))
		count++
	}
	if count == 0 {
		return nil, errors.New("at least one target import filter is required")
	}
	items := []TargetImport{}
	if err := c.client.Get(ctx, "/imports", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 imports")
	}
	for _, item := range items {
		if err := validateTargetImport(item, filters); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c *TargetClient) GetImport(ctx context.Context, importID int) (TargetImport, error) {
	if err := targetRequirePositiveID("import", importID); err != nil {
		return TargetImport{}, err
	}
	var item TargetImport
	if err := c.client.Get(ctx, "/imports/"+strconv.Itoa(importID), nil, &item); err != nil {
		return TargetImport{}, errors.Wrap(err, "get target Wodby 2 import")
	}
	if item.ID != importID {
		return TargetImport{}, targetUnexpectedID("import", item.ID, importID)
	}
	if err := validateTargetImport(item, TargetImportFilters{}); err != nil {
		return TargetImport{}, err
	}
	return item, nil
}

func (c *TargetClient) StartURLImport(ctx context.Context, input TargetStartURLImportInput) (TargetOperationResult, error) {
	if err := targetRequirePositiveID("app service", input.AppServiceID); err != nil {
		return TargetOperationResult{}, err
	}
	if strings.TrimSpace(input.ImportName) == "" {
		return TargetOperationResult{}, errors.New("target import capability name is required")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return TargetOperationResult{}, errors.New("target URL import requires an absolute HTTPS URL without embedded credentials")
	}
	importName := input.ImportName
	importURL := input.URL
	body := struct {
		AppServiceID int `json:"appServiceId"`
		Import       struct {
			ImportName *string `json:"importName,omitempty"`
			Source     string  `json:"source"`
			URL        *string `json:"url,omitempty"`
		} `json:"import"`
	}{
		AppServiceID: input.AppServiceID,
	}
	body.Import.ImportName = &importName
	body.Import.Source = "URL"
	body.Import.URL = &importURL

	var result TargetOperationResult
	if err := c.client.Post(ctx, "/imports", nil, body, &result); err != nil {
		return TargetOperationResult{}, errors.Wrap(err, "start target Wodby 2 URL import")
	}
	if !result.Success {
		return TargetOperationResult{}, errors.New("target Wodby 2 URL import request was not accepted")
	}
	if err := targetValidateOptionalPositiveID("task", result.TaskID); err != nil {
		return TargetOperationResult{}, err
	}
	return result, nil
}

func (c *TargetClient) GetTask(ctx context.Context, taskID int) (TargetTask, error) {
	if err := targetRequirePositiveID("task", taskID); err != nil {
		return TargetTask{}, err
	}
	var item TargetTask
	if err := c.client.Get(ctx, "/tasks/"+strconv.Itoa(taskID), nil, &item); err != nil {
		return TargetTask{}, errors.Wrap(err, "get target Wodby 2 task")
	}
	if item.ID != taskID {
		return TargetTask{}, targetUnexpectedID("task", item.ID, taskID)
	}
	if err := targetRequirePositiveID("task user", item.UserID); err != nil {
		return TargetTask{}, err
	}
	for label, id := range map[string]*int{
		"organization": item.OrgID,
		"app":          item.AppID,
		"app instance": item.AppInstanceID,
		"cluster":      item.ClusterID,
	} {
		if err := targetValidateOptionalPositiveID(label, id); err != nil {
			return TargetTask{}, err
		}
	}
	return item, nil
}

func (c *TargetClient) CreateAppBuild(ctx context.Context, appServiceIDs []int) (TargetAppBuildsCreateResponse, error) {
	ids, err := normalizePositiveIDs(appServiceIDs, "target app service")
	if err != nil {
		return TargetAppBuildsCreateResponse{}, err
	}
	if len(ids) == 0 {
		return TargetAppBuildsCreateResponse{}, errors.New("at least one target app service ID is required to create a build")
	}
	body := struct {
		AppServiceIDs []int `json:"appServiceIds"`
	}{AppServiceIDs: ids}
	var response TargetAppBuildsCreateResponse
	if err := c.client.Post(ctx, "/app-builds", nil, body, &response); err != nil {
		return TargetAppBuildsCreateResponse{}, errors.Wrap(err, "create target Wodby 2 app build")
	}
	allowed := targetIDSet(ids)
	if err := validateTargetBuilds(response.Items, 0, allowed); err != nil {
		return TargetAppBuildsCreateResponse{}, err
	}
	if len(response.Items) == 0 {
		return TargetAppBuildsCreateResponse{}, errors.New("target Wodby 2 build response contains no builds")
	}
	if err := targetValidateOptionalPositiveID("task", response.TaskID); err != nil {
		return TargetAppBuildsCreateResponse{}, err
	}
	return response, nil
}

func (c *TargetClient) ListAppBuilds(ctx context.Context, appInstanceID int, page TargetPageOptions) (TargetAppBuildsResponse, error) {
	query, err := targetPagedInstanceQuery(appInstanceID, page)
	if err != nil {
		return TargetAppBuildsResponse{}, err
	}
	var response TargetAppBuildsResponse
	if err := c.client.Get(ctx, "/app-builds", query, &response); err != nil {
		return TargetAppBuildsResponse{}, errors.Wrap(err, "list target Wodby 2 app builds")
	}
	if response.TotalCount < 0 {
		return TargetAppBuildsResponse{}, errors.New("target Wodby 2 build response has a negative total count")
	}
	if err := targetValidateOptionalPositiveID("next page", response.NextPage); err != nil {
		return TargetAppBuildsResponse{}, err
	}
	if err := validateTargetBuilds(response.Items, appInstanceID, nil); err != nil {
		return TargetAppBuildsResponse{}, err
	}
	return response, nil
}

func (c *TargetClient) GetAppBuild(ctx context.Context, buildID int) (TargetAppBuild, error) {
	if err := targetRequirePositiveID("app build", buildID); err != nil {
		return TargetAppBuild{}, err
	}
	var item TargetAppBuild
	if err := c.client.Get(ctx, "/app-builds/"+strconv.Itoa(buildID), nil, &item); err != nil {
		return TargetAppBuild{}, errors.Wrap(err, "get target Wodby 2 app build")
	}
	if item.ID != buildID {
		return TargetAppBuild{}, targetUnexpectedID("app build", item.ID, buildID)
	}
	if err := validateTargetBuild(item, 0, nil); err != nil {
		return TargetAppBuild{}, err
	}
	return item, nil
}

func (c *TargetClient) CreateAppDeployment(ctx context.Context, input TargetCreateAppDeploymentInput) (TargetAppDeployment, error) {
	if len(input.Services) == 0 {
		return TargetAppDeployment{}, errors.New("at least one target app service is required to create a deployment")
	}
	serviceIDs := map[int]bool{}
	for _, service := range input.Services {
		if err := targetRequirePositiveID("app service", service.AppServiceID); err != nil {
			return TargetAppDeployment{}, err
		}
		if serviceIDs[service.AppServiceID] {
			return TargetAppDeployment{}, errors.Errorf("target app service ID %d appears more than once in deployment input", service.AppServiceID)
		}
		serviceIDs[service.AppServiceID] = true
		if err := targetValidateOptionalPositiveID("app service build", service.AppServiceBuildID); err != nil {
			return TargetAppDeployment{}, err
		}
	}
	var item TargetAppDeployment
	if err := c.client.Post(ctx, "/app-deployments", nil, input, &item); err != nil {
		return TargetAppDeployment{}, errors.Wrap(err, "create target Wodby 2 app deployment")
	}
	if err := validateTargetDeployment(item, 0, serviceIDs); err != nil {
		return TargetAppDeployment{}, err
	}
	return item, nil
}

func (c *TargetClient) ListAppDeployments(ctx context.Context, appInstanceID int, page TargetPageOptions) (TargetAppDeploymentsResponse, error) {
	query, err := targetPagedInstanceQuery(appInstanceID, page)
	if err != nil {
		return TargetAppDeploymentsResponse{}, err
	}
	var response TargetAppDeploymentsResponse
	if err := c.client.Get(ctx, "/app-deployments", query, &response); err != nil {
		return TargetAppDeploymentsResponse{}, errors.Wrap(err, "list target Wodby 2 app deployments")
	}
	if response.TotalCount < 0 {
		return TargetAppDeploymentsResponse{}, errors.New("target Wodby 2 deployment response has a negative total count")
	}
	if err := targetValidateOptionalPositiveID("next page", response.NextPage); err != nil {
		return TargetAppDeploymentsResponse{}, err
	}
	for _, item := range response.Items {
		if err := validateTargetDeployment(item, appInstanceID, nil); err != nil {
			return TargetAppDeploymentsResponse{}, err
		}
	}
	return response, nil
}

func (c *TargetClient) GetAppDeployment(ctx context.Context, deploymentID int) (TargetAppDeployment, error) {
	if err := targetRequirePositiveID("app deployment", deploymentID); err != nil {
		return TargetAppDeployment{}, err
	}
	var item TargetAppDeployment
	if err := c.client.Get(ctx, "/app-deployments/"+strconv.Itoa(deploymentID), nil, &item); err != nil {
		return TargetAppDeployment{}, errors.Wrap(err, "get target Wodby 2 app deployment")
	}
	if item.ID != deploymentID {
		return TargetAppDeployment{}, targetUnexpectedID("app deployment", item.ID, deploymentID)
	}
	if err := validateTargetDeployment(item, 0, nil); err != nil {
		return TargetAppDeployment{}, err
	}
	return item, nil
}

func validateTargetStack(item TargetStack) error {
	if err := targetRequirePositiveID("stack", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("stack revision", item.RevID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("stack organization", item.OrgID); err != nil {
		return err
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target Wodby 2 stack ID %d returned an empty name", item.ID)
	}
	if !strings.EqualFold(strings.TrimSpace(item.Status), "OK") {
		return errors.Errorf("target Wodby 2 stack %q has status %q", item.Name, item.Status)
	}
	return nil
}

func validateTargetCatalogService(item TargetCatalogService, targetOrgID int, expectedName string) error {
	if err := targetRequirePositiveID("service", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("service revision", item.RevID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("service organization", item.OrgID); err != nil {
		return err
	}
	if item.Name != expectedName || strings.TrimSpace(item.Title) == "" {
		return errors.Errorf("target service response does not exactly match %q", expectedName)
	}
	if !item.Public && item.OrgID != targetOrgID {
		return errors.Errorf("target service %q belongs to organization ID %d", item.Name, item.OrgID)
	}
	if !strings.EqualFold(strings.TrimSpace(item.Status), "OK") {
		return errors.Errorf("target service %q status is %q", item.Name, item.Status)
	}
	return nil
}

func validateTargetStackService(item TargetStackService) error {
	if err := targetRequirePositiveID("stack service", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("service revision", item.ServiceRevID); err != nil {
		return err
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target Wodby 2 stack service ID %d returned an empty name", item.ID)
	}
	if err := targetValidateOptionalPositiveID("build source integration", item.BuildSourceIntegrationID); err != nil {
		return err
	}
	for _, option := range item.Options {
		if err := targetRequirePositiveID("stack service option", option.ID); err != nil {
			return err
		}
		if option.StackServiceID != item.ID || strings.TrimSpace(option.Version) == "" {
			return errors.New("target stack service returned an invalid option")
		}
	}
	for _, setting := range item.Settings {
		if err := targetRequirePositiveID("stack service setting", setting.ID); err != nil {
			return err
		}
		if setting.StackServiceID != item.ID || strings.TrimSpace(setting.Name) == "" {
			return errors.New("target stack service returned an invalid setting")
		}
	}
	return nil
}

func validateTargetStackServiceEnvVar(item TargetStackServiceEnvVar, stackServiceID int) error {
	if err := targetRequirePositiveID("stack service environment variable", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("stack service", item.StackServiceID); err != nil {
		return err
	}
	if stackServiceID > 0 && item.StackServiceID != stackServiceID {
		return errors.Errorf("target stack environment variable belongs to stack service ID %d, expected %d", item.StackServiceID, stackServiceID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("target stack environment variable returned an empty name")
	}
	return targetValidateOptionalPositiveID("stack environment variable secret", item.ValueSecretID)
}

func validateTargetStackServiceCronSchedule(item TargetStackServiceCronSchedule, stackServiceID int) error {
	if err := targetRequirePositiveID("stack service cron schedule", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("stack service", item.StackServiceID); err != nil {
		return err
	}
	if stackServiceID > 0 && item.StackServiceID != stackServiceID {
		return errors.Errorf("target stack cron schedule belongs to stack service ID %d, expected %d", item.StackServiceID, stackServiceID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("target stack cron schedule returned an empty name")
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.EqualFold(strings.TrimSpace(*left), strings.TrimSpace(*right))
}

func validateTargetCreateAppInput(input TargetCreateAppInput) error {
	for label, id := range map[string]int{
		"organization":   input.OrgID,
		"stack revision": input.StackRevID,
		"cluster":        input.ClusterID,
		"environment":    input.EnvID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.InstanceName) == "" {
		return errors.New("target app and initial instance names are required")
	}
	if err := targetValidateOptionalPositiveID("project", input.ProjectID); err != nil {
		return err
	}
	if err := targetValidateOptionalNonNegativeID("CI integration", input.CIIntegrationID); err != nil {
		return err
	}
	return targetValidateOptionalPositiveID("registry integration", input.RegistryIntegrationID)
}

func validateTargetCreateAppInstanceInput(input TargetCreateAppInstanceInput) error {
	for label, id := range map[string]int{
		"app":            input.AppID,
		"stack revision": input.StackRevID,
		"cluster":        input.ClusterID,
		"environment":    input.EnvID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if strings.TrimSpace(input.InstanceName) == "" {
		return errors.New("target app instance name is required")
	}
	if err := targetValidateOptionalNonNegativeID("CI integration", input.CIIntegrationID); err != nil {
		return err
	}
	return targetValidateOptionalPositiveID("registry integration", input.RegistryIntegrationID)
}

func validateTargetApp(item TargetApp, orgID int) error {
	if err := targetRequirePositiveID("app", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app organization", item.OrgID); err != nil {
		return err
	}
	if orgID > 0 && item.OrgID != orgID {
		return errors.Errorf("target Wodby 2 app ID %d belongs to organization ID %d, expected %d", item.ID, item.OrgID, orgID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target Wodby 2 app ID %d returned an empty name", item.ID)
	}
	return nil
}

func validateTargetAppInstance(item TargetAppInstance, appID int) error {
	for label, id := range map[string]int{
		"app instance":   item.ID,
		"app":            item.AppID,
		"cluster":        item.ClusterID,
		"environment":    item.EnvID,
		"stack":          item.StackID,
		"stack revision": item.StackRevID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if appID > 0 && item.AppID != appID {
		return errors.Errorf("target Wodby 2 app instance ID %d belongs to app ID %d, expected %d", item.ID, item.AppID, appID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target Wodby 2 app instance ID %d returned an empty name", item.ID)
	}
	return nil
}

func validateTargetAppService(item TargetAppService, appInstanceID int) error {
	for label, id := range map[string]int{
		"app service":      item.ID,
		"app instance":     item.AppInstanceID,
		"service revision": item.ServiceRevID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target Wodby 2 app service ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	if err := targetValidateOptionalPositiveID("parent app service", item.ParentAppServiceID); err != nil {
		return err
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target Wodby 2 app service ID %d returned an empty name", item.ID)
	}
	return nil
}

func validateTargetAppServiceUpdateInput(input TargetAppServiceUpdateInput) error {
	if input.Replicas == nil && input.Version == nil && input.Disabled == nil && input.Main == nil && input.BuildSource == nil {
		return errors.New("target app service update must include at least one field")
	}
	if input.Replicas != nil && *input.Replicas < 0 {
		return errors.New("target app service replicas must not be negative")
	}
	if input.Version != nil && strings.TrimSpace(*input.Version) == "" {
		return errors.New("target app service version must not be empty")
	}
	if input.BuildSource == nil {
		return nil
	}
	switch input.BuildSource.BuildSourceType {
	case TargetBuildSourcePublic, TargetBuildSourceClone, TargetBuildSourceConnect:
	default:
		return errors.Errorf("unsupported target build source type %q", input.BuildSource.BuildSourceType)
	}
	if err := targetValidateOptionalPositiveID("build source integration", input.BuildSource.IntegrationID); err != nil {
		return err
	}
	if input.BuildSource.GitRefType != nil {
		switch *input.BuildSource.GitRefType {
		case TargetGitRefBranch, TargetGitRefTag, TargetGitRefCommit:
		default:
			return errors.Errorf("unsupported target Git ref type %q", *input.BuildSource.GitRefType)
		}
	}
	return nil
}

func validateTargetEnvVar(item TargetAppServiceEnvVar, appServiceID int) error {
	if item.ID == 0 {
		return errors.New("target app service environment variable ID must be non-zero")
	}
	if item.ID < 0 && item.Source == nil {
		return errors.New("synthetic target environment variable is missing its inherited source")
	}
	if err := targetRequirePositiveID("app service", item.AppServiceID); err != nil {
		return err
	}
	if appServiceID > 0 && item.AppServiceID != appServiceID {
		return errors.Errorf("target environment variable ID %d belongs to app service ID %d, expected %d", item.ID, item.AppServiceID, appServiceID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target environment variable ID %d returned an empty name", item.ID)
	}
	if item.ValueSecretID != nil {
		if *item.ValueSecretID == 0 {
			return errors.New("target environment variable secret ID must be non-zero")
		}
		if *item.ValueSecretID < 0 && item.Source == nil {
			return errors.New("mutable target environment variable returned a synthetic secret ID")
		}
	}
	return nil
}

func validateTargetCronSchedule(item TargetAppServiceCronSchedule, appServiceID int) error {
	if err := targetRequirePositiveID("app service cron schedule", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app service", item.AppServiceID); err != nil {
		return err
	}
	if appServiceID > 0 && item.AppServiceID != appServiceID {
		return errors.Errorf("target cron schedule ID %d belongs to app service ID %d, expected %d", item.ID, item.AppServiceID, appServiceID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target cron schedule ID %d returned an empty name", item.ID)
	}
	return nil
}

func validateTargetAppServiceSetting(item TargetAppServiceSetting, appServiceID int) error {
	if err := targetRequirePositiveID("app service setting", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app service", item.AppServiceID); err != nil {
		return err
	}
	if item.AppServiceID != appServiceID {
		return errors.Errorf("target app service setting ID %d belongs to app service ID %d, expected %d", item.ID, item.AppServiceID, appServiceID)
	}
	if strings.TrimSpace(item.Name) == "" {
		return errors.Errorf("target app service setting ID %d returned an empty name", item.ID)
	}
	return targetValidateOptionalPositiveID("source setting", item.FromSettingID)
}

func validateTargetAppPort(item TargetAppPort, appInstanceID int) error {
	for label, id := range map[string]int{
		"app port":     item.ID,
		"app endpoint": item.AppEndpointID,
		"app instance": item.AppInstanceID,
		"app service":  item.AppServiceID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if item.Number <= 0 || item.Number > 65535 {
		return errors.Errorf("target app port ID %d returned invalid port number %d", item.ID, item.Number)
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target app port ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	return nil
}

func validateTargetCreateRouteInput(input TargetCreateAppRouteInput) error {
	if err := targetRequirePositiveID("app service", input.AppServiceID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app port", input.Port); err != nil {
		return err
	}
	if strings.TrimSpace(input.Host) == "" {
		return errors.New("target app route host is required")
	}
	if input.Disabled != nil && *input.Disabled && (input.Main || input.Primary) {
		return errors.New("a disabled target app route cannot be main or primary")
	}
	return validateTargetRouteEnums(input.PathType, input.Action)
}

func validateTargetUpdateRouteInput(input TargetUpdateAppRouteInput) error {
	if input.Disabled == nil && input.Main == nil && input.Primary == nil && input.Path == nil &&
		input.PathType == nil && input.Action == nil && input.RedirectScheme == nil &&
		input.RedirectHost == nil && input.RedirectPath == nil && input.RedirectStatusCode == nil {
		return errors.New("target app route update must include at least one field")
	}
	return validateTargetRouteEnums(input.PathType, input.Action)
}

func validateTargetRouteEnums(pathType, action *string) error {
	if pathType != nil && *pathType != TargetRoutePathPrefix && *pathType != TargetRoutePathExact {
		return errors.Errorf("unsupported target app route path type %q", *pathType)
	}
	if action != nil && *action != TargetRouteActionBackend && *action != TargetRouteActionRedirect {
		return errors.Errorf("unsupported target app route action %q", *action)
	}
	return nil
}

func validateTargetAppRoute(item TargetAppRoute, appInstanceID int) error {
	for label, id := range map[string]int{
		"app route":    item.ID,
		"app instance": item.AppInstanceID,
		"app service":  item.AppServiceID,
		"app port":     item.PortID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target app route ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.Errorf("target app route ID %d returned an empty host", item.ID)
	}
	if item.Cert != nil {
		if err := validateTargetCert(*item.Cert, item.AppInstanceID, item.AppServiceID); err != nil {
			return errors.Wrapf(err, "validate certificate for target app route ID %d", item.ID)
		}
	}
	return nil
}

func validateTargetCert(item TargetCert, appInstanceID, appServiceID int) error {
	if err := targetRequirePositiveID("certificate", item.ID); err != nil {
		return err
	}
	if strings.TrimSpace(item.Issuer) == "" || strings.TrimSpace(item.Status) == "" {
		return errors.Errorf("target certificate ID %d returned an empty issuer or status", item.ID)
	}
	if err := targetValidateOptionalPositiveID("certificate app instance", item.AppInstanceID); err != nil {
		return err
	}
	if err := targetValidateOptionalPositiveID("certificate app service", item.AppServiceID); err != nil {
		return err
	}
	if item.AppInstanceID != nil && *item.AppInstanceID != appInstanceID {
		return errors.Errorf(
			"target certificate ID %d belongs to app instance ID %d, expected %d",
			item.ID,
			*item.AppInstanceID,
			appInstanceID,
		)
	}
	if item.AppServiceID != nil && *item.AppServiceID != appServiceID {
		return errors.Errorf(
			"target certificate ID %d belongs to app service ID %d, expected %d",
			item.ID,
			*item.AppServiceID,
			appServiceID,
		)
	}
	return nil
}

func validateTargetRouteSetting(item TargetAppRouteSetting, routeID int) error {
	for label, id := range map[string]int{
		"app route setting": item.ID,
		"app instance":      item.AppInstanceID,
		"app route":         item.RouteID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if item.RouteID != routeID {
		return errors.Errorf("target app route setting ID %d belongs to route ID %d, expected %d", item.ID, item.RouteID, routeID)
	}
	if !targetValidRouteSetting(item.Name) {
		return errors.Errorf("target app route setting ID %d returned unsupported name %q", item.ID, item.Name)
	}
	return nil
}

func targetValidRouteSetting(name string) bool {
	switch name {
	case TargetRouteSettingHTTPSRedirect,
		TargetRouteSettingNoIndex,
		TargetRouteSettingRequestBodySize,
		TargetRouteSettingSessionAffinity,
		TargetRouteSettingPathRewrite,
		TargetRouteSettingHSTS:
		return true
	default:
		return false
	}
}

func validateTargetCreateAuthInput(input TargetCreateAppAuthInput) error {
	if err := targetRequirePositiveID("app instance", input.AppInstanceID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Login) == "" || strings.TrimSpace(input.Realm) == "" {
		return errors.New("target app authentication login and realm are required")
	}
	return validateTargetAuthScope(input.AppServiceID, input.AppRouteID)
}

func validateTargetAuthScope(appServiceID, appRouteID *int) error {
	if err := targetValidateOptionalPositiveID("app service", appServiceID); err != nil {
		return err
	}
	if err := targetValidateOptionalPositiveID("app route", appRouteID); err != nil {
		return err
	}
	if appRouteID != nil && appServiceID == nil {
		return errors.New("target route-scoped authentication requires an app service ID")
	}
	return nil
}

func validateTargetAppAuth(item TargetAppAuth, appInstanceID int) error {
	if err := targetRequirePositiveID("app authentication entry", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app instance", item.AppInstanceID); err != nil {
		return err
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target app authentication entry ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	if err := validateTargetAuthScope(item.AppServiceID, item.AppRouteID); err != nil {
		return err
	}
	if strings.TrimSpace(item.Login) == "" || strings.TrimSpace(item.Realm) == "" {
		return errors.Errorf("target app authentication entry ID %d returned an empty login or realm", item.ID)
	}
	return nil
}

func validateTargetImport(item TargetImport, filters TargetImportFilters) error {
	if err := targetRequirePositiveID("import", item.ID); err != nil {
		return err
	}
	for label, id := range map[string]*int{
		"app instance":           item.AppInstanceID,
		"app service":            item.AppServiceID,
		"database":               item.DatabaseID,
		"database schema":        item.DatabaseDBID,
		"app service deployment": item.AppServiceDeploymentID,
		"task":                   item.TaskID,
		"backup":                 item.BackupID,
	} {
		if err := targetValidateOptionalPositiveID(label, id); err != nil {
			return err
		}
	}
	for _, relation := range []struct {
		label    string
		actual   *int
		expected *int
	}{
		{"app instance", item.AppInstanceID, filters.AppInstanceID},
		{"app service", item.AppServiceID, filters.AppServiceID},
		{"database", item.DatabaseID, filters.DatabaseID},
		{"database schema", item.DatabaseDBID, filters.DatabaseDBID},
	} {
		if relation.expected != nil && (relation.actual == nil || *relation.actual != *relation.expected) {
			return errors.Errorf("target import ID %d does not belong to requested %s ID %d", item.ID, relation.label, *relation.expected)
		}
	}
	return nil
}

func validateTargetBuilds(items []TargetAppBuild, appInstanceID int, allowedServiceIDs map[int]bool) error {
	seen := map[int]bool{}
	for _, item := range items {
		if seen[item.ID] {
			return errors.Errorf("target Wodby 2 build response contains duplicate build ID %d", item.ID)
		}
		seen[item.ID] = true
		if err := validateTargetBuild(item, appInstanceID, allowedServiceIDs); err != nil {
			return err
		}
	}
	return nil
}

func validateTargetBuild(item TargetAppBuild, appInstanceID int, allowedServiceIDs map[int]bool) error {
	for label, id := range map[string]int{
		"app build":    item.ID,
		"app instance": item.AppInstanceID,
		"app service":  item.AppServiceID,
	} {
		if err := targetRequirePositiveID(label, id); err != nil {
			return err
		}
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target app build ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	if allowedServiceIDs != nil && !allowedServiceIDs[item.AppServiceID] {
		return errors.Errorf("target app build ID %d belongs to unrequested app service ID %d", item.ID, item.AppServiceID)
	}
	if err := targetValidateOptionalPositiveID("task", item.TaskID); err != nil {
		return err
	}
	for _, serviceBuild := range item.AppServiceBuilds {
		if err := targetRequirePositiveID("app service build", serviceBuild.ID); err != nil {
			return err
		}
		if err := targetRequirePositiveID("app service", serviceBuild.AppServiceID); err != nil {
			return err
		}
	}
	return nil
}

func validateTargetDeployment(item TargetAppDeployment, appInstanceID int, allowedServiceIDs map[int]bool) error {
	if err := targetRequirePositiveID("app deployment", item.ID); err != nil {
		return err
	}
	if err := targetRequirePositiveID("app instance", item.AppInstanceID); err != nil {
		return err
	}
	if appInstanceID > 0 && item.AppInstanceID != appInstanceID {
		return errors.Errorf("target app deployment ID %d belongs to app instance ID %d, expected %d", item.ID, item.AppInstanceID, appInstanceID)
	}
	if err := targetValidateOptionalPositiveID("task", item.TaskID); err != nil {
		return err
	}
	if err := targetValidateOptionalPositiveID("post-deployment task", item.PostDeploymentTaskID); err != nil {
		return err
	}
	if err := validateTargetBuilds(item.Builds, item.AppInstanceID, nil); err != nil {
		return err
	}
	for _, service := range item.AppServiceDeployments {
		if err := targetRequirePositiveID("app service deployment", service.ID); err != nil {
			return err
		}
		if err := targetRequirePositiveID("app service", service.AppServiceID); err != nil {
			return err
		}
		if allowedServiceIDs != nil && !allowedServiceIDs[service.AppServiceID] {
			return errors.Errorf("target app service deployment ID %d belongs to unrequested app service ID %d", service.ID, service.AppServiceID)
		}
		if err := targetValidateOptionalPositiveID("app service build", service.AppServiceBuildID); err != nil {
			return err
		}
	}
	return nil
}

func targetPagedInstanceQuery(appInstanceID int, page TargetPageOptions) (url.Values, error) {
	query, err := targetRequiredQueryID("appInstanceId", "app instance", appInstanceID)
	if err != nil {
		return nil, err
	}
	if page.Page < 0 || page.PageSize < 0 {
		return nil, errors.New("target page and page size must not be negative")
	}
	if page.Page > 0 {
		query.Set("page", strconv.Itoa(page.Page))
	}
	if page.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(page.PageSize))
	}
	return query, nil
}

func targetRequiredQueryID(queryName, label string, id int) (url.Values, error) {
	if err := targetRequirePositiveID(label, id); err != nil {
		return nil, err
	}
	return url.Values{queryName: []string{strconv.Itoa(id)}}, nil
}

func targetRequirePositiveID(label string, id int) error {
	if id <= 0 {
		return errors.Errorf("target %s ID must be positive", label)
	}
	return nil
}

func targetValidateOptionalPositiveID(label string, id *int) error {
	if id == nil {
		return nil
	}
	return targetRequirePositiveID(label, *id)
}

func targetValidateOptionalNonNegativeID(label string, id *int) error {
	if id != nil && *id < 0 {
		return errors.Errorf("target %s ID must not be negative", label)
	}
	return nil
}

func targetUnexpectedID(label string, actual, expected int) error {
	return errors.Errorf("target Wodby 2 %s response ID %d does not match requested ID %d", label, actual, expected)
}

func targetSafePathName(label, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.Errorf("target %s name is required", label)
	}
	if url.PathEscape(value) != value {
		return "", errors.Errorf("target %s name %q is not a safe URL path segment", label, value)
	}
	return value, nil
}

func targetIDSet(ids []int) map[int]bool {
	result := make(map[int]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result
}

func targetEqualOptionalID(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
