package request

import (
	"log"
	"net/http"
	"strings"
)

// Client sends http.Requests and returns http.Responses
// or errors in case of failure.
type Client interface {
	Do(*http.Request) (*http.Response, error)
}

// Wrap is a function type that implements the Client interface.
type Wrap func(*http.Request) (*http.Response, error)

// Do wraps original client's Do method.
func (f Wrap) Do(r *http.Request) (*http.Response, error) {
	return f(r)
}

// A Decorator wraps a Client with extra behaviour.
type Decorator func(Client) Client

// Decorate decorates a Client c with all the given Decorators, in order.
func Decorate(c Client, ds ...Decorator) Client {
	decorated := c
	for _, decorate := range ds {
		decorated = decorate(decorated)
	}
	return decorated
}

// Logging returns a Decorator that logs a Client's requests.
func Logging(l *log.Logger) Decorator {
	return func(c Client) Client {
		if l == nil {
			return c
		}

		return Wrap(func(r *http.Request) (*http.Response, error) {
			l.Printf("%s %s", r.Method, r.URL)

			return c.Do(r)
		})
	}
}

// Header returns a Decorator that adds the given HTTP header to every request
// done by a Client.
func Header(name, value string) Decorator {
	return func(c Client) Client {
		return Wrap(func(r *http.Request) (*http.Response, error) {
			r.Header.Add(name, value)
			return c.Do(r)
		})
	}
}

// Authorization returns a Decorator that authorizes every Client request
// with the given token.
func Authorization(token string) Decorator {
	return Header("Authorization", strings.Join([]string{"token", token}, " "))
}

// ContentType returns a Decorator that adds Content-Type to every Client request.
func ContentType(ct string) Decorator {
	return Header("Content-Type", ct)
}

// UserAgent returns a Decorator that adds User-Agent to every Client request.
func UserAgent(ua string) Decorator {
	return Header("User-Agent", ua)
}

// NewClient makes new HTTP client.
func NewClient(logger *log.Logger, token string) Client {
	client := Decorate(http.DefaultClient,
		Logging(logger),
		Authorization(token),
		ContentType("application/json"),
		UserAgent("wodby-cli"),
	)

	return client
}
