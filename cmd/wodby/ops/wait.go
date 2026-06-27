package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"
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

func printTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, taskID string, jobFilter string, allJobs bool) error {
	if jobFilter != "" && allJobs {
		return errors.New("use either --job or --all-jobs, not both")
	}

	var task interface{}
	if err := client.Get(ctx, "/tasks/"+taskID, nil, &task); err != nil {
		return err
	}

	jobs := taskLogJobs(task)
	if len(jobs) == 0 {
		printNoLogs(cmd, output)
		return nil
	}

	selectedJobs := jobs
	if jobFilter != "" {
		var ok bool
		selectedJobs, ok = filterTaskLogJobs(jobs, jobFilter)
		if !ok {
			return errors.Errorf("job %q not found; available jobs: %s", jobFilter, availableTaskLogJobs(jobs))
		}
	} else if len(jobs) > 1 && !allJobs {
		printTaskJobSummary(cmd, output, jobs)
		return nil
	}

	logs, err := fetchTaskJobLogs(ctx, client, selectedJobs)
	if err != nil {
		return err
	}
	if output.output == outputJSON {
		content, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return errors.WithStack(err)
		}
		cmd.Println(string(content))
		return nil
	}

	showJobHeaders := len(selectedJobs) > 1 || jobFilter != "" || allJobs
	for jobIndex, entry := range logs {
		if jobIndex > 0 {
			cmd.Println()
		}
		m := entry.(map[string]interface{})
		if showJobHeaders {
			cmd.Printf("== job %s ==\n", jobLogTitleFromEntry(m))
		}
		stepLogs, ok := m["steps"].([]interface{})
		if !ok || len(stepLogs) == 0 {
			cmd.Println("no logs")
			continue
		}
		for stepIndex, stepEntry := range stepLogs {
			stepMap, ok := stepEntry.(map[string]interface{})
			if !ok {
				continue
			}
			if stepIndex > 0 {
				cmd.Println()
			}
			cmd.Printf("== %s ==\n", stepLogTitle(stepMap))
			lines := logLines(stepMap["logs"])
			if len(lines) == 0 {
				cmd.Println("no logs")
				continue
			}
			for _, line := range lines {
				cmd.Println(line)
			}
		}
	}

	return nil
}

func printNoLogs(cmd *cobra.Command, output outputOptions) {
	if output.output == outputJSON {
		cmd.Println("[]")
		return
	}
	cmd.Println("no logs")
}

func fetchTaskJobLogs(ctx context.Context, client *rest.Client, jobs []taskLogJob) ([]interface{}, error) {
	logs := make([]interface{}, 0, len(jobs))
	for _, job := range jobs {
		stepLogs := make([]interface{}, 0, len(job.steps))
		for _, step := range job.steps {
			var logsValue interface{}
			if step.id != "" {
				if err := client.Get(ctx, "/task-steps/"+step.id+"/logs", url.Values{}, &logsValue); err != nil {
					return nil, err
				}
			}
			stepLogs = append(stepLogs, map[string]interface{}{
				"stepId":   step.id,
				"stepName": step.name,
				"status":   step.status,
				"duration": step.duration,
				"logs":     logsValue,
			})
		}
		logs = append(logs, map[string]interface{}{
			"jobId":    job.id,
			"jobName":  job.name,
			"status":   job.status,
			"duration": job.duration,
			"steps":    stepLogs,
		})
	}
	return logs, nil
}

func printTaskJobSummary(cmd *cobra.Command, output outputOptions, jobs []taskLogJob) {
	message := fmt.Sprintf("task has %d jobs; pass --job to show logs, or --all-jobs to show everything", len(jobs))
	if output.output == outputJSON {
		content, err := json.MarshalIndent(map[string]interface{}{
			"message": message,
			"jobs":    taskJobSummaries(jobs),
		}, "", "  ")
		if err != nil {
			cmd.Println(message)
			return
		}
		cmd.Println(string(content))
		return
	}

	cmd.Println(message)
	cmd.Println()
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "id\tname\tstatus\tduration\tsteps")
	for _, job := range jobs {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\n", job.id, jobLogNameOrFallback(job), job.status, job.duration, len(job.steps))
	}
	_ = writer.Flush()
}

