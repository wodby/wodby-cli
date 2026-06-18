package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/types"
)

const defaultHTTPTimeout = 30 * time.Second

type transport struct {
	underlyingTransport http.RoundTripper
	apiKey              string
	accessToken         string
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.apiKey != "" {
		req.Header.Set("X-API-KEY", t.apiKey)
	} else if t.accessToken != "" {
		req.Header.Set("X-ACCESS-TOKEN", t.accessToken)
	}
	return t.underlyingTransport.RoundTrip(req)
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type ErrorResponse struct {
	Message string `json:"message"`
}

func NewClient(config types.APIConfig) (*Client, error) {
	baseURL, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.Errorf("invalid api base url %q", config.Endpoint)
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &transport{
				underlyingTransport: http.DefaultTransport,
				apiKey:              config.Key,
				accessToken:         config.AccessToken,
			},
			Timeout: defaultHTTPTimeout,
		},
	}, nil
}

func (c *Client) Get(ctx context.Context, path string, query url.Values, out interface{}) error {
	return c.Do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, query url.Values, body interface{}, out interface{}) error {
	return c.Do(ctx, http.MethodPost, path, query, body, out)
}

func (c *Client) Put(ctx context.Context, path string, query url.Values, body interface{}, out interface{}) error {
	return c.Do(ctx, http.MethodPut, path, query, body, out)
}

func (c *Client) Delete(ctx context.Context, path string, query url.Values, out interface{}) error {
	return c.Do(ctx, http.MethodDelete, path, query, nil, out)
}

func (c *Client) Do(ctx context.Context, method string, path string, query url.Values, body interface{}, out interface{}) error {
	req, err := c.newRequest(ctx, method, path, query, body)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return decodeError(resp)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errors.WithStack(err)
	}

	return nil
}

func (c *Client) newRequest(ctx context.Context, method string, path string, query url.Values, body interface{}) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		content, err := json.Marshal(body)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		reader = bytes.NewReader(content)
	}

	endpoint := c.resolve(path, query)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func (c *Client) resolve(path string, query url.Values) string {
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	reqPath := strings.TrimLeft(path, "/")
	if reqPath == "" {
		u.Path = basePath
	} else {
		u.Path = basePath + "/" + reqPath
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func decodeError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return errors.WithStack(err)
	}

	var apiErr ErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		return errors.Errorf("api request failed: %s (%s)", apiErr.Message, resp.Status)
	}
	if len(bytes.TrimSpace(body)) != 0 {
		return errors.Errorf("api request failed: %s: %s", resp.Status, string(body))
	}

	return fmt.Errorf("api request failed: %s", resp.Status)
}
