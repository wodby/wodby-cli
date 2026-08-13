package wodby1

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func PrintReview(w io.Writer, plan Plan) {
	fmt.Fprintf(w, "%s\n", migrationColor(w, ansiBold+ansiCyan, "Wodby 1 to Wodby 2 migration plan"))
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
		{"Migration notes", strconv.Itoa(plan.Summary.Migrations)},
		{"Warnings", strconv.Itoa(plan.Summary.Confirmation + plan.Summary.ServiceWarnings)},
		{"Blocking", strconv.Itoa(plan.Summary.Blocking)},
		{"Manual follow-up", strconv.Itoa(plan.Summary.Manual)},
		{"Intentionally skipped", strconv.Itoa(plan.Summary.Intentionally)},
	})

	fmt.Fprintf(w, "\n%s\n", migrationColor(w, ansiBold+ansiGreen, "Migrations:"))
	for appIndex, app := range plan.Apps {
		fmt.Fprintf(
			w,
			"\n%s\n",
			migrationColor(w, ansiBold+ansiGreen, fmt.Sprintf("App %d/%d: %s → %s", appIndex+1, len(plan.Apps), firstNonEmpty(app.Title, app.Name), app.Name)),
		)
		if app.Repository != nil {
			fmt.Fprintln(w, "  Repository:")
			ci := "Wodby CI (default)"
			if plan.Target.CIIntegrationID > 0 {
				ci = fmt.Sprintf("third-party CI (ID %d)", plan.Target.CIIntegrationID)
			}
			gitIntegration := "-"
			if app.Repository.GitIntegrationID > 0 {
				gitIntegration = fmt.Sprintf("ID %d", app.Repository.GitIntegrationID)
			}
			repositoryCheck := "not validated"
			if strings.TrimSpace(app.Repository.RemoteGitRepoID) != "" {
				repositoryCheck = "exact match found"
			}
			printReviewTable(w, "    ", []string{"Action", "CI", "Git integration", "Target repository", "Repository check", "Build service"}, [][]string{{
				app.Repository.Action,
				ci,
				gitIntegration,
				emptyDash(app.Repository.RepositoryName),
				repositoryCheck,
				emptyDash(app.Repository.TargetService),
			}})
		}
		if len(app.Integrations) != 0 {
			fmt.Fprintln(w, "  Integrations:")
			rows := make([][]string, 0, len(app.Integrations))
			for _, integration := range app.Integrations {
				provider := integration.ProviderName
				if integration.ProviderID > 0 {
					provider = fmt.Sprintf("%s (ID %d, revision %d)", provider, integration.ProviderID, integration.ProviderRevID)
				}
				rows = append(rows, []string{
					integration.Kind, provider, emptyDash(integration.Service), integration.Action,
					emptyDash(strings.Join(integration.Variables, ", ")),
				})
			}
			printReviewTable(w, "    ", []string{"Kind", "Provider", "Service", "Action", "Variables"}, rows)
		}
		for instanceIndex, instance := range app.Instances {
			fmt.Fprintf(
				w,
				"\n  %s\n",
				migrationColor(w, ansiBold, fmt.Sprintf(
					"Instance %d/%d: %s → %s (%s → %s)",
					instanceIndex+1,
					len(app.Instances),
					firstNonEmpty(instance.Title, instance.Name),
					instance.Name,
					emptyDash(instance.SourceType),
					emptyDash(instance.TargetEnv),
				)),
			)
			stackAction := "use mapped stack"
			stackSource := emptyDash(instance.Stack.Name)
			stackTarget := firstNonEmpty(instance.Stack.Target, instance.Stack.Name)
			if instance.Stack.CreateTarget {
				stackAction = "create and configure"
				stackTarget = "new from catalog " + firstNonEmpty(instance.Stack.CatalogName, instance.Stack.Target)
			} else if instance.Stack.ExplicitMapping {
				stackAction = "use and configure existing"
			}
			fmt.Fprintln(w, "    Stack:")
			printReviewTable(w, "      ", []string{"Action", "Wodby 1 stack", "Wodby 2 stack", "Revision"}, [][]string{{
				stackAction,
				stackSource,
				stackTarget,
				emptyDash(instance.Stack.TargetVersion),
			}})
			if instance.BasicAuth.Enabled {
				status := "enabled"
				if instance.BasicAuth.SecretRedacted {
					status = "enabled, password redacted"
				}
				protectedRoutes := 0
				for _, route := range instance.Routes {
					if route.BasicAuth {
						protectedRoutes++
					}
				}
				fmt.Fprintf(
					w,
					"    Basic auth: %s; create Wodby 2 route auths for %d protected domain(s), login %q; password transferred only in memory\n",
					status,
					protectedRoutes,
					instance.BasicAuth.Login,
				)
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
					stackChange := "reuse"
					if service.AddToStack {
						stackChange = "add service"
					}
					rows = append(rows, []string{
						service.SourceName,
						target,
						emptyDash(service.SourceVersion),
						emptyDash(service.TargetVersion),
						emptyDash(service.VersionAction),
						state,
						service.Action,
						stackChange,
						strconv.Itoa(service.EnvVars),
						strconv.Itoa(service.CronJobs),
						strconv.Itoa(service.Settings),
					})
				}
				printReviewTable(w, "      ", []string{"Source", "Target", "Source version", "Target version", "Version action", "State", "Action", "Stack change", "Env vars", "Cron jobs", "Settings"}, rows)
			}
			cronRows := [][]string{}
			for _, service := range instance.Services {
				for _, schedule := range service.CronSchedules {
					cronRows = append(cronRows, []string{
						service.SourceName,
						emptyDash(service.TargetName),
						schedule.Title,
						schedule.Schedule,
						schedule.Command,
						schedule.TargetState,
					})
				}
			}
			if len(cronRows) != 0 {
				fmt.Fprintln(w, "    Cron job → cron schedule migration:")
				printReviewTable(w, "      ", []string{"Source service", "Target service", "Title", "Schedule", "Command", "Target state"}, cronRows)
			}
			if len(instance.Routes) != 0 {
				rows := make([][]string, 0, len(instance.Routes))
				for _, route := range instance.Routes {
					if strings.EqualFold(strings.TrimSpace(route.Type), "technical") {
						continue
					}
					flags := routeFlags(route)
					flags = emptyDash(flags)
					rows = append(rows, []string{
						route.Host,
						reviewRouteAction(route),
						reviewRouteTarget(route),
						reviewRouteTargetState(plan, route),
						flags,
					})
				}
				if len(rows) != 0 {
					fmt.Fprintf(w, "    Custom domains:\n")
					printReviewTable(w, "      ", []string{"Domain", "Action", "Target", "Target state", "Options"}, rows)
				}
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
						emptyDash(firstNonEmpty(item.BackupUUID, item.SourceUUID)),
						reviewBackupCreated(item.BackupCreated),
						reviewByteSize(item.Size),
					})
				}
				printReviewTable(w, "      ", []string{"Component", "Action", "Target", "Backup", "Backup created", "Size"}, rows)
			}
		}
	}

	migrationRows := reviewRows(plan.Review, SeverityMigration)
	if len(migrationRows) != 0 {
		fmt.Fprintf(w, "\n%s\n", migrationColor(w, ansiBold+ansiGreen, fmt.Sprintf("Additional migration details (%d):", len(migrationRows))))
		printReviewTableColor(w, "  ", []string{"Scope", "Subject", "Details"}, migrationRows, ansiGreen)
	}

	sections := []struct {
		severity string
		title    string
	}{
		{SeverityConfirmation, "Warnings"},
		{SeverityServiceWarning, "Enabled services not migrated"},
		{SeverityBlocking, "Blocking"},
		{SeverityManual, "Manual follow-up"},
		{SeveritySkipped, "Intentionally skipped"},
	}
	for _, section := range sections {
		rows := reviewRows(plan.Review, section.severity)
		if len(rows) != 0 {
			color := reviewSeverityColor(section.severity)
			fmt.Fprintf(w, "\n%s\n", migrationColor(w, ansiBold+color, fmt.Sprintf("%s (%d):", section.title, len(rows))))
			printReviewTableColor(w, "  ", []string{"Scope", "Subject", "Details"}, rows, color)
		}
	}
}

