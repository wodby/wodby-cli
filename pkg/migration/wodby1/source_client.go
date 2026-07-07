package wodby1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const defaultSourceTimeout = 30 * time.Second

type SourceClient struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

func NewSourceClient(baseURL string, token string) (*SourceClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.Errorf("invalid source base url %q", baseURL)
	}
	return &SourceClient{
		baseURL: parsed,
		token:   token,
		httpClient: &http.Client{
			Timeout: defaultSourceTimeout,
		},
	}, nil
}

func (c *SourceClient) ExportApp(ctx context.Context, uuid string, includeSecrets bool) (Export, error) {
	return c.getExport(ctx, "/api/v4/migrations/apps/"+url.PathEscape(uuid)+"/export", includeSecrets)
}

func (c *SourceClient) ExportServer(ctx context.Context, uuid string, includeSecrets bool) (Export, error) {
	return c.getExport(ctx, "/api/v4/migrations/servers/"+url.PathEscape(uuid)+"/export", includeSecrets)
}

func (c *SourceClient) getExport(ctx context.Context, path string, includeSecrets bool) (Export, error) {
	query := url.Values{}
	if includeSecrets {
		query.Set("include_secrets", "true")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(path, query), nil)
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", authHeader(c.token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024*20))
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Export{}, fmt.Errorf("source API request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var export Export
	if err := json.Unmarshal(body, &export); err != nil {
		return Export{}, errors.WithStack(err)
	}
	if err := export.Validate(); err != nil {
		return Export{}, err
	}
	return export, nil
}

func (c *SourceClient) resolve(path string, query url.Values) string {
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

func authHeader(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "token ") {
		return token
	}
	return "token " + token
}
