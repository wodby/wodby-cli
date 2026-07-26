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
// migration authorization.
type TargetCurrentUser struct {
	ID      int    `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"isAdmin"`
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

// CurrentUser fetches the authenticated Wodby 2 user and their global
// administrator status.
func (c *TargetClient) CurrentUser(ctx context.Context) (TargetCurrentUser, error) {
	var user TargetCurrentUser
	if err := c.client.Get(ctx, "/user", nil, &user); err != nil {
		return TargetCurrentUser{}, errors.Wrap(err, "fetch target Wodby 2 user")
	}
	return user, nil
}

// RequireAdmin rejects credentials that do not belong to a global Wodby 2
// administrator. Migration-specific target operations must call this before
// discovery or mutation.
func (c *TargetClient) RequireAdmin(ctx context.Context) (TargetCurrentUser, error) {
	user, err := c.CurrentUser(ctx)
	if err != nil {
		return TargetCurrentUser{}, err
	}
	if !user.IsAdmin {
		return TargetCurrentUser{}, errors.New("target Wodby 2 administrator credentials are required for migration")
	}
	return user, nil
}
