package wodby1

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

const (
	TargetBlockerSelectorRequired = "selector_required"
	TargetBlockerSelectorInvalid  = "selector_invalid"
	TargetBlockerNotFound         = "not_found"
	TargetBlockerAmbiguous        = "ambiguous"
	TargetBlockerWrongOrg         = "wrong_organization"
	TargetBlockerWrongProject     = "wrong_project"

	TargetOwnershipScopeOrg     = "org"
	TargetOwnershipScopeProject = "project"
)

type TargetOrg struct {
	ID              int                    `json:"id"`
	Name            string                 `json:"name"`
	Title           string                 `json:"title"`
	Domain          string                 `json:"domain,omitempty"`
	DefaultTimeZone string                 `json:"defaultTimeZone,omitempty"`
	Capabilities    *TargetOrgCapabilities `json:"capabilities,omitempty"`
	Subscription    *TargetOrgSubscription `json:"subscription,omitempty"`
}

type TargetOrgCapabilities struct {
	CustomDomains    bool `json:"customDomains"`
	AutoBackups      bool `json:"autoBackups"`
	Users            bool `json:"users"`
	Projects         bool `json:"projects"`
	CronSchedules    bool `json:"cronSchedules"`
	Autoscale        bool `json:"autoscale"`
	AppInstancePause bool `json:"appInstancePause"`
	WebShell         bool `json:"webShell"`
	WodbyCloud       bool `json:"wodbyCloud"`
}

type TargetOrgSubscription struct {
	Status string                     `json:"status"`
	Plan   *TargetOrgSubscriptionPlan `json:"plan,omitempty"`
}

type TargetOrgSubscriptionPlan struct {
	Name          string  `json:"name"`
	Title         string  `json:"title"`
	Usage         float64 `json:"usage"`
	UsageIncluded float64 `json:"usageIncluded"`
	SpendingLimit float64 `json:"spendingLimit"`
	PricePerUnit  float64 `json:"pricePerUnit"`
}

type TargetProject struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	OrgID int    `json:"orgId"`
}

type TargetClusterCapabilities struct {
	EnvoyGateway   bool `json:"envoyGateway"`
	RedirectRoutes bool `json:"redirectRoutes"`
}

type TargetCluster struct {
	ID             int                       `json:"id"`
	Name           string                    `json:"name"`
	Title          string                    `json:"title"`
	Status         string                    `json:"status"`
	OrgID          int                       `json:"orgId"`
	OwnershipScope string                    `json:"ownershipScope"`
	OwnerProjectID int                       `json:"ownerProjectId,omitempty"`
	IPs            []string                  `json:"ips,omitempty"`
	Hostname       *string                   `json:"hostname,omitempty"`
	Capabilities   TargetClusterCapabilities `json:"capabilities"`
}

type TargetEnv struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Type  string `json:"type"`
	OrgID int    `json:"orgId"`
}

type TargetScopeSelectors struct {
	Org     string
	Project string
	Cluster string
}

type TargetDiscoveryRequest struct {
	TargetScopeSelectors
	Environments []string
}

type TargetScopeDiscovery struct {
	User       TargetCurrentUser   `json:"user"`
	Membership TargetOrgMembership `json:"membership"`
	Org        TargetOrg           `json:"org"`
	Project    TargetProject       `json:"project"`
	Cluster    TargetCluster       `json:"cluster"`
}

type TargetResolvedEnv struct {
	Selector string    `json:"selector"`
	Env      TargetEnv `json:"env"`
}

type TargetDiscoveryResult struct {
	TargetScopeDiscovery
	Environments []TargetResolvedEnv `json:"environments"`
}

// TargetDiscoveryBlocker is a stable, machine-readable selector or
// relationship failure that callers can turn into a migration review item.
type TargetDiscoveryBlocker struct {
	Code          string `json:"code"`
	Resource      string `json:"resource"`
	Selector      string `json:"selector,omitempty"`
	ExpectedOrgID int    `json:"expectedOrgId,omitempty"`
	ActualOrgID   int    `json:"actualOrgId,omitempty"`
	ProjectID     int    `json:"projectId,omitempty"`
}

