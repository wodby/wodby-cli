package wodby1

import (
	"context"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
	"github.com/wodby/wodby-cli/pkg/types"
)

// TargetClient provides the Wodby 2 API operations required by migration
// planning and execution.
type TargetClient struct {
	client *rest.Client
}

// TargetCurrentUser is the authenticated Wodby 2 account relevant to
// migration authorization. Organization authorization is derived from the
// selected organization's membership, not the account's platform-admin flag.
type TargetCurrentUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	// IsAdmin is retained for response compatibility only. Migration
	// authorization must never use this platform-level flag.
	IsAdmin bool `json:"isAdmin"`
}

// TargetOrgMembership is the authenticated account's relationship to a
// Wodby 2 organization.
type TargetOrgMembership struct {
	ID     int    `json:"id"`
	UserID *int   `json:"userId,omitempty"`
	OrgID  int    `json:"orgId"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

func NewTargetClient(config types.APIConfig) (*TargetClient, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if endpoint.User != nil {
		return nil, errors.New("Wodby 2 API base URL must not contain user credentials")
	}
	if !strings.EqualFold(endpoint.Scheme, "https") && !isLoopbackHost(endpoint.Hostname()) {
		return nil, errors.New("Wodby 2 API base URL must use HTTPS (plain HTTP is allowed only for loopback development)")
	}
	config.Key = strings.TrimSpace(config.Key)
	config.AccessToken = strings.TrimSpace(config.AccessToken)
	client, err := rest.NewClient(config)
	if err != nil {
		return nil, err
	}
	return &TargetClient{client: client}, nil
}

// CurrentUser fetches the authenticated Wodby 2 user.
func (c *TargetClient) CurrentUser(ctx context.Context) (TargetCurrentUser, error) {
	var user TargetCurrentUser
	if err := c.client.Get(ctx, "/user", nil, &user); err != nil {
		return TargetCurrentUser{}, errors.Wrap(err, "fetch target Wodby 2 user")
	}
	return user, nil
}

// ListOrgMemberships fetches memberships in the selected organization. The
// target API returns only the caller's own membership to users without
// organization-wide membership visibility.
func (c *TargetClient) ListOrgMemberships(ctx context.Context, orgID int) ([]TargetOrgMembership, error) {
	query, err := targetOrgQuery(orgID)
	if err != nil {
		return nil, err
	}
	items := []TargetOrgMembership{}
	if err := c.client.Get(ctx, "/org-memberships", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 organization memberships")
	}
	return items, nil
}

// RequireOrgOwnerOrAdmin rejects credentials that are not an active OWNER or ADMIN
// of the selected Wodby 2 organization.
func (c *TargetClient) RequireOrgOwnerOrAdmin(ctx context.Context, orgID int) (TargetCurrentUser, TargetOrgMembership, error) {
	if orgID <= 0 {
		return TargetCurrentUser{}, TargetOrgMembership{}, errors.New("target organization ID must be positive")
	}
	user, err := c.CurrentUser(ctx)
	if err != nil {
		return TargetCurrentUser{}, TargetOrgMembership{}, err
	}
	if user.ID <= 0 {
		return TargetCurrentUser{}, TargetOrgMembership{}, errors.New("target Wodby 2 user response has an invalid ID")
	}

	memberships, err := c.ListOrgMemberships(ctx, orgID)
	if err != nil {
		return TargetCurrentUser{}, TargetOrgMembership{}, err
	}

	var match TargetOrgMembership
	found := false
	for _, membership := range memberships {
		if membership.OrgID != orgID {
			return TargetCurrentUser{}, TargetOrgMembership{}, errors.Errorf(
				"target Wodby 2 membership response contains organization ID %d, expected %d",
				membership.OrgID,
				orgID,
			)
		}
		if membership.UserID == nil || *membership.UserID != user.ID {
			continue
		}
		if found {
			return TargetCurrentUser{}, TargetOrgMembership{}, errors.Errorf(
				"target Wodby 2 returned multiple memberships for user ID %d in organization ID %d",
				user.ID,
				orgID,
			)
		}
		match = membership
		found = true
	}

	role := strings.ToLower(strings.TrimSpace(match.Role))
	status := strings.ToLower(strings.TrimSpace(match.Status))
	if match.ID <= 0 || status != "ok" || (role != "owner" && role != "admin") {
		return TargetCurrentUser{}, TargetOrgMembership{}, errors.Errorf(
			"target Wodby 2 organization OWNER or ADMIN credentials are required for organization ID %d",
			orgID,
		)
	}
	match.Role = role
	match.Status = status
	return user, match, nil
}
