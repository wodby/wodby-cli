package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/request"
)

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestGetLatestVersion(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		status     string
		body       string
		want       string
		wantErr    string
	}{
		{
			name:       "successful version",
			statusCode: http.StatusOK,
			status:     "200 OK",
			body:       " 0.1.0-beta11\n",
			want:       "0.1.0-beta11",
		},
		{
			name:       "API authorization error",
			statusCode: http.StatusUnauthorized,
			status:     "401 Unauthorized",
			body:       `{"error":{"message":"Unknown authorization token"}}`,
			wantErr:    "Unknown authorization token",
		},
		{
			name:       "non-JSON API error",
			statusCode: http.StatusBadGateway,
			status:     "502 Bad Gateway",
			body:       "upstream unavailable",
			wantErr:    "502 Bad Gateway",
		},
		{
			name:       "empty successful response",
			statusCode: http.StatusOK,
			status:     "200 OK",
			body:       " \n",
			wantErr:    "empty minimum CLI version returned by API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackingReadCloser{Reader: strings.NewReader(tt.body)}
			client := &Client{
				Client: request.Wrap(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.statusCode,
						Status:     tt.status,
						Body:       body,
					}, nil
				}),
				Config: &Config{
					Scheme: "https",
					Host:   "api.example.com",
					Prefix: "/api/v2",
				},
			}

			got, err := client.GetLatestVersion()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("GetLatestVersion() error = %v", err)
				}
				if got != tt.want {
					t.Fatalf("GetLatestVersion() = %q, want %q", got, tt.want)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("GetLatestVersion() error = %v, want %q", err, tt.wantErr)
			}

			if !body.closed {
				t.Fatal("GetLatestVersion() did not close the response body")
			}
		})
	}
}