func (e *TargetDiscoveryBlocker) Error() string {
	switch e.Code {
	case TargetBlockerSelectorRequired:
		return fmt.Sprintf("target %s selector is required", e.Resource)
	case TargetBlockerSelectorInvalid:
		return fmt.Sprintf("target %s selector %q must be a positive ID or exact name", e.Resource, e.Selector)
	case TargetBlockerNotFound:
		if e.ExpectedOrgID > 0 {
			return fmt.Sprintf("target %s selector %q was not found in organization ID %d", e.Resource, e.Selector, e.ExpectedOrgID)
		}
		return fmt.Sprintf("target %s selector %q was not found", e.Resource, e.Selector)
	case TargetBlockerAmbiguous:
		return fmt.Sprintf("target %s selector %q matched multiple resources", e.Resource, e.Selector)
	case TargetBlockerWrongOrg:
		return fmt.Sprintf(
			"target %s selector %q belongs to organization ID %d, expected organization ID %d",
			e.Resource,
			e.Selector,
			e.ActualOrgID,
			e.ExpectedOrgID,
		)
	case TargetBlockerWrongProject:
		return fmt.Sprintf("target cluster selector %q is not associated with project ID %d", e.Selector, e.ProjectID)
	default:
		return fmt.Sprintf("target %s selector %q could not be resolved", e.Resource, e.Selector)
	}
}

func (c *TargetClient) ListOrgs(ctx context.Context) ([]TargetOrg, error) {
	items := []TargetOrg{}
	if err := c.client.Get(ctx, "/orgs", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 organizations")
	}
	sort.Slice(items, func(i, j int) bool {
		return compareTargetIdentity(items[i].ID, items[i].Name, items[j].ID, items[j].Name)
	})
	return items, nil
}

func (c *TargetClient) GetOrg(ctx context.Context, id int) (TargetOrg, error) {
	if id <= 0 {
		return TargetOrg{}, errors.New("target organization ID must be positive")
	}
	var item TargetOrg
	if err := c.client.Get(ctx, "/orgs/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetOrg{}, errors.Wrap(err, "get target Wodby 2 organization")
	}
	return item, nil
}

func (c *TargetClient) ListProjects(ctx context.Context, orgID int) ([]TargetProject, error) {
	query, err := targetOrgQuery(orgID)
	if err != nil {
		return nil, err
	}
	items := []TargetProject{}
	if err := c.client.Get(ctx, "/projects", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 projects")
	}
	sort.Slice(items, func(i, j int) bool {
		return compareTargetIdentity(items[i].ID, items[i].Name, items[j].ID, items[j].Name)
	})
	return items, nil
}

func (c *TargetClient) GetProject(ctx context.Context, id int) (TargetProject, error) {
	if id <= 0 {
		return TargetProject{}, errors.New("target project ID must be positive")
	}
	var item TargetProject
	if err := c.client.Get(ctx, "/projects/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetProject{}, errors.Wrap(err, "get target Wodby 2 project")
	}
	return item, nil
}

func (c *TargetClient) ListClusters(ctx context.Context, orgID int, projectIDs []int) ([]TargetCluster, error) {
	query, err := targetOrgQuery(orgID)
	if err != nil {
		return nil, err
	}
	projectIDs, err = normalizePositiveIDs(projectIDs, "target project")
	if err != nil {
		return nil, err
	}
	if len(projectIDs) != 0 {
		values := make([]string, len(projectIDs))
		for i, id := range projectIDs {
			values[i] = strconv.Itoa(id)
		}
		query.Set("projectIds", strings.Join(values, ","))
	}

	items := []TargetCluster{}
	if err := c.client.Get(ctx, "/clusters", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 clusters")
	}
	sort.Slice(items, func(i, j int) bool {
		return compareTargetIdentity(items[i].ID, items[i].Name, items[j].ID, items[j].Name)
	})
	return items, nil
}

func (c *TargetClient) GetCluster(ctx context.Context, id int) (TargetCluster, error) {
	if id <= 0 {
		return TargetCluster{}, errors.New("target cluster ID must be positive")
	}
	var item TargetCluster
	if err := c.client.Get(ctx, "/clusters/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetCluster{}, errors.Wrap(err, "get target Wodby 2 cluster")
	}
	return item, nil
}

