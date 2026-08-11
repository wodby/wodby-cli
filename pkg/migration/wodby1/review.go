package wodby1

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

func PrintReview(w io.Writer, plan Plan) {
	fmt.Fprintf(w, "Wodby 1 to Wodby 2 migration plan\n")
	printReviewTable(w, "  ", []string{"Field", "Value"}, reviewOverviewRows(plan))
	fmt.Fprintf(w, "\nSummary:\n")
	printReviewTable(w, "  ", []string{"Item", "Count"}, [][]string{
		{"Apps", strconv.Itoa(plan.Summary.Apps)},
		{"Instances", strconv.Itoa(plan.Summary.Instances)},
		{"Services", strconv.Itoa(plan.Summary.Services)},
		{"Routes", strconv.Itoa(plan.Summary.Routes)},
		{"Env vars", strconv.Itoa(plan.Summary.EnvVars)},
		{"Cron jobs", strconv.Itoa(plan.Summary.CronJobs)},
		{"Imports", strconv.Itoa(plan.Summary.Imports)},
		{"Blocking", strconv.Itoa(plan.Summary.Blocking)},
		{"Requires confirmation", strconv.Itoa(plan.Summary.Confirmation)},
		{"Manual follow-up", strconv.Itoa(plan.Summary.Manual)},
		{"Intentionally skipped", strconv.Itoa(plan.Summary.Intentionally)},
	})

	for _, app := range plan.Apps {
		fmt.Fprintf(w, "\nApp: %s\n", firstNonEmpty(app.Title, app.Name))
		for _, instance := range app.Instances {
			fmt.Fprintf(
				w,
				"  Instance: %s (%s)\n",
				firstNonEmpty(instance.Title, instance.Name),
				emptyDash(instance.SourceType),
			)
			if instance.BasicAuth.Enabled {
				status := "enabled"
				if instance.BasicAuth.SecretRedacted {
					status = "enabled, password redacted"
				}
				fmt.Fprintf(w, "    Basic auth: %s\n", status)
			}
			if len(instance.Services) != 0 {
				fmt.Fprintf(w, "    Services:\n")
				rows := make([][]string, 0, len(instance.Services))
				for _, service := range instance.Services {
					target := service.TargetName
					if target == "" {
						target = "-"
					}
					state := "disabled"
					if service.Enabled {
						state = "enabled"
					}
					rows = append(rows, []string{
						service.SourceName,
						target,
						state,
						service.Action,
						strconv.Itoa(service.EnvVars),
						strconv.Itoa(service.CronJobs),
					})
				}
				printReviewTable(w, "      ", []string{"Source", "Target", "State", "Action", "Env vars", "Cron jobs"}, rows)
			}
			if len(instance.Routes) != 0 {
				fmt.Fprintf(w, "    Routes:\n")
				rows := make([][]string, 0, len(instance.Routes))
				for _, route := range instance.Routes {
					flags := routeFlags(route)
					flags = emptyDash(flags)
					port := "-"
					if route.PortNumber != nil {
						port = fmt.Sprintf("%d", *route.PortNumber)
					}
					rows = append(rows, []string{
						route.Host,
						route.Action,
						emptyDash(route.Type),
						emptyDash(route.Service),
						port,
						flags,
					})
				}
				printReviewTable(w, "      ", []string{"Host", "Action", "Type", "Service", "Port", "Flags"}, rows)
			}
			if len(instance.Imports) != 0 {
				fmt.Fprintf(w, "    Data imports:\n")
				rows := make([][]string, 0, len(instance.Imports))
				for _, item := range instance.Imports {
					target := "-"
					if item.TargetService != "" || item.TargetImport != "" {
						target = emptyDash(item.TargetService) + ":" + emptyDash(item.TargetImport)
					}
					rows = append(rows, []string{
						item.Component,
						item.Action,
						target,
						strconv.FormatInt(item.BackupCreated, 10),
						strconv.FormatInt(item.Size, 10),
					})
				}
				printReviewTable(w, "      ", []string{"Component", "Action", "Target", "Backup created", "Size"}, rows)
			}
		}
	}

	sections := []struct {
		severity string
		title    string
	}{
		{SeverityBlocking, "Blocking"},
		{SeverityConfirmation, "Requires confirmation"},
		{SeverityManual, "Manual follow-up"},
		{SeveritySkipped, "Intentionally skipped"},
	}
	for _, section := range sections {
		rows := make([][]string, 0)
		for _, item := range plan.Review {
			if item.Severity != section.severity {
				continue
			}
			scope := item.App
			if item.Instance != "" {
				scope += "/" + item.Instance
			}
			if strings.TrimSpace(scope) == "" {
				scope = "-"
			}
			rows = append(rows, []string{scope, item.Subject, item.Message})
		}
		if len(rows) != 0 {
			fmt.Fprintf(w, "\n%s (%d):\n", section.title, len(rows))
			printReviewTable(w, "  ", []string{"Scope", "Subject", "Details"}, rows)
		}
	}
}