func reviewSeverityColor(severity string) string {
	switch severity {
	case SeverityBlocking:
		return ansiRed
	case SeverityConfirmation, SeverityServiceWarning, SeverityManual:
		return ansiOrange
	case SeveritySkipped:
		return ansiGray
	default:
		return ansiCyan
	}
}

func reviewRows(items []ReviewItem, severity string) [][]string {
	rows := make([][]string, 0)
	for _, item := range items {
		if item.Severity != severity {
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
	return rows
}

func reviewBackupCreated(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).UTC().Format("2 Jan 2006, 15:04 UTC")
}

func reviewByteSize(size int64) string {
	if size < 0 {
		return "-"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := "B"
	for _, candidate := range units {
		value /= 1024
		unit = candidate
		if value < 1024 {
			break
		}
	}
	precision := 1
	if math.Abs(value) >= 10 {
		precision = 0
	}
	return fmt.Sprintf("%.*f %s", precision, value, unit)
}

func reviewOverviewRows(plan Plan) [][]string {
	rows := make([][]string, 0)
	instanceCount := plan.Summary.Instances
	if instanceCount == 0 {
		for _, app := range plan.Apps {
			instanceCount += len(app.Instances)
		}
	}
	if len(plan.Apps) == 1 && len(plan.Apps[0].Instances) == 1 {
		rows = append(rows, []string{"Source", reviewSourceLabel(plan.Apps[0], plan.Apps[0].Instances[0])})
	} else if len(plan.Apps) != 0 {
		rows = append(rows, []string{"Source", fmt.Sprintf("%d app(s), %d instance(s)", len(plan.Apps), instanceCount)})
	} else {
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
	} else {
		for _, app := range plan.Apps {
			for _, integration := range app.Integrations {
				if integration.Kind == "ci" {
					targetCI = "Wodby CI by default; Custom CI for source ci deployments"
				}
			}
		}
	}
	rows = append(rows, []string{"Target CI", targetCI})
	if plan.Target.Subscription != nil && plan.Target.Subscription.Plan != nil {
		subscriptionPlan := plan.Target.Subscription.Plan
		rows = append(rows, []string{
			"Target plan",
			firstNonEmpty(subscriptionPlan.Title, subscriptionPlan.Name),
		})
		if strings.EqualFold(strings.TrimSpace(subscriptionPlan.Name), "developer") {
			rows = append(rows, []string{
				"App-service usage",
				fmt.Sprintf("%.0f of %.0f before migration", subscriptionPlan.Usage, subscriptionPlan.UsageIncluded),
			})
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

func printReviewTableColor(w io.Writer, indent string, headers []string, rows [][]string, color string) {
	if !migrationColorEnabled(w) {
		printReviewTable(w, indent, headers, rows)
		return
	}
	var buffer bytes.Buffer
	printReviewTable(&buffer, indent, headers, rows)
	for _, line := range strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n") {
		fmt.Fprintln(w, migrationColor(w, color, line))
	}
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
	if route.SSL {
		flags = append(flags, "TLS")
	}
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

func reviewRouteAction(route RoutePlan) string {
	switch route.Action {
	case "create_backend":
		return "serve"
	case "create_redirect":
		return "redirect"
	case "skip_disabled":
		return "skip disabled source domain"
	case "skip_technical":
		return "skip technical domain"
	default:
		return route.Action
	}
}

func reviewRouteTarget(route RoutePlan) string {
	if route.Action == "create_redirect" {
		switch {
		case strings.TrimSpace(route.RedirectTarget) != "":
			return route.RedirectTarget
		case route.RedirectToWWW:
			return "www hostname"
		case route.RedirectNonWWW:
			return "non-www hostname"
		default:
			return "redirect target"
		}
	}
	if strings.TrimSpace(route.Service) == "" {
		return "-"
	}
	if route.PortNumber == nil {
		return route.Service
	}
	return fmt.Sprintf("%s:%d", route.Service, *route.PortNumber)
}

func reviewRouteTargetState(plan Plan, route RoutePlan) string {
	switch route.Action {
	case "create_backend", "create_redirect":
		if plan.Target.OrgCapabilities == nil {
			return "creation state not verified"
		}
		if !plan.Target.OrgCapabilities.CustomDomains {
			return "will be created disabled"
		}
		return "will be created enabled"
	case "skip_disabled", "skip_technical":
		return "will not be migrated"
	default:
		if route.ReviewRequired {
			return "blocked"
		}
		return "not resolved"
	}
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
