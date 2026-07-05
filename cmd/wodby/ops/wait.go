package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var successfulStatuses = map[string]bool{
	"completed":          true,
	"complete":           true,
	"done":               true,
	"done-with-warnings": true,
	"done with warnings": true,
	"done_with_warnings": true,
	"success":            true,
	"succeeded":          true,
	"ok":                 true,
}

var failedStatuses = map[string]bool{
	"backed off": true,
	"backed-off": true,
	"backed_off": true,
	"canceled":   true,
	"cancelled":  true,
	"errored":    true,
	"error":      true,
	"failed":     true,
	"timed out":  true,
	"timed-out":  true,
	"timed_out":  true,
	"timeout":    true,
	"timedout":   true,
}

const defaultTaskLogStreamTimeout = 10 * time.Minute

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

func printOperationTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, value interface{}) (bool, error) {
	if outputFormat(cmd, output) == outputJSON {
		return false, nil
	}
	taskID := firstTaskID(value)
	if taskID == "" {
		return false, nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Operation started. Streaming task logs for task %s.\n\n", taskID)
	if err := streamTaskLogs(ctx, cmd, client, taskID, defaultTaskLogStreamTimeout); err != nil {
		return true, err
	}
	return true, nil
}

func printCreatedResourceTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, value interface{}, resource string, taskQueryName string) (bool, error) {
	if outputFormat(cmd, output) == outputJSON {
		return false, nil
	}

	resourceID := firstCreatedResourceID(value)
	taskID := firstTaskID(value)
	if taskID == "" && resourceID != "" {
		var err error
		taskID, err = fetchReferencedTaskID(ctx, client, taskQueryName, resourceID)
		if err != nil {
			return false, err
		}
	}
	return printResolvedCreatedResourceTaskLogs(ctx, cmd, client, resource, resourceID, taskID)
}

func printAppCreateTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, value interface{}) (bool, error) {
	if outputFormat(cmd, output) == outputJSON {
		return false, nil
	}

	appID := firstCreatedResourceID(value)
	instanceID := ""
	taskID := firstTaskID(value)
	if taskID == "" && appID != "" {
		var err error
		taskID, err = fetchReferencedTaskID(ctx, client, "appId", appID)
		if err != nil {
			return false, err
		}
	}
	if taskID == "" && appID != "" {
		var err error
		instanceID, err = createdAppInstanceID(ctx, client, value, appID)
		if err != nil {
			return false, err
		}
		if instanceID != "" {
			taskID, err = fetchReferencedTaskID(ctx, client, "appInstanceId", instanceID)
			if err != nil {
				return false, err
			}
		}
	}

	handled, err := printResolvedCreatedResourceTaskLogs(ctx, cmd, client, "app", appID, taskID)
	if err != nil || !handled {
		return handled, err
	}
	if instanceID == "" && appID != "" {
		instanceID, err = createdAppInstanceID(ctx, client, value, appID)
		if err != nil {
			return true, err
		}
	}
	if err := streamCreatedAppInstanceFollowUpTask(ctx, cmd, client, instanceID, taskID); err != nil {
		return true, err
	}
	return true, nil
}

func printAppInstanceCreateTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, output outputOptions, value interface{}) (bool, error) {
	if outputFormat(cmd, output) == outputJSON {
		return false, nil
	}

	instanceID := firstCreatedResourceID(value)
	taskID := firstTaskID(value)
	if taskID == "" && instanceID != "" {
		var err error
		taskID, err = fetchReferencedTaskID(ctx, client, "appInstanceId", instanceID)
		if err != nil {
			return false, err
		}
	}

	handled, err := printResolvedCreatedResourceTaskLogs(ctx, cmd, client, "app instance", instanceID, taskID)
	if err != nil || !handled {
		return handled, err
	}
	if err := streamCreatedAppInstanceFollowUpTask(ctx, cmd, client, instanceID, taskID); err != nil {
		return true, err
	}
	return true, nil
}

func printResolvedCreatedResourceTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, resource string, resourceID string, taskID string) (bool, error) {
	if taskID == "" {
		return false, nil
	}

	if resourceID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s creation started. Streaming task logs for %s %s (task %s).\n\n", humanizeColumnTitle(resource), resource, resourceID, taskID)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s creation started. Streaming task logs for task %s.\n\n", humanizeColumnTitle(resource), taskID)
	}
	if err := streamTaskLogs(ctx, cmd, client, taskID, defaultTaskLogStreamTimeout); err != nil {
		return true, err
	}
	return true, nil
}

func firstCreatedResourceID(value interface{}) string {
	rows := responseRows(value)
	if len(rows) == 0 {
		return ""
	}
	return firstScalarPath(rows[0], "id")
}

func createdAppInstanceID(ctx context.Context, client *rest.Client, value interface{}, appID string) (string, error) {
	rows := responseRows(value)
	if len(rows) == 0 {
		return "", nil
	}

	app := rows[0]
	if id := embeddedAppInstanceID(app); id != "" {
		return id, nil
	}

	orgID := firstScalarPath(app, "orgId", "organizationId", "org.id", "organization.id")
	if orgID == "" {
		return "", nil
	}

	query := url.Values{
		"appId":    []string{appID},
		"orgId":    []string{orgID},
		"pageSize": []string{"1"},
	}
	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return "", err
	}

	instanceRows := responseRows(result)
	if len(instanceRows) == 0 {
		return "", nil
	}
	return firstScalarPath(instanceRows[0], "id", "appInstanceId", "appInstance.id"), nil
}

func embeddedAppInstanceID(app map[string]interface{}) string {
	for _, path := range []string{
		"appInstanceId",
		"instanceId",
		"initialAppInstanceId",
		"initialInstanceId",
		"appInstance.id",
		"instance.id",
		"initialAppInstance.id",
		"initialInstance.id",
	} {
		if id := firstScalarPath(app, path); id != "" {
			return id
		}
	}

	for _, path := range []string{"appInstances", "instances"} {
		rows := responseRows(valueAtPath(app, path))
		if len(rows) == 0 {
			continue
		}
		if id := firstScalarPath(rows[0], "id", "appInstanceId", "appInstance.id"); id != "" {
			return id
		}
	}

	return ""
}

func streamCreatedAppInstanceFollowUpTask(ctx context.Context, cmd *cobra.Command, client *rest.Client, instanceID string, previousTaskID string) error {
	if instanceID == "" {
		return nil
	}

	buildID, taskID, err := latestAppInstanceOperationTask(ctx, client, "/app-builds", instanceID)
	if err != nil {
		return err
	}
	if taskID != "" && taskID != previousTaskID {
		fmt.Fprintf(cmd.OutOrStdout(), "\nBuild started. Streaming task logs for build %s (task %s).\n\n", buildID, taskID)
		return streamTaskLogs(ctx, cmd, client, taskID, defaultTaskLogStreamTimeout)
	}

	deploymentID, taskID, err := latestAppInstanceOperationTask(ctx, client, "/app-deployments", instanceID)
	if err != nil {
		return err
	}
	if taskID != "" && taskID != previousTaskID {
		fmt.Fprintf(cmd.OutOrStdout(), "\nDeployment started. Streaming task logs for deployment %s (task %s).\n\n", deploymentID, taskID)
		return streamTaskLogs(ctx, cmd, client, taskID, defaultTaskLogStreamTimeout)
	}

	return nil
}

func latestAppInstanceOperationTask(ctx context.Context, client *rest.Client, path string, instanceID string) (string, string, error) {
	rows, err := fetchRows(ctx, client, path, url.Values{
		"appInstanceId": []string{instanceID},
		"pageSize":      []string{"1"},
	})
	if err != nil {
		return "", "", err
	}
	row := latestByTime(rows)
	if row == nil {
		return "", "", nil
	}

	resourceID := firstScalarPath(row, "id")
	taskID := firstScalarPath(row, "taskId", "task.id", "task")
	return resourceID, taskID, nil
}