func taskJobSummaries(jobs []taskLogJob) []map[string]interface{} {
	summaries := make([]map[string]interface{}, 0, len(jobs))
	for _, job := range jobs {
		summaries = append(summaries, map[string]interface{}{
			"id":       job.id,
			"name":     jobLogNameOrFallback(job),
			"status":   job.status,
			"duration": job.duration,
			"steps":    taskStepSummaries(job.steps),
		})
	}
	return summaries
}

func taskStepSummaries(steps []taskLogStep) []map[string]interface{} {
	summaries := make([]map[string]interface{}, 0, len(steps))
	for _, step := range steps {
		summaries = append(summaries, map[string]interface{}{
			"id":       step.id,
			"name":     stepLogNameOrFallback(step),
			"status":   step.status,
			"duration": step.duration,
		})
	}
	return summaries
}

func filterTaskLogJobs(jobs []taskLogJob, filter string) ([]taskLogJob, bool) {
	filter = normalizeDisplayToken(filter)
	for _, job := range jobs {
		if normalizeDisplayToken(job.id) == filter || normalizeDisplayToken(job.name) == filter || normalizeDisplayToken(jobLogNameOrFallback(job)) == filter {
			return []taskLogJob{job}, true
		}
	}
	return nil, false
}

func availableTaskLogJobs(jobs []taskLogJob) string {
	labels := make([]string, 0, len(jobs))
	for _, job := range jobs {
		label := jobLogNameOrFallback(job)
		if job.id != "" && job.id != label {
			label += " (" + job.id + ")"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, ", ")
}

func formatTaskJobsColumn(row map[string]interface{}) string {
	jobs := taskLogJobs(row)
	if len(jobs) == 0 {
		return ""
	}

	lines := make([]string, 0)
	for _, job := range jobs {
		details := compactNonEmpty(job.status, job.duration, fmt.Sprintf("%d steps", len(job.steps)))
		line := jobLogNameWithID(job)
		if len(details) != 0 {
			line += " (" + strings.Join(details, ", ") + ")"
		}
		lines = append(lines, line)
		for _, step := range job.steps {
			stepDetails := compactNonEmpty(step.status, step.duration)
			stepLine := "  - " + stepLogNameWithID(step)
			if len(stepDetails) != 0 {
				stepLine += " (" + strings.Join(stepDetails, ", ") + ")"
			}
			lines = append(lines, stepLine)
		}
	}
	return strings.Join(lines, "\n")
}

func compactNonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

type taskLogJob struct {
	id       string
	name     string
	status   string
	duration string
	index    int
	steps    []taskLogStep
}

type taskLogStep struct {
	id       string
	name     string
	status   string
	duration string
}

func taskLogJobs(task interface{}) []taskLogJob {
	rows := responseRows(task)
	if len(rows) == 0 {
		return nil
	}

	row := rows[0]
	jobs := make([]taskLogJob, 0)
	if topLevelSteps := taskLogStepsFromValue(row["steps"]); len(topLevelSteps) != 0 {
		jobs = append(jobs, taskLogJob{
			index:    1,
			name:     stepLogNameFromRow(row),
			status:   firstScalarPath(row, "status"),
			duration: stepLogDuration(row),
			steps:    topLevelSteps,
		})
	}

	rawJobs, ok := row["jobs"].([]interface{})
	if !ok {
		return jobs
	}
	for _, rawJob := range rawJobs {
		jobMap, ok := rawJob.(map[string]interface{})
		if !ok {
			continue
		}
		index := len(jobs) + 1
		jobs = append(jobs, taskLogJob{
			id:       formatValue(jobMap["id"]),
			name:     jobLogName(jobMap),
			status:   firstScalarPath(jobMap, "status"),
			duration: stepLogDuration(jobMap),
			index:    index,
			steps:    taskLogStepsFromValue(jobMap["steps"]),
		})
	}
	return jobs
}

func taskLogSteps(task interface{}) []taskLogStep {
	jobs := taskLogJobs(task)
	var steps []taskLogStep
	for _, job := range jobs {
		steps = append(steps, job.steps...)
	}
	return steps
}

func taskLogStepsFromValue(value interface{}) []taskLogStep {
	rawSteps, ok := value.([]interface{})
	if !ok {
		return nil
	}
	steps := make([]taskLogStep, 0, len(rawSteps))
	for _, step := range rawSteps {
		stepMap, ok := step.(map[string]interface{})
		if !ok {
			continue
		}
		id := formatValue(stepMap["id"])
		name := stepLogName(stepMap)
		if id == "" && name == "" {
			continue
		}
		steps = append(steps, taskLogStep{
			id:       id,
			name:     name,
			status:   firstScalarPath(stepMap, "status"),
			duration: stepLogDuration(stepMap),
		})
	}
	return steps
}

func jobLogName(job map[string]interface{}) string {
	return firstTitlePath(job, "title", "name", "jobTitle", "jobName", "type")
}

func stepLogNameFromRow(row map[string]interface{}) string {
	return firstTitlePath(row, "title", "name", "taskTitle", "taskName", "type")
}

func jobLogNameOrFallback(job taskLogJob) string {
	if job.name != "" {
		return job.name
	}
	if job.id != "" {
		return "job " + job.id
	}
	return fmt.Sprintf("job %d", job.index)
}

func jobLogNameWithID(job taskLogJob) string {
	name := jobLogNameOrFallback(job)
	if job.id != "" && !strings.Contains(name, job.id) {
		return name + " [" + job.id + "]"
	}
	return name
}

func stepLogNameOrFallback(step taskLogStep) string {
	if step.name != "" {
		return step.name
	}
	if step.id != "" {
		return "step " + step.id
	}
	return "step"
}

func stepLogNameWithID(step taskLogStep) string {
	name := stepLogNameOrFallback(step)
	if step.id != "" && !strings.Contains(name, step.id) {
		return name + " [" + step.id + "]"
	}
	return name
}

func jobLogTitleFromEntry(entry map[string]interface{}) string {
	job := taskLogJob{
		id:       formatValue(entry["jobId"]),
		name:     formatValue(entry["jobName"]),
		status:   formatValue(entry["status"]),
		duration: formatValue(entry["duration"]),
	}
	title := jobLogNameWithID(job)
	if job.duration != "" {
		title += " (" + job.duration + ")"
	}
	return title
}

func stepLogTitle(entry map[string]interface{}) string {
	step := taskLogStep{
		id:       formatValue(entry["stepId"]),
		name:     formatValue(entry["stepName"]),
		duration: formatValue(entry["duration"]),
	}
	title := stepLogNameOrFallback(step)
	if step.duration != "" {
		title += " (" + step.duration + ")"
	}
	return title
}

func stepLogName(step map[string]interface{}) string {
	return firstTitlePath(step, "title", "name", "stepTitle", "stepName", "action", "type")
}

func stepLogDuration(step map[string]interface{}) string {
	if duration := durationFromNumericPath(step, time.Second, "durationSeconds", "durationSecond", "durationSec", "durationSecs"); duration != "" {
		return duration
	}
	if duration := durationFromNumericPath(step, time.Millisecond, "durationMs", "durationMS", "durationMilliseconds", "durationMillis"); duration != "" {
		return duration
	}
	if value := firstScalarPath(step, "duration"); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return formatDisplayDuration(duration)
		}
		return value
	}
	return formatDurationColumn(step)
}

func durationFromNumericPath(row map[string]interface{}, unit time.Duration, paths ...string) string {
	for _, path := range paths {
		value, ok := numericPathValue(row, path)
		if ok {
			return formatDisplayDuration(time.Duration(value * float64(unit)))
		}
	}
	return ""
}

func numericPathValue(row map[string]interface{}, path string) (float64, bool) {
	switch v := valueAtPath(row, path).(type) {
	case json.Number:
		value, err := v.Float64()
		return value, err == nil
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
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
