package api

import (
	"context"
	"net/http"
	"os"

	"github.com/hasura/go-graphql-client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/types"
)

type transport struct {
	underlyingTransport http.RoundTripper
	apiKey              string
	accessToken         string
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.apiKey != "" {
		req.Header.Set("X-API-KEY", t.apiKey)
	} else {
		req.Header.Set("X-ACCESS-TOKEN", t.accessToken)
	}
	return t.underlyingTransport.RoundTrip(req)
}

type Client struct {
	client *graphql.Client
	config types.APIConfig
	logger *logrus.Entry
}

func NewClient(config types.APIConfig) *Client {
	if os.Getenv("DEBUG") != "" {
		logrus.SetLevel(logrus.DebugLevel)
	}
	httpClient := http.Client{Transport: &transport{underlyingTransport: http.DefaultTransport}}
	return &Client{
		client: graphql.NewClient(config.Endpoint, &httpClient),
		config: config,
		logger: logrus.WithField("logger", "client"),
	}
}

func (c *Client) GetAppBuild(ctx context.Context, id int) (types.AppBuild, error) {
	var query struct {
		AppBuild types.AppBuild `graphql:"appBuild(id: $id)"`
	}
	variables := map[string]interface{}{"id": id}
	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	return query.AppBuild, nil
}

func (c *Client) GetDockerRegistryCredentials(ctx context.Context, appBuildID int) (types.DockerRegistryCredentials, error) {
	var query struct {
		DockerRegistryCredentials types.DockerRegistryCredentials `graphql:"dockerRegistryCredentials(appBuildID: $appBuildID)"`
	}
	variables := map[string]interface{}{"appBuildID": appBuildID}
	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}
	return query.DockerRegistryCredentials, nil
}

func (c *Client) Deploy(ctx context.Context, input types.DeploymentInput) (types.AppDeployment, error) {
	var m struct {
		deployFromCI types.AppDeployment `graphql:"deployFromCI(input: $input)"`
	}
	variables := map[string]interface{}{"input": input}

	err := c.client.Mutate(ctx, &m, variables)
	if err != nil {
		return types.AppDeployment{}, errors.WithStack(err)
	}

	return m.deployFromCI, nil
}

func (c *Client) NewCIBuild(ctx context.Context, input types.NewBuildFromCIInput) (types.AppBuild, error) {
	var m struct {
		newBuildFromCI types.AppBuild `graphql:"newBuildFromCI(input: $input)"`
	}
	variables := map[string]interface{}{"input": input}

	err := c.client.Mutate(ctx, &m, variables)
	if err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}

	return m.newBuildFromCI, nil
}