func (c *TargetClient) ListEnvs(ctx context.Context, orgID int) ([]TargetEnv, error) {
	query, err := targetOrgQuery(orgID)
	if err != nil {
		return nil, err
	}
	items := []TargetEnv{}
	if err := c.client.Get(ctx, "/envs", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 environments")
	}
	sort.Slice(items, func(i, j int) bool {
		return compareTargetIdentity(items[i].ID, items[i].Name, items[j].ID, items[j].Name)
	})
	return items, nil
}

func (c *TargetClient) GetEnv(ctx context.Context, id int) (TargetEnv, error) {
	if id <= 0 {
		return TargetEnv{}, errors.New("target environment ID must be positive")
	}
	var item TargetEnv
	if err := c.client.Get(ctx, "/envs/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetEnv{}, errors.Wrap(err, "get target Wodby 2 environment")
	}
	return item, nil
}

// DiscoverTargetScope derives the target organization from the API key (or
// validates an explicitly expected organization), verifies that the
// authenticated account is an active OWNER or ADMIN, and then resolves the
// optional project and required cluster selectors. When a project is selected,
// cluster availability is verified through the projectIds list filter. Without
// an explicit project, a project-owned cluster defaults the target app to its
// owner project; organization-owned clusters keep the target app
// organization-owned.
func (c *TargetClient) DiscoverTargetScope(ctx context.Context, selectors TargetScopeSelectors) (TargetScopeDiscovery, error) {
	clusterSelector, err := normalizeTargetSelector("cluster", selectors.Cluster)
	if err != nil {
		return TargetScopeDiscovery{}, err
	}

	org, err := c.resolveTargetOrg(ctx, selectors.Org)
	if err != nil {
		return TargetScopeDiscovery{}, err
	}
	user, membership, err := c.RequireOrgOwnerOrAdmin(ctx, org.ID)
	if err != nil {
		return TargetScopeDiscovery{}, err
	}
	var project TargetProject
	projectSelector := strings.TrimSpace(selectors.Project)
	if projectSelector != "" {
		projectSelector, err = normalizeTargetSelector("project", projectSelector)
		if err != nil {
			return TargetScopeDiscovery{}, err
		}
		project, err = c.resolveProject(ctx, org.ID, projectSelector)
		if err != nil {
			return TargetScopeDiscovery{}, err
		}
	}
	cluster, err := c.resolveCluster(ctx, org.ID, project.ID, clusterSelector)
	if err != nil {
		return TargetScopeDiscovery{}, err
	}
	if project.ID == 0 {
		project, err = c.resolveClusterOwnerProject(ctx, org.ID, cluster)
		if err != nil {
			return TargetScopeDiscovery{}, err
		}
	}

	return TargetScopeDiscovery{
		User:       user,
		Membership: membership,
		Org:        org,
		Project:    project,
		Cluster:    cluster,
	}, nil
}

// resolveTargetOrg derives the destination from the organization-scoped API
// key when no selector is supplied. An explicit selector remains supported by
// the discovery package for callers that need to validate an expected org.
func (c *TargetClient) resolveTargetOrg(ctx context.Context, selector string) (TargetOrg, error) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		normalized, err := normalizeTargetSelector("organization", selector)
		if err != nil {
			return TargetOrg{}, err
		}
		return c.resolveOrg(ctx, normalized)
	}

	orgs, err := c.ListOrgs(ctx)
	if err != nil {
		return TargetOrg{}, errors.Wrap(err, "resolve target organization from Wodby 2 API key")
	}
	switch len(orgs) {
	case 0:
		return TargetOrg{}, errors.New("target Wodby 2 API key does not expose an organization")
	case 1:
		return orgs[0], nil
	default:
		return TargetOrg{}, errors.Errorf(
			"target Wodby 2 API key exposed %d organizations; expected exactly one organization-scoped key",
			len(orgs),
		)
	}
}

