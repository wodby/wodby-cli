package ops

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	machineTypeColumns       = []string{"name", "title", "sharedCpu", "disk", "priceHourly", "priceMonthly"}
	stackValueColumns        = []string{"id", "name", "value", "envType", "createdAt"}
	stackTokenColumns        = []string{"id", "name", "value", "regex", "envType", "createdAt"}
	resolvedConfigColumns    = []string{"name", "filepath", "status", "defaultSource", "platform", "error"}
	podColumns               = []string{"name", "workload", "phase", "ready", "terminating", "node", "cpuUsage", "memoryUsage", "createdAt"}
	appPortSettingColumns    = []string{"id", "name", "value", "updatedAt"}
	appEndpointColumns       = []string{"id", "name", "appServiceId", "primary", "routeIds", "portIds"}
	appAnnotationColumns     = []string{"id", "name", "value", "type", "appServiceId", "appRouteId", "updatedAt"}
	projectMembershipColumns = []string{"id", "projectId", "orgMembershipId", "teamId", "role", "updatedAt"}
	teamColumns              = []string{"id", "name", "title", "members", "updatedAt"}
	ssoProviderColumns       = []string{"id", "name", "kind", "enabled", "jitEnabled", "issuerUrl", "domains"}
	ssoDomainColumns         = []string{"id", "domain", "verified", "verificationRecordType", "verificationRecordName", "verificationRecordValue"}
)

// findChild returns the subcommand with the given name, or nil.
func findChild(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

// newConfirmedPostCommand posts to pathPattern after confirmation and prints the operation result.
func newConfirmedPostCommand(use string, short string, pathPattern string, prompt string, out outputOptions) *cobra.Command {
	var yes bool
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, prompt); err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath(pathPattern, args[0]), nil)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	addWaitFlags(cmd, &wait)
	return cmd
}

// newOrgListCommand lists an organization collection at path, inferring the organization when the credentials expose
// only one.
func newOrgListCommand(use string, short string, path string, columns []string, out outputOptions) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, url.Values{"orgId": []string{resolvedOrgID}}, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	return cmd
}

// newQueryListCommand lists a collection filtered by the positional argument passed as queryName.
func newQueryListCommand(use string, short string, path string, queryName string, columns []string, out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, url.Values{queryName: []string{args[0]}}, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
}

func addClusterRemainingCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(
		newServiceChildCreateCommand("update-kubernetes-version ID", "Upgrade the cluster to a Kubernetes version from its upgrade plan", "/clusters/%s/actions/update-kubernetes-version", operationColumns, out, []jsonFlagSpec{
			stringJSONFlag("version", "version", "Target Kubernetes version", true),
		}),
		newServiceChildCreateCommand("scale ID", "Change the cluster's minimum and maximum node count", "/clusters/%s/actions/scale", operationColumns, out, []jsonFlagSpec{
			intJSONFlag("min-nodes", "minNodeCount", "Minimum node count", false),
			intJSONFlag("max-nodes", "maxNodeCount", "Maximum node count", true),
		}),
		newConfirmedPostCommand("recycle-nodes ID", "Replace the cluster nodes one at a time", "/clusters/%s/actions/recycle-nodes", "Replace all cluster nodes?", out),
		newServiceChildCreateCommand("update-k3s-public-ip ID", "Change the public IP address of a k3s cluster", "/clusters/%s/actions/update-k3s-public-ip", clusterColumns, out, []jsonFlagSpec{
			stringJSONFlag("ip", "publicIp", "Public IP address", true),
		}),
		newClusterCreateVariantCommand("create-k3s", "Register a self-hosted k3s cluster", "/clusters/actions/create-k3s", []jsonFlagSpec{
			stringJSONFlag("name", "name", "Cluster machine name", true),
			stringJSONFlag("title", "title", "Cluster title", true),
			boolJSONFlag("disable-monitoring", "disableMonitoring", "Disable cluster monitoring", true),
		}, clusterColumns, out),
		newClusterCreateVariantCommand("create-wodby-cloud", "Create a cluster hosted by Wodby and billed to the organization", "/clusters/actions/create-wodby-cloud", []jsonFlagSpec{
			boolJSONFlag("demo", "demo", "Create a free demo cluster", true),
			stringJSONFlag("region", "region", "Region", false),
			stringJSONFlag("machine-type", "machineType", "Machine type from cluster machine-types", false),
			intJSONFlag("min-nodes", "minNodeCount", "Minimum node count", false),
			intJSONFlag("max-nodes", "maxNodeCount", "Maximum node count", false),
		}, []string{"cluster.id", "cluster.name", "cluster.status", "taskId"}, out),
		newK3SInstallCommand(out),
		&cobra.Command{
			Use:   "machine-types",
			Short: "List Wodby Cloud machine types with their prices",
			RunE: func(cmd *cobra.Command, args []string) error {
				return getAndPrintList(cmd, out, "/wodby-cloud/machine-types", nil, machineTypeColumns)
			},
		},
	)
}

