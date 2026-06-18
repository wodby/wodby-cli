package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var successfulStatuses = map[string]bool{
	"completed": true,
	"complete":  true,
	"success":   true,
	"succeeded": true,
	"ok":        true,
}

var failedStatuses = map[string]bool{
	"canceled":  true,
	"cancelled": true,
	"errored":   true,
	"error":     true,
	"failed":    true,
}

func waitForTask(ctx context.Context, client *rest.Client, id string, timeout time.Duration) (interface{}, error) {
	return waitForResource(ctx, client, "/tasks/"+id, timeout, "task")
}

func waitForDeployment(ctx context.Context, client *rest.Client, id string, timeout time.Duration) (interface{}, error) {
	return waitForResource(ctx, client, "/app-deployments/"+id, timeout, "deployment")
}

func waitForResource(ctx context.Context, client *rest.Client, path string, timeout time.Duration, resource string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		var result interface{}
		if err := client.Get(ctx, path, nil, &result); err != nil {
			return nil, err
		}

		rows := asRows(result)
		if len(rows) == 0 {
			return result, errors.Errorf("%s response did not include a status", resource)
		}
		status := strings.ToLower(formatValue(rows[0]["status"]))
		if successfulStatuses[status] {
			return result, nil
		}
		if failedStatuses[status] {
			return result, errors.Errorf("%s finished with status %q", resource, status)
		}

		select {
		case <-ctx.Done():
			return result, errors.Errorf("timed out waiting for %s", resource)
		case <-ticker.C:
		}
	}
}

func printTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, taskID string) error {
	var task interface{}
	if err := client.Get(ctx, "/tasks/"+taskID, nil, &task); err != nil {
		return err
	}

	stepIDs := taskStepIDs(task)
	if len(stepIDs) == 0 {
		return errors.New("task has no loggable steps")
	}

	logs := make([]interface{}, 0, len(stepIDs))
	for _, stepID := range stepIDs {
		var stepLogs interface{}
		if err := client.Get(ctx, "/task-steps/"+stepID+"/logs", url.Values{}, &stepLogs); err != nil {
			return err
		}
		logs = append(logs, map[string]interface{}{
			"stepId": stepID,
			"logs":   stepLogs,
		})
	}

	if output.output == outputJSON {
		content, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return errors.WithStack(err)
		}
		cmd.Println(string(content))
		return nil
	}

	for _, entry := range logs {
		m := entry.(map[string]interface{})
		cmd.Printf("== step %s ==\n", m["stepId"])
		lines := logLines(m["logs"])
		for _, line := range lines {
			cmd.Println(line)
		}
	}

	return nil
}

func taskStepIDs(task interface{}) []string {
	rows := asRows(task)
	if len(rows) == 0 {
		return nil
	}

	var ids []string
	jobs, ok := rows[0]["jobs"].([]interface{})
	if !ok {
		return ids
	}
	for _, job := range jobs {
		jobMap, ok := job.(map[string]interface{})
		if !ok {
			continue
		}
		steps, ok := jobMap["steps"].([]interface{})
		if !ok {
			continue
		}
		for _, step := range steps {
			stepMap, ok := step.(map[string]interface{})
			if !ok {
				continue
			}
			id := formatValue(stepMap["id"])
			if id != "" {
				ids = append(ids, id)
			}
		}
	}

	return ids
}

func logLines(value interface{}) []string {
	rows := asRows(value)
	if len(rows) == 0 {
		return nil
	}

	rawLines, ok := rows[0]["lines"].([]interface{})
	if !ok {
		return nil
	}

	lines := make([]string, 0, len(rawLines))
	for _, rawLine := range rawLines {
		line, ok := rawLine.(map[string]interface{})
		if !ok {
			continue
		}
		level := formatValue(line["level"])
		message := formatValue(line["message"])
		if level == "" {
			lines = append(lines, message)
		} else {
			lines = append(lines, fmt.Sprintf("[%s] %s", level, message))
		}
	}

	return lines
}