func (c *TargetClient) resolveClusterOwnerProject(ctx context.Context, orgID int, cluster TargetCluster) (TargetProject, error) {
	switch strings.ToLower(strings.TrimSpace(cluster.OwnershipScope)) {
	case TargetOwnershipScopeOrg:
		return TargetProject{}, nil
	case TargetOwnershipScopeProject:
		if cluster.OwnerProjectID <= 0 {
			return TargetProject{}, errors.Errorf(
				"target cluster %q is project-owned but did not return an owner project ID",
				cluster.Name,
			)
		}
		return c.resolveProject(ctx, orgID, strconv.Itoa(cluster.OwnerProjectID))
	case "":
		return TargetProject{}, errors.Errorf("target cluster %q did not return an ownership scope", cluster.Name)
	default:
		return TargetProject{}, errors.Errorf(
			"target cluster %q returned unsupported ownership scope %q",
			cluster.Name,
			cluster.OwnershipScope,
		)
	}
}

// DiscoverTarget performs complete read-only target discovery. Callers that
// must verify admin access before loading a source export can call
// DiscoverTargetScope first, then ResolveTargetEnvs after planning.
func (c *TargetClient) DiscoverTarget(ctx context.Context, request TargetDiscoveryRequest) (TargetDiscoveryResult, error) {
	scope, err := c.DiscoverTargetScope(ctx, request.TargetScopeSelectors)
	if err != nil {
		return TargetDiscoveryResult{}, err
	}
	envs, err := c.ResolveTargetEnvs(ctx, scope.Org.ID, request.Environments)
	if err != nil {
		return TargetDiscoveryResult{}, err
	}
	return TargetDiscoveryResult{
		TargetScopeDiscovery: scope,
		Environments:         envs,
	}, nil
}

// ResolveTargetEnvs resolves exact environment IDs or names inside an
// organization already returned by DiscoverTargetScope. It performs no
// mutation and returns results ordered by normalized selector.
func (c *TargetClient) ResolveTargetEnvs(ctx context.Context, orgID int, selectors []string) ([]TargetResolvedEnv, error) {
	if orgID <= 0 {
		return nil, errors.New("target organization ID must be positive")
	}
	normalized := make([]string, 0, len(selectors))
	seen := map[string]bool{}
	for _, selector := range selectors {
		value, err := normalizeTargetSelector("environment", selector)
		if err != nil {
			return nil, err
		}
		if !seen[value] {
			seen[value] = true
			normalized = append(normalized, value)
		}
	}
	sort.Strings(normalized)

	result := make([]TargetResolvedEnv, 0, len(normalized))
	var envs []TargetEnv
	envsLoaded := false
	for _, selector := range normalized {
		id, isID, err := targetSelectorID("environment", selector)
		if err != nil {
			return nil, err
		}
		var env TargetEnv
		if isID {
			env, err = c.GetEnv(ctx, id)
			if err != nil {
				if isTargetNotFound(err) {
					return nil, newTargetNotFoundBlocker("environment", selector, orgID)
				}
				return nil, errors.Wrap(err, "resolve target environment selector")
			}
			if env.OrgID != orgID {
				return nil, newTargetWrongOrgBlocker("environment", selector, env.OrgID, orgID)
			}
		} else {
			if !envsLoaded {
				envs, err = c.ListEnvs(ctx, orgID)
				if err != nil {
					return nil, errors.Wrap(err, "resolve target environment selector")
				}
				envsLoaded = true
			}
			env, err = selectTargetEnvByName(envs, selector, orgID)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, TargetResolvedEnv{Selector: selector, Env: env})
	}
	return result, nil
}