// newClusterCreateVariantCommand creates a k3s or Wodby Cloud cluster from flags or a JSON body.
func newClusterCreateVariantCommand(use string, short string, path string, specs []jsonFlagSpec, columns []string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				values, err := bodyFromJSONFlagSpecs(cmd, specs, false)
				if err != nil {
					return err
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				addAvailableToAllProjects(cmd, values)
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), path, nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	addAvailableToAllProjectsFlag(cmd)
	return cmd
}

func newK3SInstallCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "k3s-install-command ID",
		Short: "Print the command that connects a k3s server; it contains short-lived credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/clusters/k3s-install-command/%s", args[0]), nil, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatValue(objectItem(result)["command"]))
			return nil
		},
	}
}

// getAndPrintList prints a collection fetched from path.
func getAndPrintList(cmd *cobra.Command, out outputOptions, path string, query url.Values, columns []string) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, query, &result); err != nil {
		return err
	}
	return printClientResult(cmd, client, out, result, columns)
}

func addStackRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var provider string
	infra := &cobra.Command{
		Use:   "infra",
		Short: "List the infrastructure stacks Wodby installs for a public Kubernetes provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(provider, "--provider"); err != nil {
				return err
			}
			return getAndPrintList(cmd, out, "/catalog/infra-stacks", url.Values{"provider": []string{provider}}, stackColumns)
		},
	}
	infra.Flags().StringVar(&provider, "provider", "", "Provider name, such as aws")

	cmd.AddCommand(
		newRawBodyPostCommand("create", "Create a stack from catalog services (JSON body: orgId, projectId, name, title, services)", "/stacks", stackColumns, out),
		newConfirmedPostCommand("discard-draft ID", "Drop the stack's unpublished draft revision", "/stacks/%s/actions/discard-draft", "Discard the stack draft?", out),
		infra,
		newStackValueCommand("helm-value", "Helm value", "helm-values", "stack-helm-values", stackValueColumns, true, out),
		newStackValueCommand("token", "token", "tokens", "stack-tokens", stackTokenColumns, true, out),
		newStackValueCommand("annotation", "annotation", "annotations", "stack-annotations", stackValueColumns, false, out),
	)
}

