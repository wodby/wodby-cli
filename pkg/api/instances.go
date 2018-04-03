package api

import (
	"net/http"

	"github.com/wodby/wodby-cli/pkg/types"
)

// DeployBuildPayload is the deploy build action payload.
type DeployBuildPayload struct {
	Tag        string               `json:"tag"`
	Metadata   *types.BuildMetadata `json:"info"`
	PostDeploy *bool                `json:"post_deployment,omitempty"`
}

// NewGetBuildConfigRequest makes new build config request.
func (c *Client) NewGetBuildConfigRequest(UUID string) (*http.Request, error) {
	u := c.NewURL("/instances/%s/build-config", UUID)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// GetBuildConfig does build config request.
func (c *Client) GetBuildConfig(UUID string) (*types.BuildConfig, error) {
	req, err := c.NewGetBuildConfigRequest(UUID)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}

	config := new(types.BuildConfig)
	err = c.DecodeResponse(resp, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// NewDeployBuildRequest makes new deploy build request.
func (c *Client) NewDeployBuildRequest(UUID string, payload *DeployBuildPayload) (*http.Request, error) {
	u := c.NewURL("/instances/%s/deploy/build", UUID)

	body, err := c.EncodePayload(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", u.String(), body)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// DeployBuild does deploy build request.
func (c *Client) DeployBuild(UUID string, payload *DeployBuildPayload) (*ResTask, error) {
	req, err := c.NewDeployBuildRequest(UUID, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}

	result := new(ResTask)
	err = c.DecodeResponse(resp, result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