func fetchReferencedTaskID(ctx context.Context, client *rest.Client, queryName string, id string) (string, error) {
	query := url.Values{
		queryName:  []string{id},
		"pageSize": []string{"10"},
	}
	var result interface{}
	if err := client.Get(ctx, "/tasks", query, &result); err != nil {
		return "", err
	}

	rows := responseRows(result)
	if len(rows) == 0 {
		return "", nil
	}
	for _, row := range rows {
		taskID := firstScalarPath(row, "id", "taskId", "task.id")
		if taskID == "" {
			continue
		}
		status := strings.ToLower(firstScalarPath(row, "status"))
		if !successfulStatuses[status] && !failedStatuses[status] {
			return taskID, nil
		}
	}
	for _, row := range rows {
		if taskID := firstScalarPath(row, "id", "taskId", "task.id"); taskID != "" {
			return taskID, nil
		}
	}
	return "", nil
}

func streamTaskLogs(ctx context.Context, cmd *cobra.Command, client *rest.Client, taskID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	seen := map[string]int{}
	printedLogs := false
	for {
		var task interface{}
		if err := client.Get(ctx, "/tasks/"+taskID, nil, &task); err != nil {
			return err
		}

		printed, err := printNewTaskLogLines(ctx, cmd, client, task, seen)
		if err != nil {
			return err
		}
		printedLogs = printedLogs || printed

		status := taskStatus(task)
		if successfulStatuses[status] {
			if !printedLogs {
				fmt.Fprintln(cmd.OutOrStdout(), "no logs")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Task completed.")
			return nil
		}
		if failedStatuses[status] {
			if !printedLogs {
				fmt.Fprintln(cmd.OutOrStdout(), "no logs")
			}
			return errors.Errorf("task finished with status %q", status)
		}

		select {
		case <-ctx.Done():
			return errors.New("timed out streaming task logs")
		case <-ticker.C:
		}
	}
}

func taskStatus(task interface{}) string {
	rows := responseRows(task)
	if len(rows) == 0 {
		return ""
	}
	return strings.ToLower(formatValue(rows[0]["status"]))
}

func printNewTaskLogLines(ctx context.Context, cmd *cobra.Command, client *rest.Client, task interface{}, seen map[string]int) (bool, error) {
	jobs := taskLogJobs(task)
	printedAny := false
	for _, job := range jobs {
		for _, step := range job.steps {
			if step.id == "" {
				continue
			}
			logsValue, err := fetchTaskStepLogs(ctx, client, step.id)
			if err != nil {
				if isTaskStepLogStreamEndedError(err) {
					continue
				}
				return false, err
			}
			lines := logLines(logsValue)
			key := taskStreamStepKey(job, step)
			start := seen[key]
			if start >= len(lines) {
				continue
			}
			if start == 0 {
				if printedAny || streamHasPrinted(seen) {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				fmt.Fprintf(cmd.OutOrStdout(), "== %s ==\n", taskStreamStepTitle(job, step, len(jobs) > 1))
			}
			for _, line := range lines[start:] {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			seen[key] = len(lines)
			printedAny = true
		}
	}
	return printedAny, nil
}

func isTaskStepLogStreamEndedError(err error) bool {
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	message := apiErr.Message
	if strings.TrimSpace(message) == "" {
		message = apiErr.Response.UserMessage()
	}
	if strings.TrimSpace(message) == "" {
		message = apiErr.Body
	}
	message = strings.ToLower(strings.TrimSpace(message))
	for _, pattern := range []string{
		"failed to start stream: no such stream",
		"log stream expired",
		"stream expired",
		"stream ended",
		"stream closed",
		"stream is closed",
		"no such stream",
	} {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

func streamHasPrinted(seen map[string]int) bool {
	for _, count := range seen {
		if count != 0 {
			return true
		}
	}
	return false
}

func taskStreamStepKey(job taskLogJob, step taskLogStep) string {
	if step.id != "" {
		return step.id
	}
	return fmt.Sprintf("%d/%s", job.index, stepLogNameOrFallback(step))
}

func taskStreamStepTitle(job taskLogJob, step taskLogStep, includeJob bool) string {
	title := stepLogNameOrFallback(step)
	if includeJob {
		title = jobLogNameOrFallback(job) + " / " + title
	}
	return logTitleWithDetails(title, step.status, step.duration)
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
	if outputFormat(cmd, output) == outputJSON {
		content, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return errors.WithStack(err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(content))
		return nil
	}

	showJobHeaders := len(selectedJobs) > 1 || jobFilter != "" || allJobs
	for jobIndex, entry := range logs {
		if jobIndex > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		m := entry.(map[string]interface{})
		if showJobHeaders {
			fmt.Fprintf(cmd.OutOrStdout(), "== job %s ==\n", jobLogTitleFromEntry(m))
		}
		stepLogs, ok := m["steps"].([]interface{})
		if !ok || len(stepLogs) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no logs")
			continue
		}
		for stepIndex, stepEntry := range stepLogs {
			stepMap, ok := stepEntry.(map[string]interface{})
			if !ok {
				continue
			}
			if stepIndex > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "== %s ==\n", stepLogTitle(stepMap))
			lines := logLines(stepMap["logs"])
			if len(lines) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no logs")
				continue
			}
			for _, line := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
		}
	}

	return nil
}

func printNoLogs(cmd *cobra.Command, output outputOptions) {
	if outputFormat(cmd, output) == outputJSON {
		fmt.Fprintln(cmd.OutOrStdout(), "[]")
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "no logs")
}

func fetchTaskJobLogs(ctx context.Context, client *rest.Client, jobs []taskLogJob) ([]interface{}, error) {
	logs := make([]interface{}, 0, len(jobs))
	for _, job := range jobs {
		stepLogs := make([]interface{}, 0, len(job.steps))
		for _, step := range job.steps {
			var logsValue interface{}
			if step.id != "" {
				var err error
				logsValue, err = fetchTaskStepLogs(ctx, client, step.id)
				if err != nil {
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

func fetchTaskStepLogs(ctx context.Context, client *rest.Client, stepID string) (interface{}, error) {
	query := url.Values{"delivery": []string{"auto"}}
	var result interface{}
	if err := client.Get(ctx, "/task-steps/"+stepID+"/logs", query, &result); err != nil {
		return nil, err
	}

	response, ok := result.(map[string]interface{})
	if !ok {
		return result, nil
	}
	signedURL := firstScalarPath(response, "url")
	if signedURL == "" {
		return result, nil
	}

	content, err := downloadSignedLogURL(ctx, signedURL)
	if err != nil {
		return nil, err
	}
	downloaded := make(map[string]interface{}, len(response)+1)
	for key, value := range response {
		if key == "url" {
			continue
		}
		downloaded[key] = value
	}
	downloaded["lines"] = logLinesFromText(content)
	return downloaded, nil
}

func downloadSignedLogURL(ctx context.Context, signedURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "download log object")
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", errors.Errorf("download log object returned status %d", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.WithStack(err)
	}
	return string(content), nil
}

func printTaskJobSummary(cmd *cobra.Command, output outputOptions, jobs []taskLogJob) {
	message := fmt.Sprintf("task has %d jobs; pass --job to show logs, or --all-jobs to show everything", len(jobs))
	if outputFormat(cmd, output) == outputJSON {
		content, err := json.MarshalIndent(map[string]interface{}{
			"message": message,
			"jobs":    taskJobSummaries(jobs),
		}, "", "  ")
		if err != nil {
			fmt.Fprintln(cmd.OutOrStdout(), message)
			return
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(content))
		return
	}

	fmt.Fprintln(cmd.OutOrStdout(), message)
	fmt.Fprintln(cmd.OutOrStdout())
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
	id        string
	name      string
	status    string
	logStatus string
	system    string
	startedAt string
	duration  string
	index     int
	steps     []taskLogStep
}

type taskLogStep struct {
	id        string
	name      string
	status    string
	logStatus string
	system    string
	startedAt string
	duration  string
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
			index:     1,
			name:      stepLogNameFromRow(row),
			status:    firstScalarPath(row, "status"),
			logStatus: firstScalarPath(row, "logStatus", "logsStatus"),
			system:    firstScalarPath(row, "system"),
			startedAt: firstScalarPath(row, "startedAt"),
			duration:  stepLogDuration(row),
			steps:     topLevelSteps,
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
			id:        formatValue(jobMap["id"]),
			name:      jobLogName(jobMap),
			status:    firstScalarPath(jobMap, "status"),
			logStatus: firstScalarPath(jobMap, "logStatus", "logsStatus"),
			system:    firstScalarPath(jobMap, "system"),
			startedAt: firstScalarPath(jobMap, "startedAt"),
			duration:  stepLogDuration(jobMap),
			index:     index,
			steps:     taskLogStepsFromValue(jobMap["steps"]),
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
			id:        id,
			name:      name,
			status:    firstScalarPath(stepMap, "status"),
			logStatus: firstScalarPath(stepMap, "logStatus", "logsStatus"),
			system:    firstScalarPath(stepMap, "system"),
			startedAt: firstScalarPath(stepMap, "startedAt"),
			duration:  stepLogDuration(stepMap),
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
	return logTitleWithDetails(jobLogNameWithID(job), job.status, job.duration)
}

func stepLogTitle(entry map[string]interface{}) string {
	step := taskLogStep{
		id:       formatValue(entry["stepId"]),
		name:     formatValue(entry["stepName"]),
		status:   formatValue(entry["status"]),
		duration: formatValue(entry["duration"]),
	}
	return logTitleWithDetails(stepLogNameOrFallback(step), step.status, step.duration)
}

func logTitleWithDetails(title string, details ...string) string {
	parts := compactNonEmpty(details...)
	if len(parts) != 0 {
		return title + " (" + strings.Join(parts, ", ") + ")"
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
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return logLinesFromText(v)
	case []string:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, logLinesFromText(item)...)
		}
		return lines
	case []interface{}:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, logLines(item)...)
		}
		return lines
	case []map[string]interface{}:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, logLines(item)...)
		}
		return lines
	case map[string]interface{}:
		for _, key := range []string{"lines", "logs", "items", "results", "item", "result", "data", "messages", "entries"} {
			if nested, ok := v[key]; ok {
				return logLines(nested)
			}
		}
		if line := logLine(v); line != "" {
			return []string{line}
		}
		return nil
	default:
		return logLinesFromText(formatValue(v))
	}
}