// newStackValueCommand manages one kind of stack-wide value that applies to every service of a stack revision.
func newStackValueCommand(use string, noun string, kind string, itemPath string, columns []string, updatable bool, out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: use, Aliases: []string{use + "s"}, Short: "Manage stack-wide " + noun + "s"}
	createSpecs := []jsonFlagSpec{
		stringJSONFlag("name", "name", "Name", true),
		stringJSONFlag("env-type", "envType", "Limit it to an environment type", false),
	}
	updateSpecs := []jsonFlagSpec{boolJSONFlag("secret", "secret", "Store the value as a secret", true)}
	switch kind {
	case "tokens":
		createSpecs = append(createSpecs,
			stringJSONFlag("value", "value", "Value; leave it out to generate one from --regex", false),
			stringJSONFlag("regex", "regex", "Pattern that generates the value for each environment", false),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true))
		updateSpecs = append(updateSpecs, stringJSONFlag("value", "value", "Value", false), stringJSONFlag("regex", "regex", "Pattern", false))
	case "annotations":
		createSpecs = append(createSpecs, stringJSONFlag("value", "value", "Value", true))
	default:
		createSpecs = append(createSpecs, stringJSONFlag("value", "value", "Value", true), boolJSONFlag("secret", "secret", "Store the value as a secret", true))
		updateSpecs = append(updateSpecs, stringJSONFlag("value", "value", "Value", true))
	}
	cmd.AddCommand(
		newServiceChildListCommand("list REVISION_ID", "List the stack-wide "+noun+"s of a stack revision", "/stack-revisions/%s/"+kind, columns, out),
		newServiceChildCreateCommand("create STACK_ID", "Add a stack-wide "+noun+" to the stack's draft", "/stacks/%s/configuration/"+kind, columns, out, createSpecs),
		newDeleteCommand("delete ID", "Delete stack-wide "+noun, "/"+itemPath+"/", columns, out),
	)
	if updatable {
		cmd.AddCommand(newServiceChildUpdateCommand("update ID", "Update stack-wide "+noun, "/"+itemPath+"/%s", columns, out, updateSpecs))
	}
	return cmd
}

func addServiceRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var optionVersion, platform string
	configs := &cobra.Command{
		Use:   "configs REVISION_ID",
		Short: "List a service revision's configuration files with their resolved default content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(optionVersion, "--option-version"); err != nil {
				return err
			}
			query := url.Values{"optionVersion": []string{optionVersion}}
			addQuery(query, "platform", platform)
			return getAndPrintList(cmd, out, escapedPath("/service-revisions/%s/configs", args[0]), query, resolvedConfigColumns)
		},
	}
	configs.Flags().StringVar(&optionVersion, "option-version", "", "Service option version")
	configs.Flags().StringVar(&platform, "platform", "", "Image platform, such as linux/arm64")
	cmd.AddCommand(
		newDeleteCommand("delete ID", "Delete a service that no stack uses", "/services/", catalogServiceColumns, out),
		newUpdateFromGitCommand("update-from-git ID", "Update a Git-sourced service to another ref", "/services/%s/actions/update-from-git", out),
		newServiceChildListCommand("inheriting NAME", "List the services that inherit from a service", "/services/by-name/%s/inheriting-services", catalogServiceColumns, out),
		configs,
	)
}

func addProviderRemainingCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(
		newServiceChildUpdateCommand("update ID", "Change the provider title", "/providers/%s", providerColumns, out, []jsonFlagSpec{
			stringJSONFlag("title", "title", "Provider title", true),
		}),
		newDeleteCommand("delete ID", "Delete a custom provider that no integration uses", "/providers/", providerColumns, out),
	)
}

func addBackupRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var saveTo string
	download := &cobra.Command{
		Use:   "download ID",
		Short: "Get a short-lived download URL for a backup file, or save the file with --save-to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/backups/%s/download", args[0]), nil, &result); err != nil {
				return err
			}
			if saveTo == "" {
				return printClientResult(cmd, client, out, result, []string{"url", "expiresAt"})
			}
			return downloadFile(cmd, formatValue(objectItem(result)["url"]), saveTo)
		},
	}
	download.Flags().StringVar(&saveTo, "save-to", "", "Save the backup file to this path")
	cmd.AddCommand(download)
}

// downloadFile saves the content at rawURL to path without sending Wodby credentials.
func downloadFile(cmd *cobra.Command, rawURL string, path string) error {
	if rawURL == "" {
		return errors.New("the API returned no download URL")
	}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "download backup")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("download backup: unexpected status %s", resp.Status)
	}
	file, err := os.Create(path)
	if err != nil {
		return errors.WithStack(err)
	}
	written, copyErr := io.Copy(file, resp.Body)
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(path)
		return errors.Wrap(copyErr, "save backup")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved %d bytes to %s\n", written, path)
	return nil
}

func addAppServiceRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var all bool
	var workload, container string
	pods := &cobra.Command{
		Use:   "pods ID",
		Short: "List an app service's pods with their containers and resource use",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			if all {
				query.Set("mode", "all")
			}
			addQuery(query, "workload", workload)
			addQuery(query, "container", container)
			return getAndPrintList(cmd, out, escapedPath("/app-services/%s/pods", args[0]), query, podColumns)
		},
	}
	pods.Flags().BoolVar(&all, "all", false, "Include every workload, not only the main one")
	pods.Flags().StringVar(&workload, "workload", "", "Only pods of this workload")
	pods.Flags().StringVar(&container, "container", "", "Only pods with this container")
	cmd.AddCommand(pods)
	if cron := findChild(cmd, "cron-schedule"); cron != nil {
		cron.AddCommand(newGetCommand("get ID", "Get app service cron schedule", "/app-service-cron-schedules/", appServiceCronScheduleColumns, out))
	}
}

func addEnvironmentRemainingCommands(cmd *cobra.Command, out outputOptions) {
	if auth := findChild(cmd, "auth"); auth != nil {
		auth.AddCommand(newGetCommand("get ID", "Get HTTP basic authentication", "/app-auths/", appAuthColumns, out))
	}
	if access := findChild(cmd, "access"); access != nil {
		access.AddCommand(newGetCommand("get-by-id ACCESS_ID", "Get app access by its ID", "/app-accesses/", appAccessColumns, out))
	}
	annotation := &cobra.Command{Use: "annotation", Aliases: []string{"annotations"}, Short: "Manage ingress annotations of app environments"}
	var kinds string
	list := &cobra.Command{
		Use:   "list ENVIRONMENT_ID",
		Short: "List app environment annotations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"appEnvironmentId": []string{args[0]}}
			addQuery(query, "types", kinds)
			return getAndPrintList(cmd, out, "/app-annotations", query, appAnnotationColumns)
		},
	}
	list.Flags().StringVar(&kinds, "type", "", "Annotation type, such as ingress")
	defaultToList(annotation, list)
	annotation.AddCommand(
		list,
		newGetCommand("get ID", "Get app annotation", "/app-annotations/", appAnnotationColumns, out),
		newAppAnnotationCreateCommand(out),
		newServiceChildUpdateCommand("update ID", "Update app annotation", "/app-annotations/%s", appAnnotationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Annotation name", true),
			stringJSONFlag("value", "value", "Annotation value", true),
			intJSONFlag("service", "appServiceId", "Limit it to one service", false),
			intJSONFlag("route", "appRouteId", "Limit it to one route", false),
		}),
		newDeleteCommand("delete ID", "Delete app annotation", "/app-annotations/", appAnnotationColumns, out),
	)
	cmd.AddCommand(annotation, newEndpointCommand("endpoint", "Show app environment endpoints", out))
}

func newAppAnnotationCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var service, route, kind string
	var name, value string
	cmd := &cobra.Command{
		Use:   "create ENVIRONMENT_ID",
		Short: "Add an ingress annotation to an app environment, service or route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				values := map[string]interface{}{"name": name, "value": value, "type": kind}
				if err := addOptionalInt(values, "appEnvironmentId", args[0], "ENVIRONMENT_ID"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "appServiceId", service, "--service"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "appRouteId", route, "--route"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-annotations", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appAnnotationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Annotation name")
	cmd.Flags().StringVar(&value, "value", "", "Annotation value")
	cmd.Flags().StringVar(&kind, "type", "ingress", "Annotation type")
	cmd.Flags().StringVar(&service, "service", "", "Limit it to one app service")
	cmd.Flags().StringVar(&route, "route", "", "Limit it to one route")
	return cmd
}

// newEndpointCommand shows the endpoints that group an app service's routes and ports.
func newEndpointCommand(use string, short string, out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: use, Aliases: []string{use + "s"}, Short: short}
	list := newQueryListCommand("list ENVIRONMENT_ID", "List app environment endpoints", "/app-endpoints", "appEnvironmentId", appEndpointColumns, out)
	defaultToList(cmd, list)
	cmd.AddCommand(list, newGetCommand("get ID", "Get app endpoint", "/app-endpoints/", appEndpointColumns, out))
	return cmd
}

func addPortRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var publicPort int
	publish := &cobra.Command{
		Use:   "publish ID",
		Short: "Expose an app port on the cluster's public address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var body interface{}
			if cmd.Flags().Changed("public-port") {
				body = map[string]interface{}{"publicPort": publicPort}
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/app-ports/%s/actions/publish", args[0]), nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appPortColumns)
		},
	}
	publish.Flags().IntVar(&publicPort, "public-port", 0, "Public port; a free port is chosen when left out")

	settings := &cobra.Command{Use: "settings", Aliases: []string{"setting"}, Short: "Manage app port settings"}
	list := newServiceChildListCommand("list PORT_ID", "List app port settings", "/app-ports/%s/settings", appPortSettingColumns, out)
	defaultToList(settings, list)
	var yes bool
	deleteSetting := &cobra.Command{
		Use:   "delete PORT_ID NAME",
		Short: "Remove an app port setting",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Remove the port setting?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), escapedPath("/app-ports/%s/settings/%s", args[0], args[1]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	deleteSetting.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	settings.AddCommand(
		list,
		newServiceChildSetCommand("set PORT_ID NAME", "Set an app port setting (NAME such as IDLE_TIMEOUT)", "/app-ports/%s/settings/%s", appPortSettingColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Setting value", true),
		}),
		newSettingsReplaceCommand("replace PORT_ID", "Replace all app port settings; a setting left out is removed", "/app-ports/%s/settings", appPortSettingColumns, "IDLE_TIMEOUT=300", out),
		deleteSetting,
	)
	cmd.AddCommand(
		newServiceChildUpdateCommand("update ID", "Change the public port of an app port", "/app-ports/%s", appPortColumns, out, []jsonFlagSpec{
			intJSONFlag("public-port", "publicPort", "Public port", true),
		}),
		publish,
		&cobra.Command{
			Use:   "unpublish ID",
			Short: "Stop exposing an app port on the cluster's public address",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := newRESTClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := client.Post(cmd.Context(), escapedPath("/app-ports/%s/actions/unpublish", args[0]), nil, nil, &result); err != nil {
					return err
				}
				return printClientResult(cmd, client, out, result, appPortColumns)
			},
		},
		settings,
	)
}

func addMemberRemainingCommands(cmd *cobra.Command, out outputOptions) {
	var orgID, email, role string
	var teams, projects []string
	invite := &cobra.Command{
		Use:   "invite",
		Short: "Invite a person to the organization by email",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(email, "--email"); err != nil {
				return err
			}
			if err := requireFlag(role, "--role"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			body := map[string]interface{}{"email": email, "role": role}
			if err := addOptionalInt(body, "orgId", resolvedOrgID, "--org"); err != nil {
				return err
			}
			if err := addIntList(body, "teamIds", teams, "--team"); err != nil {
				return err
			}
			if err := addIntList(body, "projectIds", projects, "--project"); err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/org-memberships", nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, memberColumns)
		},
	}
	invite.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	invite.Flags().StringVar(&email, "email", "", "Email address")
	invite.Flags().StringVar(&role, "role", "", "member, admin or owner")
	invite.Flags().StringArrayVar(&teams, "team", nil, "Team ID to add the member to; repeatable")
	invite.Flags().StringArrayVar(&projects, "project", nil, "Project ID to add the member to; repeatable")
	cmd.AddCommand(
		invite,
		newServiceChildUpdateCommand("update ID", "Change a member's organization role", "/org-memberships/%s", memberColumns, out, []jsonFlagSpec{
			stringJSONFlag("role", "role", "member, admin or owner", true),
		}),
		newDeleteCommand("remove ID", "Remove a member or pending invitation", "/org-memberships/", memberColumns, out),
	)
}

