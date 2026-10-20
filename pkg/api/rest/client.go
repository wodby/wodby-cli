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
	Title   string       `json:"title"`
	Detail  string       `json:"detail"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Errors  []FieldError `json:"errors"`
}

type FieldError struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type APIError struct {
	StatusCode int
	Status     string
	Response   ErrorResponse
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api request failed: %s (%s)", e.Message, e.Status)
	}
	if strings.TrimSpace(e.Body) != "" {
		return fmt.Sprintf("api request failed: %s: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("api request failed: %s", e.Status)
}

func NewClient(config types.APIConfig) (*Client, error) {
	baseURL, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.Errorf("invalid api base url %q", config.Endpoint)
	}

	underlying := config.Transport
	if underlying == nil {
		underlying = http.DefaultTransport
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &transport{
				underlyingTransport: underlying,
				apiKey:              config.Key,
				accessToken:         config.AccessToken,
			},
			Timeout: defaultHTTPTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if !strings.EqualFold(req.URL.Scheme, origin.Scheme) ||
					!strings.EqualFold(req.URL.Host, origin.Host) {
					return errors.New("API redirect to a different origin is not allowed")
				}
				return nil
			},
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

// busyMessage is how Wodby 2 reports a database lock conflict. The API
// raises it only after rolling the request back, before anything is saved,
// so the same request can be sent again.
const busyMessage = "configuration is busy; retry the operation"

// busyRetryDelays are the waits before each repeat of a request that Wodby 2
// answered as busy, about a minute in all.
var busyRetryDelays = []time.Duration{
	2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second,
}

// IsBusy reports whether Wodby 2 rejected a request because the
// configuration it changes was locked by another operation.
func IsBusy(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message+" "+apiErr.Body), busyMessage)
}

func (c *Client) Do(ctx context.Context, method string, path string, query url.Values, body interface{}, out interface{}) error {
	for attempt := 0; ; attempt++ {
		err := c.do(ctx, method, path, query, body, out)
		if err == nil || !IsBusy(err) || attempt >= len(busyRetryDelays) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(busyRetryDelays[attempt]):
		}
	}
}

func (c *Client) do(ctx context.Context, method string, path string, query url.Values, body interface{}, out interface{}) error {
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
	if err := json.Unmarshal(body, &apiErr); err == nil {
		if message := apiErr.UserMessage(); message != "" {
			return &APIError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Response:   apiErr,
				Message:    message,
			}
		}
	}
	if len(bytes.TrimSpace(body)) != 0 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(body),
		}
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
	}
}

func (r ErrorResponse) UserMessage() string {
	message := firstNonEmpty(r.Message, r.Detail, r.Title)
	if len(r.Errors) == 0 {
		return message
	}

	fieldMessages := make([]string, 0, len(r.Errors))
	for _, fieldErr := range r.Errors {
		detail := firstNonEmpty(fieldErr.Detail, fieldErr.Code)
		if detail == "" {
			continue
		}
		if fieldErr.Field != "" {
			detail = fieldErr.Field + ": " + detail
		}
		fieldMessages = append(fieldMessages, detail)
	}
	if len(fieldMessages) == 0 {
		return message
	}
	if message == "" {
		return strings.Join(fieldMessages, "; ")
	}
	return message + ": " + strings.Join(fieldMessages, "; ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