func logLinesFromText(value string) []string {
	if lines := logLinesFromJSONText(value); len(lines) != 0 {
		return lines
	}
	return splitLogText(value)
}

func logLinesFromJSONText(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	if decoded, ok := decodeJSONString(trimmed); ok {
		if lines := logLinesFromDecodedJSON(decoded); len(lines) != 0 {
			return lines
		}
	}

	rawLines := splitLogText(value)
	lines := make([]string, 0, len(rawLines))
	decodedAny := false
	for _, rawLine := range rawLines {
		decoded, ok := decodeJSONString(strings.TrimSpace(rawLine))
		if !ok {
			return nil
		}
		lineValues := logLinesFromDecodedJSON(decoded)
		if len(lineValues) != 0 {
			decodedAny = true
			lines = append(lines, lineValues...)
		}
	}
	if !decodedAny {
		return nil
	}
	return lines
}

func logLinesFromDecodedJSON(value interface{}) []string {
	switch value.(type) {
	case map[string]interface{}, []interface{}, []map[string]interface{}:
		return logLines(value)
	default:
		return splitLogText(formatValue(value))
	}
}

func decodeJSONString(value string) (interface{}, bool) {
	var decoded interface{}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return decoded, true
}

func splitLogText(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.Split(value, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func logLine(line map[string]interface{}) string {
	message := firstScalarPath(line, "message", "msg", "text", "line", "content", "log")
	if message == "" {
		return ""
	}
	message = strings.TrimRight(message, "\r\n")
	level := firstScalarPath(line, "level", "severity", "stream")
	timestamp := firstScalarPath(line, "timestamp", "time", "ts", "createdAt")
	labels := compactNonEmpty(timestamp, level)
	if len(labels) != 0 {
		return fmt.Sprintf("[%s] %s", strings.Join(labels, "] ["), message)
	}
	return message
}