func addIntList(values map[string]interface{}, key string, raw []string, flag string) error {
	ids, err := parseIntValues(raw, flag)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		values[key] = ids
	}
	return nil
}

func addProjectRemainingCommands(cmd *cobra.Command, out outputOptions) {
	member := &cobra.Command{Use: "member", Aliases: []string{"members"}, Short: "Manage project members and teams"}
	var members, teams []string
	var role string
	add := &cobra.Command{
		Use:   "add PROJECT_ID",
		Short: "Give organization members or teams a role in a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(role, "--role"); err != nil {
				return err
			}
			body := map[string]interface{}{"role": role}
			if err := addOptionalInt(body, "projectId", args[0], "PROJECT_ID"); err != nil {
				return err
			}
			if err := addIntList(body, "orgMembershipIds", members, "--member"); err != nil {
				return err
			}
			if err := addIntList(body, "teamIds", teams, "--team"); err != nil {
				return err
			}
			if _, ok := body["orgMembershipIds"]; !ok {
				if _, ok := body["teamIds"]; !ok {
					return errors.New("pass --member or --team")
				}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/project-memberships", nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, projectMembershipColumns)
		},
	}
	add.Flags().StringArrayVar(&members, "member", nil, "Organization member ID; repeatable")
	add.Flags().StringArrayVar(&teams, "team", nil, "Team ID; repeatable")
	add.Flags().StringVar(&role, "role", "", "read, write or admin")
	list := newQueryListCommand("list PROJECT_ID", "List project members and teams", "/project-memberships", "projectId", projectMembershipColumns, out)
	defaultToList(member, list)
	member.AddCommand(
		list,
		add,
		newServiceChildUpdateCommand("update ID", "Change a project role", "/project-memberships/%s", projectMembershipColumns, out, []jsonFlagSpec{
			stringJSONFlag("role", "role", "read, write or admin", true),
		}),
		newDeleteCommand("remove ID", "Remove a member or team from a project", "/project-memberships/", projectMembershipColumns, out),
	)
	cmd.AddCommand(member)
}

// newTeamCommand manages teams of organization members.
func newTeamCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "team", Aliases: []string{"teams"}, Short: "Manage teams"}
	addOutputFlag(cmd, &out)
	list := newOrgListCommand("list", "List teams", "/teams", teamColumns, out)
	defaultToList(cmd, list)

	var orgID string
	getByName := &cobra.Command{
		Use:   "get-by-name NAME",
		Short: "Get team by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/teams/by-name/%s", args[0]), url.Values{"orgId": []string{resolvedOrgID}}, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, teamColumns)
		},
	}
	getByName.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.AddCommand(list, getByName, newTeamWriteCommand("create", "Create team", http.MethodPost, out), newTeamWriteCommand("update ID", "Rename a team and replace its members", http.MethodPut, out),
		newDeleteCommand("delete ID", "Delete team", "/teams/", teamColumns, out))
	return cmd
}

// newTeamWriteCommand creates a team, or updates one. The API replaces the whole member list, so an update keeps the
// current name, title and members for any of them not given as flags.
func newTeamWriteCommand(use string, short string, method string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title string
	var members []string
	args := cobra.NoArgs
	if method == http.MethodPut {
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				items, err := teamMemberItems(members)
				if err != nil {
					return err
				}
				values := map[string]interface{}{"name": name, "title": title, "members": items}
				if method == http.MethodPut {
					if err := fillTeamUpdateDefaults(cmd, client, args[0], values); err != nil {
						return err
					}
				} else {
					resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
					if err != nil {
						return err
					}
					if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
						return err
					}
				}
				if err := requireFlag(formatValue(values["name"]), "--name"); err != nil {
					return err
				}
				if err := requireFlag(formatValue(values["title"]), "--title"); err != nil {
					return err
				}
				requestBody = values
			}
			var result interface{}
			if method == http.MethodPost {
				err = client.Post(cmd.Context(), "/teams", nil, requestBody, &result)
			} else {
				err = client.Put(cmd.Context(), escapedPath("/teams/%s", args[0]), nil, requestBody, &result)
			}
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, teamColumns)
		},
	}
	addBodyFlags(cmd, &body)
	memberUsage := "Member as ORG_MEMBERSHIP_ID=member or ORG_MEMBERSHIP_ID=leader; repeat for each member"
	if method == http.MethodPost {
		cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	} else {
		memberUsage += "; replaces the member list, which is kept when left out"
	}
	cmd.Flags().StringVar(&name, "name", "", "Team machine name")
	cmd.Flags().StringVar(&title, "title", "", "Team title")
	cmd.Flags().StringArrayVar(&members, "member", nil, memberUsage)
	return cmd
}

