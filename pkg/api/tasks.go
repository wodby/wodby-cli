package api

import (
	"net/http"
	"time"

	"github.com/wodby/wodby-cli/pkg/types"
	"github.com/wodby/wodby-cli/pkg/utils"
	"github.com/pkg/errors"
	"fmt"
)

func (c *Client) NewGetTaskRequest(UUID string) (*http.Request, error) {
	u := c.NewURL("/tasks/%s", UUID)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

func (c *Client) GetTask(UUID string) (*types.Task, error) {
	req, err := c.NewGetTaskRequest(UUID)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}

	task := new(types.Task)
	err = c.DecodeResponse(resp, task)
	if err != nil {
		return nil, err
	}

	return task, nil
}

func (c *Client) WaitTask(UUID string) error {
	return utils.WaitFor(func() (bool, error) {
		fmt.Print(".")
		task, err := c.GetTask(UUID)
		if err != nil {
			return false, err
		}

		switch task.Status {
		case types.TaskStatusDone:
			return true, nil
		case types.TaskStatusCanceled:
			return false, errors.New("task canceled")
		case types.TaskStatusFailed:
			return false, errors.New("task failed")
		}

		return false, nil
	}, 5*time.Second, 900*time.Second)
}
