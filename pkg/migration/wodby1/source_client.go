package wodby1

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	defaultSourceTimeout = 30 * time.Second
	maxSourceExportSize  = 20 * 1024 * 1024
	maxSourceErrorSize   = 4 * 1024
)

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
	if parsed.User != nil {
		return nil, errors.New("Wodby 1 source base URL must not contain user credentials")
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("Wodby 1 source base URL must use HTTPS (plain HTTP is allowed only for loopback development)")
	}
	token, err = normalizeSourceToken(token)
	if err != nil {
		return nil, err
	}
	return &SourceClient{
		baseURL: parsed,
		token:   token,
		httpClient: &http.Client{
			Timeout: defaultSourceTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if !strings.EqualFold(req.URL.Scheme, origin.Scheme) ||
					!strings.EqualFold(req.URL.Host, origin.Host) {
					return errors.New("source API redirect to a different origin is not allowed")
				}
				return nil
			},
		},
	}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeSourceToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if len(token) >= len("token ") && strings.EqualFold(token[:len("token ")], "token ") {
		token = strings.TrimSpace(token[len("token "):])
	}
	if len(token) != 64 {
		return "", errors.New("Wodby 1 source API token must contain exactly 64 characters")
	}
	for _, char := range token {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			continue
		}
		return "", errors.New("Wodby 1 source API token contains unsupported characters")
	}
	return token, nil
}

func (c *SourceClient) ExportApp(ctx context.Context, uuid string, includeSecrets bool) (Export, error) {
	if err := validateSourceUUID(uuid); err != nil {
		return Export{}, err
	}
	return c.getExport(ctx, "app", uuid, "/api/v4/migrations/v2/apps/"+url.PathEscape(uuid)+"/export", includeSecrets)
}

func (c *SourceClient) ExportServer(ctx context.Context, uuid string, includeSecrets bool) (Export, error) {
	if err := validateSourceUUID(uuid); err != nil {
		return Export{}, err
	}
	return c.getExport(ctx, "server", uuid, "/api/v4/migrations/v2/servers/"+url.PathEscape(uuid)+"/export", includeSecrets)
}

func validateSourceUUID(uuid string) error {
	if uuid == "" || uuid != strings.TrimSpace(uuid) {
		return errors.New("Wodby 1 source UUID must be a non-empty value without surrounding whitespace")
	}
	for _, char := range uuid {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			continue
		}
		return errors.New("Wodby 1 source UUID contains unsupported characters")
	}
	return nil
}

func (c *SourceClient) getExport(ctx context.Context, kind string, uuid string, path string, includeSecrets bool) (Export, error) {
	query := url.Values{}
	if includeSecrets {
		query.Set("include_secrets", "true")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(path, query), nil)
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("X-API-Key", strings.TrimSpace(c.token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceExportSize+1))
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	if len(body) > maxSourceExportSize {
		return Export{}, errors.Errorf("source migration export exceeds the %d-byte safety limit", maxSourceExportSize)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errorBody := body
		if len(errorBody) > maxSourceErrorSize {
			errorBody = errorBody[:maxSourceErrorSize]
		}
		return Export{}, fmt.Errorf("source API request failed: %s: %s", resp.Status, strings.TrimSpace(string(errorBody)))
	}

	export, err := DecodeExport(body)
	if err != nil {
		return Export{}, errors.WithStack(err)
	}
	if err := export.ValidateSource(kind, uuid); err != nil {
		return Export{}, err
	}
	if export.SecretsIncluded != includeSecrets {
		return Export{}, errors.Errorf(
			"source migration export secrets_included=%t does not match include_secrets=%t",
			export.SecretsIncluded,
			includeSecrets,
		)
	}
	export.ResponseDigest = fmt.Sprintf("%x", sha256.Sum256(body))
	export.Digest, err = export.ContentDigest()
	if err != nil {
		return Export{}, errors.Wrap(err, "compute normalized source export digest")
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