// teamMemberItems parses ORG_MEMBERSHIP_ID=ROLE flags into team member request items.
func teamMemberItems(members []string) ([]interface{}, error) {
	items := make([]interface{}, 0, len(members))
	for _, member := range members {
		id, role, ok := strings.Cut(member, "=")
		number, err := strconv.Atoi(strings.TrimSpace(id))
		if !ok || err != nil || (role != "member" && role != "leader") {
			return nil, errors.Errorf("invalid --member %q: use ORG_MEMBERSHIP_ID=member or ORG_MEMBERSHIP_ID=leader", member)
		}
		items = append(items, map[string]interface{}{"orgMembershipId": number, "role": role})
	}
	return items, nil
}

// fillTeamUpdateDefaults copies the team's current name, title and members into values for flags left out.
func fillTeamUpdateDefaults(cmd *cobra.Command, client *rest.Client, id string, values map[string]interface{}) error {
	if cmd.Flags().Changed("name") && cmd.Flags().Changed("title") && cmd.Flags().Changed("member") {
		return nil
	}
	var current map[string]interface{}
	if err := client.Get(cmd.Context(), escapedPath("/teams/%s", id), nil, &current); err != nil {
		return err
	}
	if !cmd.Flags().Changed("name") {
		values["name"] = current["name"]
	}
	if !cmd.Flags().Changed("title") {
		values["title"] = current["title"]
	}
	if !cmd.Flags().Changed("member") {
		items := []interface{}{}
		currentMembers, _ := current["members"].([]interface{})
		for _, raw := range currentMembers {
			member, _ := raw.(map[string]interface{})
			items = append(items, map[string]interface{}{"orgMembershipId": member["orgMembershipId"], "role": member["role"]})
		}
		values["members"] = items
	}
	return nil
}

func addOrgRemainingCommands(cmd *cobra.Command, out outputOptions) {
	sso := &cobra.Command{Use: "sso-provider", Aliases: []string{"sso-providers", "sso"}, Short: "Manage single sign-on providers"}
	list := newOrgListCommand("list", "List SSO providers", "/org-sso-providers", ssoProviderColumns, out)
	defaultToList(sso, list)
	sso.AddCommand(
		list,
		newGetCommand("get ID", "Get SSO provider", "/org-sso-providers/", ssoProviderColumns, out),
		newRawBodyPostCommand("create", "Create an SSO provider (JSON body: orgId, kind, name, issuerUrl, clientId, clientSecret, enabled, jitEnabled, domains)", "/org-sso-providers", ssoProviderColumns, out),
		newRawBodyPutCommand("update ID", "Replace SSO provider settings; leave clientSecret out to keep it", "/org-sso-providers/%s", ssoProviderColumns, out),
		newDeleteCommand("delete ID", "Delete SSO provider", "/org-sso-providers/", ssoProviderColumns, out),
		&cobra.Command{
			Use:   "verify-domain ID DOMAIN",
			Short: "Check the DNS record that proves the organization owns an SSO domain",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := newRESTClient()
				if err != nil {
					return err
				}
				var result interface{}
				if err := client.Post(cmd.Context(), escapedPath("/org-sso-providers/%s/actions/verify-domain", args[0]), nil, map[string]interface{}{"domain": args[1]}, &result); err != nil {
					return err
				}
				return printClientResult(cmd, client, out, result, ssoDomainColumns)
			},
		},
	)
	cmd.AddCommand(sso)
}