func reviewOverviewRows(plan Plan) [][]string {
	rows := make([][]string, 0)
	instanceCount := 0
	for _, app := range plan.Apps {
		instanceCount += len(app.Instances)
		for _, instance := range app.Instances {
			rows = append(rows, []string{"Source", reviewSourceLabel(app, instance)})
		}
		if len(app.Instances) == 0 {
			rows = append(rows, []string{"Source", firstNonEmpty(app.Title, app.Name)})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"Source", "Wodby 1 " + emptyDash(plan.Source.Kind)})
	}

	rows = append(rows, []string{
		"Target organization",
		firstNonEmpty(plan.Target.OrgName, plan.Target.Org),
	})
	if plan.Target.ProjectID > 0 || strings.TrimSpace(plan.Target.Project) != "" {
		project := firstNonEmpty(plan.Target.ProjectName, plan.Target.Project)
		if plan.Target.ProjectID > 0 && strings.TrimSpace(plan.Target.Project) == "" {
			project += " (cluster owner default)"
		}
		rows = append(rows, []string{
			"Target project",
			project,
		})
	}
	rows = append(rows, []string{
		"Target cluster",
		firstNonEmpty(plan.Target.ClusterName, plan.Target.Cluster),
	})
	targetCI := "Wodby CI (built-in)"
	if plan.Target.CIIntegrationID != 0 {
		targetCI = "Selected third-party CI integration"
	}
	rows = append(rows, []string{"Target CI", targetCI})

	for _, app := range plan.Apps {
		appContext := ""
		if len(plan.Apps) > 1 {
			appContext = firstNonEmpty(app.Title, app.Name) + ": "
		}
		rows = append(rows, []string{
			"Target app",
			appContext + emptyDash(app.Name),
		})
		for _, instance := range app.Instances {
			context := ""
			if instanceCount > 1 {
				context = reviewInstanceLabel(app, instance) + ": "
			}
			rows = append(
				rows,
				[]string{
					"Target instance",
					context + emptyDash(instance.Name),
				},
				[]string{
					"Environment mapping",
					context + emptyDash(instance.SourceType) + " -> " + emptyDash(instance.TargetEnv),
				},
				[]string{
					"Target stack",
					context + firstNonEmpty(instance.Stack.Target, instance.Stack.Name),
				},
			)
		}
	}
	return rows
}

func reviewSourceLabel(app AppPlan, instance InstancePlan) string {
	return fmt.Sprintf(
		"%s (%s)",
		reviewInstanceLabel(app, instance),
		emptyDash(instance.SourceType),
	)
}

func reviewInstanceLabel(app AppPlan, instance InstancePlan) string {
	return firstNonEmpty(app.Title, app.Name) + " - " + firstNonEmpty(instance.Title, instance.Name)
}

func printReviewTable(w io.Writer, indent string, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeReviewTableRow(tw, indent, headers)
	separator := make([]string, len(headers))
	for index, header := range headers {
		separator[index] = strings.Repeat("-", len(header))
	}
	writeReviewTableRow(tw, indent, separator)
	for _, row := range rows {
		writeReviewTableRow(tw, indent, row)
	}
	_ = tw.Flush()
}

func writeReviewTableRow(w io.Writer, indent string, cells []string) {
	sanitized := make([]string, len(cells))
	replacer := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	for index, cell := range cells {
		sanitized[index] = replacer.Replace(cell)
	}
	fmt.Fprintf(w, "%s%s\n", indent, strings.Join(sanitized, "\t"))
}

func verifiedLabel(verified bool) string {
	if verified {
		return "verified"
	}
	return "not verified"
}

func routeFlags(route RoutePlan) string {
	var flags []string
	if route.Primary {
		flags = append(flags, "primary")
	}
	if route.BasicAuth {
		flags = append(flags, "basicAuth")
	}
	if route.Redirect {
		flags = append(flags, "redirect")
	}
	if len(route.Settings) != 0 {
		settings := make([]string, 0, len(route.Settings))
		for _, setting := range route.Settings {
			settings = append(settings, setting.Name+"="+setting.Value)
		}
		flags = append(flags, "settings="+strings.Join(settings, ","))
	}
	if route.ReviewRequired {
		flags = append(flags, "review")
	}
	return strings.Join(flags, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "-"
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