func (c *TargetClient) resolveOrg(ctx context.Context, selector string) (TargetOrg, error) {
	id, isID, err := targetSelectorID("organization", selector)
	if err != nil {
		return TargetOrg{}, err
	}
	if isID {
		item, err := c.GetOrg(ctx, id)
		if err != nil {
			if isTargetNotFound(err) {
				return TargetOrg{}, newTargetNotFoundBlocker("organization", selector, 0)
			}
			return TargetOrg{}, errors.Wrap(err, "resolve target organization selector")
		}
		return item, nil
	}

	items, err := c.ListOrgs(ctx)
	if err != nil {
		return TargetOrg{}, errors.Wrap(err, "resolve target organization selector")
	}
	matches := make([]TargetOrg, 0, 1)
	for _, item := range items {
		if item.Name == selector {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return TargetOrg{}, newTargetNotFoundBlocker("organization", selector, 0)
	case 1:
		return matches[0], nil
	default:
		return TargetOrg{}, newTargetAmbiguousBlocker("organization", selector)
	}
}

func (c *TargetClient) resolveProject(ctx context.Context, orgID int, selector string) (TargetProject, error) {
	id, isID, err := targetSelectorID("project", selector)
	if err != nil {
		return TargetProject{}, err
	}
	if isID {
		item, err := c.GetProject(ctx, id)
		if err != nil {
			if isTargetNotFound(err) {
				return TargetProject{}, newTargetNotFoundBlocker("project", selector, orgID)
			}
			return TargetProject{}, errors.Wrap(err, "resolve target project selector")
		}
		if item.OrgID != orgID {
			return TargetProject{}, newTargetWrongOrgBlocker("project", selector, item.OrgID, orgID)
		}
		return item, nil
	}

	items, err := c.ListProjects(ctx, orgID)
	if err != nil {
		return TargetProject{}, errors.Wrap(err, "resolve target project selector")
	}
	matches := make([]TargetProject, 0, 1)
	wrongOrg := make([]TargetProject, 0, 1)
	for _, item := range items {
		if item.Name != selector {
			continue
		}
		if item.OrgID == orgID {
			matches = append(matches, item)
		} else {
			wrongOrg = append(wrongOrg, item)
		}
	}
	switch len(matches) {
	case 0:
		if len(wrongOrg) != 0 {
			return TargetProject{}, newTargetWrongOrgBlocker("project", selector, wrongOrg[0].OrgID, orgID)
		}
		return TargetProject{}, newTargetNotFoundBlocker("project", selector, orgID)
	case 1:
		return matches[0], nil
	default:
		return TargetProject{}, newTargetAmbiguousBlocker("project", selector)
	}
}

func (c *TargetClient) resolveCluster(ctx context.Context, orgID int, projectID int, selector string) (TargetCluster, error) {
	id, isID, err := targetSelectorID("cluster", selector)
	if err != nil {
		return TargetCluster{}, err
	}
	var item TargetCluster
	if isID {
		item, err = c.GetCluster(ctx, id)
		if err != nil {
			if isTargetNotFound(err) {
				return TargetCluster{}, newTargetNotFoundBlocker("cluster", selector, orgID)
			}
			return TargetCluster{}, errors.Wrap(err, "resolve target cluster selector")
		}
		if item.OrgID != orgID {
			return TargetCluster{}, newTargetWrongOrgBlocker("cluster", selector, item.OrgID, orgID)
		}
	} else {
		items, listErr := c.ListClusters(ctx, orgID, nil)
		if listErr != nil {
			return TargetCluster{}, errors.Wrap(listErr, "resolve target cluster selector")
		}
		item, err = selectTargetClusterByName(items, selector, orgID)
		if err != nil {
			return TargetCluster{}, err
		}
	}
	if projectID == 0 {
		return item, nil
	}

	projectClusters, err := c.ListClusters(ctx, orgID, []int{projectID})
	if err != nil {
		return TargetCluster{}, errors.Wrap(err, "validate target cluster project relationship")
	}
	for _, projectCluster := range projectClusters {
		if projectCluster.ID == item.ID {
			return item, nil
		}
	}
	return TargetCluster{}, &TargetDiscoveryBlocker{
		Code:      TargetBlockerWrongProject,
		Resource:  "cluster",
		Selector:  selector,
		ProjectID: projectID,
	}
}

func selectTargetClusterByName(items []TargetCluster, selector string, orgID int) (TargetCluster, error) {
	matches := make([]TargetCluster, 0, 1)
	wrongOrg := make([]TargetCluster, 0, 1)
	for _, item := range items {
		if item.Name != selector {
			continue
		}
		if item.OrgID == orgID {
			matches = append(matches, item)
		} else {
			wrongOrg = append(wrongOrg, item)
		}
	}
	switch len(matches) {
	case 0:
		if len(wrongOrg) != 0 {
			return TargetCluster{}, newTargetWrongOrgBlocker("cluster", selector, wrongOrg[0].OrgID, orgID)
		}
		return TargetCluster{}, newTargetNotFoundBlocker("cluster", selector, orgID)
	case 1:
		return matches[0], nil
	default:
		return TargetCluster{}, newTargetAmbiguousBlocker("cluster", selector)
	}
}

func selectTargetEnvByName(items []TargetEnv, selector string, orgID int) (TargetEnv, error) {
	matches := make([]TargetEnv, 0, 1)
	wrongOrg := make([]TargetEnv, 0, 1)
	for _, item := range items {
		if item.Name != selector {
			continue
		}
		if item.OrgID == orgID {
			matches = append(matches, item)
		} else {
			wrongOrg = append(wrongOrg, item)
		}
	}
	switch len(matches) {
	case 0:
		if len(wrongOrg) != 0 {
			return TargetEnv{}, newTargetWrongOrgBlocker("environment", selector, wrongOrg[0].OrgID, orgID)
		}
		return TargetEnv{}, newTargetNotFoundBlocker("environment", selector, orgID)
	case 1:
		return matches[0], nil
	default:
		return TargetEnv{}, newTargetAmbiguousBlocker("environment", selector)
	}
}

func normalizeTargetSelector(resource string, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", &TargetDiscoveryBlocker{
			Code:     TargetBlockerSelectorRequired,
			Resource: resource,
		}
	}
	return selector, nil
}

