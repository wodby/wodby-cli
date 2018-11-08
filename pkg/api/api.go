package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/request"
	"github.com/wodby/wodby-cli/pkg/types"
)

// Client is Wodby API client.
type Client struct {
	Client request.Client
	Config *Config
}

// Config is config for Wodby API client.
type Config struct {
	Key    string `json:"key"`
	Scheme string `json:"proto"`
	Host   string `json:"host"`
	Prefix string `json:"prefix"`
}

// ResTask represents api request result with a task.
type ResTask struct {
	Task struct {
		UUID string
	}
}

// NewPath makes new Path.
func (c *Client) NewPath(format string, params ...interface{}) string {
	return strings.Join([]string{c.Config.Prefix, fmt.Sprintf(format, params...)}, "")
}

// NewURL makes new URL.
func (c *Client) NewURL(format string, params ...interface{}) *url.URL {
	return &url.URL{Host: c.Config.Host, Scheme: c.Config.Scheme, Path: c.NewPath(format, params...)}
}

// EncodePayload encodes the payload.
func (c *Client) EncodePayload(payload interface{}) (io.Reader, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return bytes.NewBuffer(b), nil
}

// DecodeResponse decodes the response.
func (c *Client) DecodeResponse(resp *http.Response, result interface{}) error {
	defer resp.Body.Close()

	str, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.New("response body reading failed")
	}

	if viper.GetBool("dump") {
		fmt.Println(string(str))
	}

	if resp.StatusCode != http.StatusOK {
		errResp := new(types.ErrorResponse)
		err = json.Unmarshal(str, errResp)

		if err != nil {
			return errors.New(resp.Status)
		} else {
			return errors.New(errResp.Error.Message)
		}
	}

	err = json.Unmarshal(str, result)
	if err != nil {
		return errors.New("response body parsing failed")
	}

	return nil
}

// NewClient makes new Wodby API client.
func NewClient(logger *log.Logger, config *Config) *Client {
	return &Client{
		Client: request.NewClient(logger, config.Key),
		Config: config,
	}
}

// Body is helper structure for determine status of request.
// type Body struct {
// 	ResultRaw json.RawMessage
// 	ErrorRaw  json.RawMessage
// }

// Error is structure for API errors.
// type Error struct {
// 	Class   string
// 	Message string
// 	Code    string
// }

// HandleResponse handles the response.
// func (c *Client) HandleResponse(resp *http.Response, result interface{}) error {
// 	defer resp.Body.Close()

// 	bodyRaw, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		return errors.New("response body reading failed")
// 	}

// 	if viper.GetBool("dump") {
// 		fmt.Println(string(bodyRaw))
// 	}

// 	body := new(Body)
// 	err = json.Unmarshal(bodyRaw, &body)
// 	if err != nil {
// 		return errors.New("response body parsing failed")
// 	}

// 	if string(body.ResultRaw) != "" {
// 		err = json.Unmarshal(body.ResultRaw, result)
// 		if err != nil {
// 			return errors.New("API result parsing failed")
// 		}
// 	} else if string(body.ErrorRaw) != "" {
// 		respErr := new(Error)
// 		err = json.Unmarshal(body.ErrorRaw, &respErr)
// 		if err != nil {
// 			return errors.New("API error parsing failed")
// 		}
// 		return errors.New(respErr.Message)
// 	} else {
// 		return errors.New("response body has wrong format")
// 	}

// 	return nil
// }
