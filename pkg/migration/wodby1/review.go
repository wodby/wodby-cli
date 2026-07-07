package wodby1

import (
	"fmt"
	"io"
	"strings"
)

func PrintReview(w io.Writer, plan Plan) {
	fmt.Fprintf(w, "Wodby 1 migration review\n")
	fmt.Fprintf(w, "Status: %s\n", plan.Status)
	fmt.Fprintf(w, "Source: %s %s\n", plan.Source.Kind, plan.Source.ID)
	if plan.Target.Org != "" || plan.Target.Project != "" || plan.Target.Cluster != "" {
		fmt.Fprintf(w, "Target: org=%s project=%s cluster=%s\n", emptyDash(plan.Target.Org), emptyDash(plan.Target.Project), emptyDash(plan.Target.Cluster))
	}
	fmt.Fprintf(w, "\nSummary:\n")
	fmt.Fprintf(w, "  Apps: %d\n", plan.Summary.Apps)
	fmt.Fprintf(w, "  Instances: %d\n", plan.Summary.Instances)
	fmt.Fprintf(w, "  Services: %d\n", plan.Summary.Services)
	fmt.Fprintf(w, "  Routes: %d\n", plan.Summary.Routes)
	fmt.Fprintf(w, "  Env vars: %d\n", plan.Summary.EnvVars)
	fmt.Fprintf(w, "  Cron jobs: %d\n", plan.Summary.CronJobs)
	fmt.Fprintf(w, "  Imports: %d\n", plan.Summary.Imports)
	fmt.Fprintf(w, "  Blocking: %d\n", plan.Summary.Blocking)
	fmt.Fprintf(w, "  Requires confirmation: %d\n", plan.Summary.Confirmation)
	fmt.Fprintf(w, "  Manual follow-up: %d\n", plan.Summary.Manual)
	fmt.Fprintf(w, "  Intentionally skipped: %d\n", plan.Summary.Intentionally)

	for _, app := range plan.Apps {
		fmt.Fprintf(w, "\nApp: %s (%s)\n", firstNonEmpty(app.Title, app.Name), app.SourceUUID)
		for _, instance := range app.Instances {
			fmt.Fprintf(w, "  Instance: %s type=%s targetEnv=%s stack=%s\n", firstNonEmpty(instance.Title, instance.Name), instance.SourceType, emptyDash(instance.TargetEnv), instance.Stack.Name)
			fmt.Fprintf(w, "    Technical domain: %s\n", instance.TechnicalDomain)
			if instance.BasicAuth.Enabled {
				status := "enabled"
				if instance.BasicAuth.SecretRedacted {
					status = "enabled, password redacted"
				}
				fmt.Fprintf(w, "    Basic auth: %s\n", status)
			}
			if len(instance.Services) != 0 {
				fmt.Fprintf(w, "    Services:\n")
				for _, service := range instance.Services {
					target := service.TargetName
					if target == "" {
						target = "-"
					}
					fmt.Fprintf(w, "      - %s -> %s action=%s envVars=%d cronJobs=%d\n", service.SourceName, target, service.Action, service.EnvVars, service.CronJobs)
				}
			}
			if len(instance.Routes) != 0 {
				fmt.Fprintf(w, "    Routes:\n")
				for _, route := range instance.Routes {
					flags := routeFlags(route)
					if flags != "" {
						flags = " " + flags
					}
					port := "-"
					if route.PortNumber != nil {
						port = fmt.Sprintf("%d", *route.PortNumber)
					}
					fmt.Fprintf(w, "      - %s service=%s portNumber=%s%s\n", route.Host, emptyDash(route.Service), port, flags)
				}
			}
		}
	}

	if len(plan.Review) != 0 {
		fmt.Fprintf(w, "\nReview items:\n")
		for _, item := range plan.Review {
			scope := item.App
			if item.Instance != "" {
				scope += "/" + item.Instance
			}
			if strings.TrimSpace(scope) == "" {
				scope = "-"
			}
			fmt.Fprintf(w, "  [%s] %s %s: %s\n", item.Severity, scope, item.Subject, item.Message)
		}
	}
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
		flags = append(flags, "settings="+strings.Join(route.Settings, ","))
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