func targetSelectorID(resource string, selector string) (int, bool, error) {
	if !looksLikeInteger(selector) {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(selector, 10, strconv.IntSize)
	if err != nil || id <= 0 {
		return 0, false, &TargetDiscoveryBlocker{
			Code:     TargetBlockerSelectorInvalid,
			Resource: resource,
			Selector: selector,
		}
	}
	return int(id), true, nil
}

func looksLikeInteger(value string) bool {
	if value == "" {
		return false
	}
	start := 0
	if value[0] == '+' || value[0] == '-' {
		if len(value) == 1 {
			return false
		}
		start = 1
	}
	for i := start; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func targetOrgQuery(orgID int) (url.Values, error) {
	if orgID < 0 {
		return nil, errors.New("target organization ID cannot be negative")
	}
	query := url.Values{}
	if orgID > 0 {
		query.Set("orgId", strconv.Itoa(orgID))
	}
	return query, nil
}

func normalizePositiveIDs(ids []int, resource string) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	normalized := append([]int(nil), ids...)
	sort.Ints(normalized)
	result := normalized[:0]
	for _, id := range normalized {
		if id <= 0 {
			return nil, errors.Errorf("%s ID must be positive", resource)
		}
		if len(result) == 0 || result[len(result)-1] != id {
			result = append(result, id)
		}
	}
	return result, nil
}

func compareTargetIdentity(aID int, aName string, bID int, bName string) bool {
	if aID != bID {
		return aID < bID
	}
	return aName < bName
}

func isTargetNotFound(err error) bool {
	var apiErr *rest.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func newTargetNotFoundBlocker(resource string, selector string, orgID int) error {
	return &TargetDiscoveryBlocker{
		Code:          TargetBlockerNotFound,
		Resource:      resource,
		Selector:      selector,
		ExpectedOrgID: orgID,
	}
}

func newTargetAmbiguousBlocker(resource string, selector string) error {
	return &TargetDiscoveryBlocker{
		Code:     TargetBlockerAmbiguous,
		Resource: resource,
		Selector: selector,
	}
}

func newTargetWrongOrgBlocker(resource string, selector string, actualOrgID int, expectedOrgID int) error {
	return &TargetDiscoveryBlocker{
		Code:          TargetBlockerWrongOrg,
		Resource:      resource,
		Selector:      selector,
		ExpectedOrgID: expectedOrgID,
		ActualOrgID:   actualOrgID,
	}
}
