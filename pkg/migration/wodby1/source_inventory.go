package wodby1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

const maxSourceListSize = 8 << 20

// SourceInventory lists what a Wodby 1 token can migrate: its organizations
// with their apps, instances and servers. It holds identifiers and names only.
type SourceInventory struct {
	Orgs []SourceInventoryOrg `json:"orgs"`
}

type SourceInventoryOrg struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Title   string                  `json:"title"`
	Apps    []SourceInventoryApp    `json:"apps"`
	Servers []SourceInventoryServer `json:"servers"`
}

type SourceInventoryApp struct {
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	Title     string                    `json:"title"`
	Status    string                    `json:"status,omitempty"`
	Instances []SourceInventoryInstance `json:"instances"`
}

type SourceInventoryInstance struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Type     string `json:"type,omitempty"`
	Status   string `json:"status,omitempty"`
	ServerID string `json:"serverId,omitempty"`
}

type SourceInventoryServer struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

// sourceText decodes a Wodby 1 value that may be a string or a number.
type sourceText string

func (t *sourceText) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*t = sourceText(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*t = sourceText(number.String())
		return nil
	}
	if string(data) == "null" {
		*t = ""
		return nil
	}
	return errors.Errorf("unexpected Wodby 1 value %s", string(data))
}

type sourceListItem struct {
	ID       sourceText `json:"id"`
	Name     sourceText `json:"name"`
	Title    sourceText `json:"title"`
	Status   sourceText `json:"status"`
	Type     sourceText `json:"type"`
	OrgID    sourceText `json:"org_id"`
	AppID    sourceText `json:"app_id"`
	ServerID sourceText `json:"server_id"`
}

// SourceInventoryFilter narrows ListInventoryFiltered.
type SourceInventoryFilter struct {
	// Org selects Wodby 1 organizations by ID, name or title, ignoring case.
	// Exact matches win; otherwise a name or title that contains it matches.
	Org string
	// OrgsOnly lists the organizations without their apps and servers, with a
	// single request, for a token that sees many of them.
	OrgsOnly bool
}

// ListInventory reads the organizations the token's user belongs to, with
// their apps, instances and servers, through the Wodby 1 API.
func (c *SourceClient) ListInventory(ctx context.Context) (SourceInventory, error) {
	return c.ListInventoryFiltered(ctx, SourceInventoryFilter{})
}

// ListInventoryFiltered reads the organizations the filter selects, with their
// apps, instances and servers unless OrgsOnly is set. Each organization takes
// three more Wodby 1 requests, so a token that sees many should select one.
func (c *SourceClient) ListInventoryFiltered(ctx context.Context, filter SourceInventoryFilter) (SourceInventory, error) {
	orgs, err := c.listSource(ctx, "/api/v3/orgs", nil)
	if err != nil {
		return SourceInventory{}, errors.Wrap(err, "list Wodby 1 organizations")
	}
	orgs = selectSourceOrgs(orgs, filter.Org)
	inventory := SourceInventory{Orgs: []SourceInventoryOrg{}}
	if filter.OrgsOnly {
		for _, org := range orgs {
			inventory.Orgs = append(inventory.Orgs, SourceInventoryOrg{
				ID: string(org.ID), Name: string(org.Name), Title: string(org.Title),
				Apps: []SourceInventoryApp{}, Servers: []SourceInventoryServer{},
			})
		}
		sort.Slice(inventory.Orgs, func(i, j int) bool { return inventory.Orgs[i].Name < inventory.Orgs[j].Name })
		return inventory, nil
	}
	for _, org := range orgs {
		query := url.Values{"org_id": []string{string(org.ID)}}
		apps, err := c.listSource(ctx, "/api/v3/apps", query)
		if err != nil {
			return SourceInventory{}, errors.Wrapf(err, "list Wodby 1 apps of organization %q", org.Name)
		}
		instances, err := c.listSource(ctx, "/api/v3/instances", query)
		if err != nil {
			return SourceInventory{}, errors.Wrapf(err, "list Wodby 1 instances of organization %q", org.Name)
		}
		servers, err := c.listSource(ctx, "/api/v3/servers", query)
		if err != nil {
			return SourceInventory{}, errors.Wrapf(err, "list Wodby 1 servers of organization %q", org.Name)
		}
		item := SourceInventoryOrg{
			ID: string(org.ID), Name: string(org.Name), Title: string(org.Title),
			Apps: []SourceInventoryApp{}, Servers: []SourceInventoryServer{},
		}
		byApp := map[string][]SourceInventoryInstance{}
		for _, instance := range instances {
			byApp[string(instance.AppID)] = append(byApp[string(instance.AppID)], SourceInventoryInstance{
				ID: string(instance.ID), Name: string(instance.Name), Title: string(instance.Title),
				Type: string(instance.Type), Status: string(instance.Status), ServerID: string(instance.ServerID),
			})
		}
		for _, app := range apps {
			appInstances := byApp[string(app.ID)]
			sort.Slice(appInstances, func(i, j int) bool { return appInstances[i].Name < appInstances[j].Name })
			if appInstances == nil {
				appInstances = []SourceInventoryInstance{}
			}
			item.Apps = append(item.Apps, SourceInventoryApp{
				ID: string(app.ID), Name: string(app.Name), Title: string(app.Title), Status: string(app.Status),
				Instances: appInstances,
			})
		}
		for _, server := range servers {
			item.Servers = append(item.Servers, SourceInventoryServer{
				ID: string(server.ID), Title: string(server.Title), Status: string(server.Status),
			})
		}
		sort.Slice(item.Apps, func(i, j int) bool { return item.Apps[i].Name < item.Apps[j].Name })
		sort.Slice(item.Servers, func(i, j int) bool { return item.Servers[i].Title < item.Servers[j].Title })
		inventory.Orgs = append(inventory.Orgs, item)
	}
	sort.Slice(inventory.Orgs, func(i, j int) bool { return inventory.Orgs[i].Name < inventory.Orgs[j].Name })
	return inventory, nil
}

// selectSourceOrgs keeps the organizations that match selector, preferring
// exact matches of ID, name or title over names and titles that contain it.
func selectSourceOrgs(orgs []sourceListItem, selector string) []sourceListItem {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		return orgs
	}
	exact, partial := []sourceListItem{}, []sourceListItem{}
	for _, org := range orgs {
		id, name, title := strings.ToLower(string(org.ID)), strings.ToLower(string(org.Name)), strings.ToLower(string(org.Title))
		switch {
		case id == selector || name == selector || title == selector:
			exact = append(exact, org)
		case strings.Contains(name, selector) || strings.Contains(title, selector):
			partial = append(partial, org)
		}
	}
	if len(exact) != 0 {
		return exact
	}
	return partial
}

func (c *SourceClient) listSource(ctx context.Context, path string, query url.Values) ([]sourceListItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(path, query), nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", strings.TrimSpace(c.token))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceListSize+1))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if int64(len(body)) > maxSourceListSize {
		return nil, errors.Errorf("Wodby 1 list response exceeds the %d-byte safety limit", maxSourceListSize)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, sourceExportAPIError(resp.StatusCode, resp.Status, body)
	}
	items := []sourceListItem{}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode Wodby 1 list response: %w", err)
	}
	return items, nil
}
