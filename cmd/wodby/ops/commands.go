package ops

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	userColumns                      = []string{"id", "email", "name", "twofa", "defaultOrg", "defaultProjects", "createdAt", "updatedAt"}
	orgColumns                       = []string{"id", "name", "title", "domain", "defaultTimeZone", "ciIntegrationId", "registryIntegrationId"}
	memberColumns                    = []string{"id", "member", "email", "role", "status", "joinedAt"}
	projectColumns                   = []string{"id", "name", "title"}
	envColumns                       = []string{"id", "name", "title", "type"}
	databaseColumns                  = []string{"id", "name", "title", "status", "kind", "type", "version", "env", "integration", "service", "region", "zone"}
	databaseDbColumns                = []string{"id", "name", "status", "charset", "collation", "database", "createdAt"}
	databaseCharsetColumns           = []string{"name", "title", "default", "defaultCollation"}
	databaseUserColumns              = []string{"id", "username", "hostname", "status", "database", "dbs", "createdAt"}
	clusterColumns                   = []string{"id", "name", "title", "status", "autoUpdates", "integration", "region", "zone", "version", "singleNode"}
	clusterGetColumns                = []string{"id", "name", "title", "status", "autoUpdates", "integration", "region", "zone", "kubernetesVersion", "infraVersion", "ips", "singleNode", "storageClasses", "storageClassesObservedAt"}
	infraAppColumns                  = []string{"id", "name", "title", "status", "stack"}
	integrationColumns               = []string{"id", "name", "title", "scope", "status", "provider", "createdAt"}
	providerColumns                  = []string{"id", "name", "title", "status", "providerVersion"}
	providerRevisionColumns          = []string{"id", "name", "title", "number", "version", "provider", "permissionAudit", "createdAt"}
	stackColumns                     = []string{"id", "name", "title", "status", "revision", "currentVersion", "outdated", "autoUpdates", "createdAt", "updatedAt"}
	stackGetColumns                  = []string{"id", "name", "title", "status", "public", "revId", "currentRevNumber", "currentVersion", "latestRevNumber", "outdated", "autoUpdates", "createdAt", "updatedAt", "services"}
	stackRevisionColumns             = []string{"id", "name", "title", "number", "draft", "version", "stack", "linkIssues", "createdAt"}
	stackServiceColumns              = []string{"id", "name", "title", "type", "serviceRev", "serviceRevPinned", "replicas", "required", "disabled", "main", "updatedAt"}
	stackServiceEnvColumns           = []string{"id", "name", "value", "secret", "envType", "workload", "container", "createdAt"}
	stackServiceValueColumns         = []string{"id", "name", "value", "secret", "envType", "createdAt"}
	stackServiceTokenColumns         = []string{"id", "name", "value", "regex", "secret", "envType", "createdAt"}
	stackServiceAnnotationColumns    = []string{"id", "name", "value", "envType", "createdAt"}
	stackServiceIntegrationColumns   = []string{"id", "name", "integration"}
	stackServiceLinkColumns          = []string{"id", "name", "linkedStackService"}
	stackServiceVolumeColumns        = []string{"id", "name", "size"}
	stackServiceSettingColumns       = []string{"id", "name", "value"}
	stackServiceConfigColumns        = []string{"id", "name", "disabled", "config"}
	stackServiceOptionColumns        = []string{"id", "version", "default", "disabled"}
	stackServiceCronScheduleColumns  = []string{"id", "name", "title", "crontab", "command", "workload", "envType", "disabled", "updatedAt"}
	catalogServiceColumns            = []string{"id", "name", "title", "type", "status", "autoUpdates", "public", "external", "revId", "latestRevNumber"}
	serviceRevisionColumns           = []string{"id", "name", "title", "type", "external", "number", "version", "service", "createdAt"}
	appColumns                       = []string{"id", "name", "title", "status", "stack", "instances"}
	appGetColumns                    = []string{"id", "name", "title", "status", "stack", "clusterApp", "instances", "createdAt", "updatedAt"}
	appStatusColumns                 = []string{"id", "title", "status", "instances", "serviceStatus", "routeStatus", "latestBuild", "latestDeployment", "needs"}
	instanceColumns                  = []string{"id", "name", "title", "status", "outdated", "autoUpdates", "app", "stack", "env", "cluster", "domain", "routingMode", "routingPending", "configurationReady", "configurationIssues"}
	instanceListColumns              = append(append([]string{}, instanceColumns...), "lastDeployedAt")
	instanceGetColumns               = append(append([]string{}, instanceColumns...), "cronHealth", "backupHealth", "serviceStatus", "routeStatus", "portStatus", "createdAt", "updatedAt")
	instanceCICDSettingsColumns      = []string{"appInstanceId", "ciIntegrationId", "registryIntegrationId", "registryRepository"}
	instanceStatusColumns            = []string{"id", "title", "status", "cronHealth", "backupHealth", "serviceStatus", "routeStatus", "portStatus", "latestBuild", "latestDeployment", "needs"}
	serviceColumns                   = []string{"id", "name", "title", "type", "status", "version", "replicas", "scalability", "disabled", "main", "needsRebuild", "needsRedeploy", "configurationReady", "configurationIssues", "buildSourceBoilerplate"}
	appServiceEnvColumns             = []string{"id", "name", "value", "secret", "runtime", "build", "envType", "workload", "container", "source", "createdAt"}
	appServiceValueColumns           = []string{"id", "name", "value", "secret", "source", "createdAt"}
	appServiceTokenColumns           = []string{"id", "name", "value", "secret", "envType", "createdAt"}
	appServiceAnnotationColumns      = []string{"id", "name", "value", "envType", "source", "createdAt"}
	appServiceIntegrationColumns     = []string{"id", "name", "integration", "createdAt", "updatedAt"}
	appServiceSettingColumns         = []string{"id", "name", "value", "var", "runtime", "build", "fromSettingId"}
	appServiceConfigColumns          = []string{"id", "name", "title", "disabled", "config"}
	appServiceLinkColumns            = []string{"id", "name", "linkedService"}
	appServiceContainerColumns       = []string{"id", "workload", "name", "requestCPU", "requestMem", "limitCPU", "limitMem"}
	appServiceVolumeColumns          = []string{"id", "name", "path", "size", "shared", "readOnly", "configuredStorageClassName", "effectiveStorageClassNames", "storageClassStatus", "storageClassSelectable", "fromVolumeId", "storageAppServiceId"}
	appServiceCronScheduleColumns    = []string{"id", "name", "title", "crontab", "command", "workload", "envType", "disabled", "updatedAt"}
	appServiceCronJobColumns         = []string{"id", "title", "status", "service", "scheduleId", "task", "createdAt"}
	logStreamColumns                 = []string{"id"}
	routeListColumns                 = []string{"id", "service", "route", "action", "cert", "primary", "private", "status", "updatedAt"}
	routeColumns                     = []string{"id", "route", "host", "path", "pathType", "action", "status", "service", "port", "cert", "certExpiresAt", "main", "primary", "private", "disabled", "redirectScheme", "redirectHost", "redirectPath", "redirectStatusCode", "lastSyncedAt", "createdAt", "updatedAt"}
	appPortListColumns               = []string{"id", "service", "name", "number", "publicPort", "private", "protocol", "updatedAt"}
	appPortColumns                   = []string{"id", "name", "number", "publicPort", "protocol", "private", "service", "instance", "createdAt", "updatedAt"}
	certColumns                      = []string{"id", "host", "status", "issuer", "certType", "expiresAt", "route", "instance", "createdAt"}
	buildListColumns                 = []string{"id", "number", "service", "services", "imageCount", "gitRefType", "gitRef", "startedAt", "duration", "status"}
	buildColumns                     = []string{"id", "number", "status", "instance", "service", "services", "images", "task", "gitRefType", "gitRef", "commitHash", "commitMessage", "createdAt", "startedAt", "endedAt", "duration"}
	deploymentListColumns            = []string{"id", "number", "services", "builds", "startedAt", "duration", "status", "postDeploymentStatus", "rollbackStatus"}
	deploymentColumns                = []string{"id", "number", "status", "postDeploymentStatus", "rollbackStatus", "instance", "services", "images", "task", "postDeploymentTask", "skipRollback", "createdAt", "startedAt", "endedAt", "duration"}
	backupColumns                    = []string{"id", "name", "status", "instance", "service", "database", "databaseDb", "task", "createdAt"}
	importListColumns                = []string{"id", "name", "source", "status", "task", "instance", "service", "database", "databaseDb", "startedAt", "duration"}
	importColumns                    = []string{"id", "name", "source", "status", "task", "instance", "service", "database", "databaseDb", "backup", "createdAt", "updatedAt", "startedAt", "endedAt", "duration"}
	taskColumns                      = []string{"id", "name", "title", "executionScope", "status", "progress", "projects", "author", "startedAt", "duration"}
	taskGetColumns                   = []string{"id", "name", "title", "executionScope", "status", "progress", "projects", "author", "app", "instance", "service", "database", "databaseDb", "originTask", "repeatedTask", "spawnedTasks", "createdAt", "startedAt", "endedAt", "duration"}
	appAccessColumns                 = []string{"id", "mode", "scope", "status", "integrationId", "effectiveUrl", "publicRoutesSuppressed", "lastError", "endpoints", "resources", "createdAt", "updatedAt"}
	appAccessCleanupColumns          = []string{"id", "appAccessId", "appInstanceId", "integrationId", "provider", "status", "attempts", "lastError", "createdAt", "updatedAt"}
	changelogColumns                 = []string{"name", "title", "kind", "previousVersion", "version", "previousRevNumber", "revNumber", "entries"}
	appInstanceStackChangelogColumns = []string{"previousStackVersion", "stackVersion", "previousStackRevNumber", "stackRevNumber", "serviceChanges"}
	clusterInfraAppChangelogColumns  = []string{"appInstanceId", "appName", "appTitle", "previousStackVersion", "stackVersion", "serviceChanges"}
	stackOriginChangelogColumns      = []string{"previousVersion", "version", "entries"}
	taskJobColumns                   = []string{"id", "name", "status", "logStatus", "system", "startedAt", "duration", "steps"}
	taskStepColumns                  = []string{"id", "name", "status", "logStatus", "system", "startedAt", "duration", "job"}
	operationColumns                 = []string{"success", "task"}
)

func Commands() []*cobra.Command {
	return []*cobra.Command{
		newUserCommand(),
		newOrgCommand(),
		newMemberCommand(),
		newProjectCommand(),
		newEnvCommand(),
		newDatabaseCommand(),
		newClusterCommand(),
		newIntegrationCommand(),
		newIntegrationKindCommand(),
		newProviderCommand(),
		newHelmCommand(),
		newStackCommand(),
		newServiceCommand(),
		newAppCommand(),
		newAppInstanceCommand("instance", "Manage app instances"),
		newAppServiceCommand("aps", []string{"app-service", "app-services"}, "Manage app services", instanceFilterFlag),
		newAppRouteCommand("route", []string{"routes"}, "Manage app routes", instanceFilterFlag),
		newAppPortCommand("port", []string{"ports"}, "Manage app ports", instanceFilterFlag),
		newAppCertCommand("cert", []string{"certs", "certificate", "certificates"}, "Manage app certificates", instanceFilterFlag),
		newBuildCommand(),
		newDeploymentCommand(),
		newBackupCommand(),
		newImportCommand(),
		newTaskCommand(),
	}
}

func newOrgCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organization context",
	}
	addOutputFlag(cmd, &out)

	cmd.AddCommand(&cobra.Command{
		Use:   "current",
		Short: "Show organizations available to the current credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/orgs", nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, orgColumns)
		},
	})

	cmd.AddCommand(
		newGetCommand("get ID", "Get organization", "/orgs/", orgColumns, out),
		newOrgUpdateCommand(out),
	)

	return cmd
}

func newUserCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage current user",
	}
	addOutputFlag(cmd, &out)

	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get current user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/user", nil, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, userColumns)
		},
	})

	body := bodyOptions{}
	var name string
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update current user",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"name": name}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/user", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, userColumns)
		},
	}
	addBodyFlags(updateCmd, &body)
	updateCmd.Flags().StringVar(&name, "name", "", "User name")
	cmd.AddCommand(updateCmd)
	return cmd
}

func newOrgUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var title, defaultTimeZone, ciIntegrationID, registryIntegrationID string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				values := map[string]interface{}{"title": title}
				addOptionalString(values, "defaultTimeZone", defaultTimeZone)
				if err := addOptionalInt(values, "ciIntegrationId", ciIntegrationID, "--ci-integration"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "registryIntegrationId", registryIntegrationID, "--registry-integration"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/orgs/"+url.PathEscape(args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, orgColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&title, "title", "", "Organization title")
	cmd.Flags().StringVar(&defaultTimeZone, "default-time-zone", "", "Default time zone")
	cmd.Flags().StringVar(&ciIntegrationID, "ci-integration", "", "Default CI integration ID")
	cmd.Flags().StringVar(&registryIntegrationID, "registry-integration", "", "Default registry integration ID")
	return cmd
}

func newMemberCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "member",
		Aliases: []string{"members"},
		Short:   "Manage organization members",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List organization members",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/org-memberships", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, memberColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd)
	return cmd
}

func newProjectCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/projects", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, projectColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/projects/"+args[0], projectColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newGetByNameCommand("get-by-name NAME", "Get project by name", "/projects/by-name/%s", projectColumns, out, true, false),
		newProjectCreateCommand(out),
		newTitleUpdateCommand("update ID", "Update project", "/projects/", projectColumns, out),
		newDeleteCommand("delete ID", "Delete project", "/projects/", projectColumns, out),
	)
	return cmd
}

func newProjectCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title, role string
	var orgMembershipIDs, teamIDs []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create project",
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				values := map[string]interface{}{
					"name":  name,
					"title": title,
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if len(orgMembershipIDs) != 0 {
					parsed, err := parseIntValues(orgMembershipIDs, "--org-membership")
					if err != nil {
						return err
					}
					values["orgMembershipIds"] = parsed
				}
				if len(teamIDs) != 0 {
					parsed, err := parseIntValues(teamIDs, "--team")
					if err != nil {
						return err
					}
					values["teamIds"] = parsed
				}
				addOptionalString(values, "role", role)
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/projects", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, projectColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&name, "name", "", "Project machine name")
	cmd.Flags().StringVar(&title, "title", "", "Project title")
	cmd.Flags().StringArrayVar(&orgMembershipIDs, "org-membership", nil, "Organization membership ID to grant; repeatable or comma-separated")
	cmd.Flags().StringArrayVar(&teamIDs, "team", nil, "Team ID to grant; repeatable or comma-separated")
	cmd.Flags().StringVar(&role, "role", "", "Project role for granted members or teams")
	return cmd
}

func newEnvCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/envs", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/envs/"+args[0], envColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newGetByNameCommand("get-by-name NAME", "Get environment by name", "/envs/by-name/%s", envColumns, out, true, false),
		newEnvCreateCommand(out),
		newEnvUpdateCommand(out),
		newDeleteCommand("delete ID", "Delete environment", "/envs/", envColumns, out),
	)
	return cmd
}

func newEnvCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title, envType string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create environment",
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(envType, "--type"); err != nil {
					return err
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				orgIDNumber, err := strconv.Atoi(resolvedOrgID)
				if err != nil {
					return errors.WithStack(err)
				}
				requestBody = map[string]interface{}{
					"orgId": orgIDNumber,
					"name":  name,
					"title": title,
					"type":  envType,
				}
			}

			var result interface{}
			if err := client.Post(cmd.Context(), "/envs", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&name, "name", "", "Environment machine name")
	cmd.Flags().StringVar(&title, "title", "", "Environment title")
	cmd.Flags().StringVar(&envType, "type", "", "Environment type: prod, staging, test, dev, or feature")
	return cmd
}

func newEnvUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, title, envType string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update environment",
		Args:  cobra.ExactArgs(1),
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(envType, "--type"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{
					"name":  name,
					"title": title,
					"type":  envType,
				}
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/envs/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Environment machine name")
	cmd.Flags().StringVar(&title, "title", "", "Environment title")
	cmd.Flags().StringVar(&envType, "type", "", "Environment type: prod, staging, test, dev, or feature")
	return cmd
}

func newDatabaseCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "database",
		Aliases: []string{"databases"},
		Short:   "Manage databases",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, kind string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List databases",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "kind", kind)
			var result interface{}
			if err := client.Get(cmd.Context(), "/databases", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&kind, "kind", "", "Database kind")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/databases/"+args[0], databaseColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newGetByNameCommand("get-by-name NAME", "Get database by name", "/databases/by-name/%s", databaseColumns, out, true, false),
		newDatabaseCreateCommand(out),
		newDatabaseUpdateCommand(out),
		newDeleteCommand("delete ID", "Delete database", "/databases/", databaseColumns, out),
		newDatabaseOptionsCommand(out),
	)
	cmd.AddCommand(newDatabaseDbCommand(), newDatabaseUserCommand())
	return cmd
}

func newDatabaseOptionsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "options",
		Short: "Show database options",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "charsets DATABASE_ID",
		Short: "List database charsets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/databases/"+url.PathEscape(args[0])+"/options/charsets", nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseCharsetColumns)
		},
	})
	return cmd
}

func newDatabaseDbCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "db",
		Aliases: []string{"dbs"},
		Short:   "Manage DBs",
	}
	addOutputFlag(cmd, &out)

	listCmd := &cobra.Command{
		Use:   "list DATABASE_ID",
		Short: "List DBs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"databaseId": []string{args[0]}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/database-dbs", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseDbColumns)
		},
	}

	getCmd := newGetCommand("get ID", "Get DB", "/database-dbs/", databaseDbColumns, out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, getCmd, newDatabaseDbCreateCommand(out), newDeleteCommand("delete ID", "Delete DB", "/database-dbs/", databaseDbColumns, out))
	return cmd
}

func newDatabaseDbCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, charset, collation string
	cmd := &cobra.Command{
		Use:   "create DATABASE_ID",
		Short: "Create DB",
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
				databaseID, err := strconv.Atoi(args[0])
				if err != nil {
					return errors.Wrap(err, "invalid DATABASE_ID")
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"databaseId": databaseID,
					"name":       name,
					"charset":    charset,
					"collation":  collation,
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/database-dbs", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseDbColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "DB name")
	cmd.Flags().StringVar(&charset, "charset", "", "DB charset")
	cmd.Flags().StringVar(&collation, "collation", "", "DB collation")
	return cmd
}

func newDatabaseUserCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "user",
		Aliases: []string{"users"},
		Short:   "Manage database users",
	}
	addOutputFlag(cmd, &out)

	listCmd := &cobra.Command{
		Use:   "list DATABASE_ID",
		Short: "List database users",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"databaseId": []string{args[0]}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/database-users", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseUserColumns)
		},
	}

	getCmd := &cobra.Command{
		Use:   "get DATABASE_ID USER_ID",
		Short: "Get database user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{"databaseId": []string{args[0]}}
			rows, err := fetchRows(cmd.Context(), client, "/database-users", query)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if firstScalarPath(row, "id") == args[1] {
					return printClientGetResult(cmd, client, out, row, databaseUserColumns)
				}
			}
			return errors.Errorf("database user %q not found in database %q", args[1], args[0])
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, getCmd, newDatabaseUserCreateCommand(out), newDatabaseUserDBsCommand(out), newDeleteCommand("delete ID", "Delete database user", "/database-users/", databaseUserColumns, out))
	return cmd
}

func newDatabaseUserCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var username, password, hostname string
	var databaseDBIDs []string
	cmd := &cobra.Command{
		Use:   "create DATABASE_ID",
		Short: "Create database user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				databaseID, err := strconv.Atoi(args[0])
				if err != nil {
					return errors.Wrap(err, "invalid DATABASE_ID")
				}
				if err := requireFlag(username, "--username"); err != nil {
					return err
				}
				if err := requireFlag(password, "--password"); err != nil {
					return err
				}
				parsedDBIDs, err := parseIntValues(databaseDBIDs, "--database-db")
				if err != nil {
					return err
				}
				values := map[string]interface{}{
					"databaseId": databaseID,
					"name":       username,
					"password":   password,
					"hostname":   hostname,
				}
				if len(parsedDBIDs) != 0 {
					values["databaseDbIds"] = parsedDBIDs
				}
				requestBody = bodyFromMap(values)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/database-users", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseUserColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&username, "username", "", "Database username")
	cmd.Flags().StringVar(&password, "password", "", "Database user password")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Database user hostname")
	cmd.Flags().StringArrayVar(&databaseDBIDs, "database-db", nil, "DB ID to grant; repeatable or comma-separated")
	return cmd
}

func newDatabaseUserDBsCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var databaseDBIDs []string
	cmd := &cobra.Command{
		Use:     "dbs USER_ID",
		Aliases: []string{"grants"},
		Short:   "Update database user DB grants",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				parsedDBIDs, err := parseIntValues(databaseDBIDs, "--database-db")
				if err != nil {
					return err
				}
				if len(parsedDBIDs) == 0 {
					return errors.New("--database-db is required unless --data/--file is provided")
				}
				requestBody = bodyFromMap(map[string]interface{}{"databaseDbIds": parsedDBIDs})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/database-users/"+args[0]+"/dbs", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseUserColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringArrayVar(&databaseDBIDs, "database-db", nil, "DB ID to grant; repeatable or comma-separated")
	return cmd
}

func newDatabaseCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, envID, integrationKindID, name, title, dbType, version, machineType, region, zone, password, residedClusterID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create database",
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
				if err := requireFlag(envID, "--env"); err != nil {
					return err
				}
				if err := requireFlag(integrationKindID, "--integration-kind"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(dbType, "--type"); err != nil {
					return err
				}
				if err := requireFlag(version, "--version"); err != nil {
					return err
				}
				if err := requireFlag(machineType, "--machine-type"); err != nil {
					return err
				}
				envIDNumber, err := strconv.Atoi(envID)
				if err != nil {
					return errors.Wrap(err, "invalid --env")
				}
				integrationKindIDNumber, err := strconv.Atoi(integrationKindID)
				if err != nil {
					return errors.Wrap(err, "invalid --integration-kind")
				}
				values := map[string]interface{}{
					"envId":             envIDNumber,
					"integrationKindId": integrationKindIDNumber,
					"name":              name,
					"title":             title,
					"type":              dbType,
					"version":           version,
					"machineType":       machineType,
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
				if err := addOptionalInt(values, "residedClusterId", residedClusterID, "--resided-cluster"); err != nil {
					return err
				}
				addOptionalString(values, "region", region)
				addOptionalString(values, "zone", zone)
				addOptionalString(values, "password", password)
				if value, ok := changedBool(cmd, "high-availability"); ok {
					values["highAvailability"] = value
				}
				if value, ok := changedBool(cmd, "storage-autoscaling"); ok {
					values["storageAutoscaling"] = value
				}
				if value, ok := changedInt(cmd, "storage-size"); ok {
					values["storageSize"] = value
				}
				if value, ok := changedInt(cmd, "iops"); ok {
					values["iops"] = value
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/databases", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&envID, "env", "", "Environment ID")
	cmd.Flags().StringVar(&integrationKindID, "integration-kind", "", "Integration kind ID")
	cmd.Flags().StringVar(&name, "name", "", "Database machine name")
	cmd.Flags().StringVar(&title, "title", "", "Database title")
	cmd.Flags().StringVar(&dbType, "type", "", "Database type")
	cmd.Flags().StringVar(&version, "version", "", "Database version")
	cmd.Flags().StringVar(&machineType, "machine-type", "", "Machine type")
	cmd.Flags().StringVar(&region, "region", "", "Provider region")
	cmd.Flags().StringVar(&zone, "zone", "", "Provider zone")
	cmd.Flags().StringVar(&password, "password", "", "Database password")
	cmd.Flags().StringVar(&residedClusterID, "resided-cluster", "", "Resided cluster ID")
	cmd.Flags().Bool("high-availability", false, "Enable high availability")
	cmd.Flags().Bool("storage-autoscaling", false, "Enable storage autoscaling")
	cmd.Flags().Int("storage-size", 0, "Storage size")
	cmd.Flags().Int("iops", 0, "Provisioned IOPS")
	return cmd
}

func newDatabaseUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var title string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"title": title}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/databases/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&title, "title", "", "Database title")
	return cmd
}

func newClusterCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "cluster",
		Aliases: []string{"clusters"},
		Short:   "Manage clusters",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, integrationID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "integrationId", integrationID)
			var result interface{}
			if err := client.Get(cmd.Context(), "/clusters", query, &result); err != nil {
				return err
			}
			return printClusterResult(cmd, client, out, result, clusterColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintCluster(cmd, out, "/clusters/"+url.PathEscape(args[0]), nil)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newClusterGetByNameCommand(out),
		newClusterAppCommand(),
		newClusterCreateCommand(out),
		newClusterUpdateCommand(out),
		newClusterSettingsCommand(out),
		newClusterActionCommand("upgrade-infra ID", "Upgrade cluster infrastructure", "/clusters/%s/actions/upgrade-infra", out),
		newClusterActionCommand("upgrade-infra-apps ID", "Upgrade cluster infrastructure apps", "/clusters/%s/actions/upgrade-infra-apps", out),
		newClusterInfraAppUpgradeChangelogCommand(out),
		newClusterDeleteCommand(out),
	)
	return cmd
}

func newClusterInfraAppUpgradeChangelogCommand(out outputOptions) *cobra.Command {
	var appInstanceID string
	cmd := &cobra.Command{
		Use:   "infra-app-upgrade-changelog ID",
		Short: "Preview cluster infrastructure app upgrades",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "appInstanceId", appInstanceID)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/cluster-infra-app-upgrade-changelogs/%s", args[0]), query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterInfraAppChangelogColumns)
		},
	}
	cmd.Flags().StringVar(&appInstanceID, "app-instance", "", "Limit the preview to one infrastructure app instance ID")
	return cmd
}

func newClusterGetByNameCommand(out outputOptions) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get-by-name NAME",
		Short: "Get cluster by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			return getAndPrintCluster(cmd, out, escapedPath("/clusters/by-name/%s", args[0]), query)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	return cmd
}

func getAndPrintCluster(cmd *cobra.Command, out outputOptions, path string, query url.Values) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, query, &result); err != nil {
		return err
	}
	return printClusterGetResult(cmd, client, out, result, clusterGetColumns)
}

func printClusterResult(cmd *cobra.Command, client *rest.Client, out outputOptions, value interface{}, columns []string) error {
	return printClientResult(cmd, client, out, value, clusterDisplayColumns(cmd, out, normalizeItems(value), columns))
}

func printClusterGetResult(cmd *cobra.Command, client *rest.Client, out outputOptions, value interface{}, columns []string) error {
	return printClientGetResult(cmd, client, out, value, clusterDisplayColumns(cmd, out, normalizeItem(value), columns))
}

func clusterDisplayColumns(cmd *cobra.Command, out outputOptions, value interface{}, columns []string) []string {
	if outputFormat(cmd, out) == outputJSON || clusterRowsHaveRegion(asRows(value)) {
		return columns
	}
	return withoutColumn(columns, "region")
}

func clusterRowsHaveRegion(rows []map[string]interface{}) bool {
	for _, row := range rows {
		if formatColumnValue(row, "region") != "" {
			return true
		}
	}
	return false
}

func withoutColumn(columns []string, column string) []string {
	filtered := make([]string, 0, len(columns))
	for _, existing := range columns {
		if existing != column {
			filtered = append(filtered, existing)
		}
	}
	return filtered
}

func newClusterSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage cluster settings",
	}
	cmd.AddCommand(newRawBodyPutCommand("update ID", "Update cluster settings", "/clusters/settings/%s", clusterColumns, out))
	return cmd
}

func newClusterActionCommand(use string, short string, pathPattern string, out outputOptions) *cobra.Command {
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
			if err := client.Post(cmd.Context(), escapedPath(pathPattern, args[0]), nil, nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
}

func newClusterDeleteCommand(out outputOptions) *cobra.Command {
	var yes, force bool
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Delete cluster?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{}
			if cmd.Flags().Changed("force") {
				query.Set("force", strconv.FormatBool(force))
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), "/clusters/"+url.PathEscape(args[0]), query, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "Force deletion")
	return cmd
}

func newClusterAppCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "app",
		Aliases: []string{"apps", "infra-app", "infra-apps"},
		Short:   "Manage cluster apps",
	}
	addOutputFlag(cmd, &out)

	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list CLUSTER_ID",
		Short: "List cluster apps",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterID := args[0]
			query := url.Values{
				"clusterId":  []string{clusterID},
				"clusterApp": []string{"true"},
			}
			addPagination(query, page, pageSize)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/apps", query, &result); err != nil {
				return err
			}
			result = filterInfraAppRowsByClusterInstances(cmd.Context(), client, result, clusterID, outputFormat(cmd, out) != outputJSON)
			return printClientResult(cmd, client, out, result, infraAppColumns)
		},
	}
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")

	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd)
	return cmd
}

func filterInfraAppRowsByClusterInstances(ctx context.Context, client *rest.Client, value interface{}, clusterID string, enrich bool) interface{} {
	rows := responseRows(value)
	if len(rows) == 0 {
		return value
	}

	instancesByAppID, ok := clusterInfraAppInstancesByAppID(ctx, client, clusterID)
	if !ok {
		if enrich {
			enrichInfraAppRows(ctx, client, normalizeItems(value), clusterID)
		}
		return value
	}

	filtered := filterResponseItems(value, func(row map[string]interface{}) bool {
		appID := firstScalarPath(row, "id", "appId", "app.id")
		return appID != "" && instancesByAppID[appID] != nil
	})
	if enrich {
		enrichInfraAppRowsFromInstances(normalizeItems(filtered), instancesByAppID, nil)
	}
	return filtered
}

func clusterInfraAppInstancesByAppID(ctx context.Context, client *rest.Client, clusterID string) (map[string]map[string]interface{}, bool) {
	query := url.Values{
		"clusterId":  []string{clusterID},
		"clusterApp": []string{"true"},
	}
	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return nil, false
	}

	instancesByAppID := map[string]map[string]interface{}{}
	for _, instance := range responseRows(result) {
		appID := firstRelationID(instance, relationColumns["app"])
		if appID != "" {
			instancesByAppID[appID] = instance
		}
	}
	return instancesByAppID, true
}

func enrichInfraAppRows(ctx context.Context, client *rest.Client, value interface{}, clusterID string) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}

	appIDs := make([]string, len(rows))
	needsInstanceData := false
	for index, row := range rows {
		appIDs[index] = firstScalarPath(row, "id", "appId", "app.id")
		if instanceID := formatInfraAppInstanceIDColumn(row); instanceID != "" {
			row["id"] = instanceID
		}
		if formatInfraAppInstanceIDColumn(row) == "" || !hasStackReference(row) {
			needsInstanceData = true
		}
	}
	if !needsInstanceData {
		return
	}

	query := url.Values{
		"clusterId":  []string{clusterID},
		"clusterApp": []string{"true"},
	}
	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return
	}

	instancesByAppID := map[string]map[string]interface{}{}
	for _, instance := range responseRows(result) {
		appID := firstRelationID(instance, relationColumns["app"])
		if appID != "" {
			instancesByAppID[appID] = instance
		}
	}
	enrichInfraAppRowsFromInstances(value, instancesByAppID, appIDs)
}

func enrichInfraAppRowsFromInstances(value interface{}, instancesByAppID map[string]map[string]interface{}, appIDs []string) {
	rows := asRows(value)
	for index, row := range rows {
		appID := firstScalarPath(row, "id", "appId", "app.id")
		if index < len(appIDs) && appIDs[index] != "" {
			appID = appIDs[index]
		}
		instance := instancesByAppID[appID]
		if instance == nil {
			continue
		}
		if instanceID := firstScalarPath(instance, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id"); instanceID != "" {
			row["id"] = instanceID
		}
		if hasStackReference(row) {
			continue
		}
		if stack := firstStackObject(instance); stack != nil {
			row["stack"] = stack
			continue
		}
		if stackID := firstRelationID(instance, relationColumns["stack"]); stackID != "" {
			row["stackId"] = stackID
		}
	}
}

func newClusterCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, integrationID, name, title, region, zone, version, machineType, billingOption string
	var serverless, disableMonitoring bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create cluster",
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
				if err := requireFlag(integrationID, "--integration"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				integrationIDNumber, err := strconv.Atoi(integrationID)
				if err != nil {
					return errors.Wrap(err, "invalid --integration")
				}
				values := map[string]interface{}{
					"integrationId":     integrationIDNumber,
					"name":              name,
					"title":             title,
					"serverless":        serverless,
					"disableMonitoring": disableMonitoring,
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
				addOptionalString(values, "region", region)
				addOptionalString(values, "zone", zone)
				addOptionalString(values, "version", version)
				addOptionalString(values, "machineType", machineType)
				addOptionalString(values, "billingOption", billingOption)
				if value, ok := changedInt(cmd, "min-node-count"); ok {
					values["minNodeCount"] = value
				}
				if value, ok := changedInt(cmd, "max-node-count"); ok {
					values["maxNodeCount"] = value
				}
				if value, ok := changedInt(cmd, "node-disk-size"); ok {
					values["nodeDiskSize"] = value
				}
				if value, ok := changedBool(cmd, "single-node"); ok {
					values["singleNode"] = value
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/clusters", nil, requestBody, &result); err != nil {
				return err
			}
			if handled, err := printCreatedResourceTaskLogs(cmd.Context(), cmd, client, out, result, "cluster", "clusterId"); handled || err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")
	cmd.Flags().StringVar(&name, "name", "", "Cluster machine name")
	cmd.Flags().StringVar(&title, "title", "", "Cluster title")
	cmd.Flags().BoolVar(&serverless, "serverless", false, "Create a serverless cluster")
	cmd.Flags().BoolVar(&disableMonitoring, "disable-monitoring", false, "Disable cluster monitoring")
	cmd.Flags().StringVar(&region, "region", "", "Provider region")
	cmd.Flags().StringVar(&zone, "zone", "", "Provider zone")
	cmd.Flags().StringVar(&version, "version", "", "Kubernetes version")
	cmd.Flags().StringVar(&machineType, "machine-type", "", "Machine type")
	cmd.Flags().StringVar(&billingOption, "billing-option", "", "Provider billing option")
	cmd.Flags().Int("min-node-count", 0, "Minimum node count")
	cmd.Flags().Int("max-node-count", 0, "Maximum node count")
	cmd.Flags().Int("node-disk-size", 0, "Node disk size")
	cmd.Flags().Bool("single-node", false, "Create a single-node cluster")
	return cmd
}

func newClusterUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var title string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"title": title}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/clusters/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&title, "title", "", "Cluster title")
	return cmd
}

func newIntegrationCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "integration",
		Aliases: []string{"integrations"},
		Short:   "Manage integrations",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, labels string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List integrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "labels", labels)
			var result interface{}
			if err := client.Get(cmd.Context(), "/integrations", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&labels, "labels", "", "Comma-separated labels")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/integrations/"+args[0], integrationColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newGetByNameCommand("get-by-name NAME", "Get integration by name", "/integrations/by-name/%s", integrationColumns, out, true, false),
		newIntegrationCreateCommand(out),
		newIntegrationUpdateCommand(out),
		newIntegrationConfigureCommand(out),
		newClusterActionCommand("test-permissions ID", "Test integration permissions", "/integrations/%s/actions/test-permissions", out),
		newRawBodyPostCommand("validate-app-access-hostname ID", "Validate an app-access hostname", "/integrations/%s/actions/validate-app-access-hostname", []string{"valid"}, out),
		newIntegrationOptionsCommand(out),
		newDeleteCommand("delete ID", "Delete integration", "/integrations/", integrationColumns, out),
	)
	return cmd
}

func newIntegrationCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, providerID, name, title, auth, scope string
	var kinds, fields []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create integration",
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
				if err := requireFlag(providerID, "--provider"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if len(kinds) == 0 {
					return errors.New("--kind is required")
				}
				providerIDNumber, err := strconv.Atoi(providerID)
				if err != nil {
					return errors.Wrap(err, "invalid --provider")
				}
				values := map[string]interface{}{
					"providerId": providerIDNumber,
					"name":       name,
					"title":      title,
					"kinds":      kinds,
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
				addOptionalString(values, "auth", auth)
				addOptionalString(values, "scope", scope)
				if err := addOptionalNameValueInputs(values, "fieldsInput", fields, "--field"); err != nil {
					return err
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/integrations", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&providerID, "provider", "", "Provider ID")
	cmd.Flags().StringVar(&name, "name", "", "Integration machine name")
	cmd.Flags().StringVar(&title, "title", "", "Integration title")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Integration kind; repeatable")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "Provider field as NAME=VALUE; repeatable")
	cmd.Flags().StringVar(&auth, "auth", "", "Integration auth mode")
	cmd.Flags().StringVar(&scope, "scope", "", "Integration scope")
	return cmd
}

func newIntegrationUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, title, scope string
	var kinds, fields []string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update integration",
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
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if len(kinds) == 0 {
					return errors.New("--kind is required")
				}
				values := map[string]interface{}{
					"name":  name,
					"title": title,
					"kinds": kinds,
				}
				addOptionalString(values, "scope", scope)
				if err := addOptionalNameValueInputs(values, "fieldsInput", fields, "--field"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/integrations/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Integration machine name")
	cmd.Flags().StringVar(&title, "title", "", "Integration title")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Integration kind; repeatable")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "Provider field as NAME=VALUE; repeatable")
	cmd.Flags().StringVar(&scope, "scope", "", "Integration scope")
	return cmd
}

func newIntegrationConfigureCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, title, scope string
	var kinds, fields []string
	cmd := &cobra.Command{
		Use:   "configure ID",
		Short: "Configure integration and reconcile provider resources",
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
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if len(kinds) == 0 {
					return errors.New("--kind is required")
				}
				values := map[string]interface{}{"name": name, "title": title, "kinds": kinds}
				addOptionalString(values, "scope", scope)
				if err := addOptionalNameValueInputs(values, "fieldsInput", fields, "--field"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/integrations/configuration/"+url.PathEscape(args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, []string{"integration", "taskId", "warnings"})
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Integration machine name")
	cmd.Flags().StringVar(&title, "title", "", "Integration title")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Integration kind; repeatable")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "Provider field as NAME=VALUE; repeatable")
	cmd.Flags().StringVar(&scope, "scope", "", "Integration scope")
	return cmd
}

func newIntegrationOptionsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "options",
		Short: "Show integration options",
	}
	cmd.AddCommand(
		newIntegrationOptionCommand("scopes INTEGRATION_ID", "List integration scopes", "/integrations/%s/options/scopes", out, nil, nil),
		newIntegrationOptionCommand("storage-buckets INTEGRATION_ID", "List storage buckets", "/integrations/%s/options/storage-buckets", out, nil, nil),
		newIntegrationOptionCommand("storage-classes INTEGRATION_ID", "List storage classes", "/integrations/%s/options/storage-classes", out, nil, nil),
		newIntegrationOptionCommand("remote-git-repos INTEGRATION_ID", "List remote Git repositories", "/integrations/%s/options/remote-git-repos", out, nil, nil),
		newIntegrationOptionCommand("remote-git-repo-file INTEGRATION_ID", "Check a file in a remote Git repository", "/integrations/%s/options/remote-git-repo-file", out, []queryFlag{{name: "remote-git-repo", queryName: "remoteGitRepoId", usage: "Remote Git repository ID", required: true}, {name: "path", queryName: "path", usage: "Repository file path", required: true}, {name: "ref", queryName: "ref", usage: "Exact Git ref", required: true}}, []string{"exists"}),
		newIntegrationOptionCommand("remote-git-repo-branches INTEGRATION_ID", "List remote Git repository branches", "/integrations/%s/options/remote-git-repo-branches", out, []queryFlag{{name: "remote-git-repo", queryName: "remoteGitRepoId", usage: "Remote Git repository ID", required: true}}, nil),
		newIntegrationOptionCommand("remote-git-repo-tags INTEGRATION_ID", "List remote Git repository tags", "/integrations/%s/options/remote-git-repo-tags", out, []queryFlag{{name: "remote-git-repo", queryName: "remoteGitRepoId", usage: "Remote Git repository ID", required: true}}, nil),
		newIntegrationOptionCommand("kube-regions INTEGRATION_ID", "List Kubernetes regions", "/integrations/%s/options/kube-regions", out, nil, nil),
		newIntegrationOptionCommand("kube-zones INTEGRATION_ID", "List Kubernetes zones", "/integrations/%s/options/kube-zones", out, nil, nil),
		newIntegrationOptionCommand("kube-versions INTEGRATION_ID", "List Kubernetes versions", "/integrations/%s/options/kube-versions", out, []queryFlag{{name: "location", queryName: "location", usage: "Provider location"}}, nil),
		newIntegrationOptionCommand("kube-machine-types INTEGRATION_ID", "List Kubernetes machine types", "/integrations/%s/options/kube-machine-types", out, []queryFlag{{name: "location", queryName: "location", usage: "Provider location"}}, nil),
		newIntegrationOptionCommand("kube-settings INTEGRATION_ID", "Get Kubernetes settings", "/integrations/%s/options/kube-settings", out, nil, nil),
		newIntegrationOptionCommand("app-access INTEGRATION_ID", "Show app-access provider options", "/integrations/%s/options/app-access", out, nil, []string{"provider", "modes", "scopes", "endpointHostMode", "fields", "configurations"}),
	)
	return cmd
}

type queryFlag struct {
	name      string
	queryName string
	usage     string
	required  bool
}

func newIntegrationOptionCommand(use string, short string, pathPattern string, out outputOptions, flags []queryFlag, columns []string) *cobra.Command {
	values := make(map[string]*string, len(flags))
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			for _, flag := range flags {
				value := strings.TrimSpace(*values[flag.name])
				if flag.required {
					if err := requireFlag(value, "--"+flag.name); err != nil {
						return err
					}
				}
				addQuery(query, flag.queryName, value)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath(pathPattern, args[0]), query, &result); err != nil {
				return err
			}
			return printOptionResult(cmd, client, out, result, columns)
		},
	}
	for _, flag := range flags {
		value := ""
		values[flag.name] = &value
		cmd.Flags().StringVar(values[flag.name], flag.name, "", flag.usage)
	}
	return cmd
}

func newIntegrationKindCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "integration-kind",
		Aliases: []string{"integration-kinds"},
		Short:   "Show integration kind options",
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(
		newIntegrationKindOptionCommand("database-types KIND_ID", "List database types", "/integration-kinds/%s/database-types", out, nil, nil),
		newIntegrationKindOptionCommand("database-versions KIND_ID", "List database versions", "/integration-kinds/%s/database-versions", out, []queryFlag{{name: "type", queryName: "dbType", usage: "Database type"}}, nil),
		newIntegrationKindOptionCommand("database-regions KIND_ID", "List database regions", "/integration-kinds/%s/database-regions", out, databaseOptionQueryFlags(false), nil),
		newIntegrationKindOptionCommand("database-machine-types KIND_ID", "List database machine types", "/integration-kinds/%s/database-machine-types", out, databaseOptionQueryFlags(true), nil),
		newIntegrationKindOptionCommand("database-settings KIND_ID", "Get database settings", "/integration-kinds/%s/database-settings", out, []queryFlag{{name: "type", queryName: "dbType", usage: "Database type"}}, nil),
	)
	return cmd
}

func databaseOptionQueryFlags(includeLocation bool) []queryFlag {
	flags := []queryFlag{
		{name: "type", queryName: "dbType", usage: "Database type"},
		{name: "version", queryName: "version", usage: "Database version"},
		{name: "ha", queryName: "ha", usage: "High availability option"},
	}
	if includeLocation {
		flags = append(flags,
			queryFlag{name: "region", queryName: "region", usage: "Provider region"},
			queryFlag{name: "zone", queryName: "zone", usage: "Provider zone"},
		)
	}
	return flags
}

func newIntegrationKindOptionCommand(use string, short string, pathPattern string, out outputOptions, flags []queryFlag, columns []string) *cobra.Command {
	return newIntegrationOptionCommand(use, short, pathPattern, out, flags, columns)
}

func newProviderCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "provider",
		Aliases: []string{"providers"},
		Short:   "Manage providers",
	}
	addOutputFlag(cmd, &out)
	listCmd := newProviderListCommand(out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newProviderGetCommand(out))
	addProviderCommands(cmd, out)
	return cmd
}

func addProviderCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(
		newProviderGetByNameCommand(out),
		newGetCommand("revision ID", "Get provider revision", "/provider-revisions/", providerRevisionColumns, out),
	)
}

func newProviderListCommand(out outputOptions) *cobra.Command {
	var orgID, projectIDs, search string
	var page, pageSize int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "search", search)
			addPagination(query, page, pageSize)
			addBoolQuery(cmd, query, "excludePublic", "exclude-public")
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/providers", query, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) != outputJSON {
				enrichProviderRevisionSummary(cmd.Context(), client, normalizeItems(result))
			}
			return printClientResult(cmd, client, out, result, providerColumns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	cmd.Flags().StringVar(&search, "search", "", "Search query")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	cmd.Flags().Bool("exclude-public", false, "Exclude public resources")
	return cmd
}

func newProviderGetCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintProvider(cmd, out, "/providers/"+url.PathEscape(args[0]), nil)
		},
	}
}

func newProviderGetByNameCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get-by-name NAME",
		Short: "Get provider by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintProvider(cmd, out, escapedPath("/providers/by-name/%s", args[0]), nil)
		},
	}
}

func getAndPrintProvider(cmd *cobra.Command, out outputOptions, path string, query url.Values) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, query, &result); err != nil {
		return err
	}
	if outputFormat(cmd, out) != outputJSON {
		enrichProviderRevisionSummary(cmd.Context(), client, normalizeItem(result))
	}
	return printClientGetResult(cmd, client, out, result, providerColumns)
}

func enrichProviderRevisionSummary(ctx context.Context, client *rest.Client, value interface{}) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}

	cache := map[string]map[string]interface{}{}
	for _, row := range rows {
		if formatProviderVersionColumn(row) != "" && firstScalarPath(row, "providerRevNumber", "providerRevision.number", "providerRev.number", "revNumber", "latestRevNumber") != "" {
			continue
		}
		revID := firstScalarPath(row, "providerRevId", "providerRev.id", "providerRevision.id", "revId", "latestRevId")
		if revID == "" {
			continue
		}
		revision, ok := cache[revID]
		if !ok {
			var result interface{}
			if err := client.Get(ctx, "/provider-revisions/"+url.PathEscape(revID), nil, &result); err != nil {
				cache[revID] = nil
				continue
			}
			revisions := responseRows(result)
			if len(revisions) == 0 {
				cache[revID] = nil
				continue
			}
			revision = revisions[0]
			cache[revID] = revision
		}
		if revision != nil {
			row["providerRevision"] = revision
		}
	}
}

func newStackCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "stack",
		Aliases: []string{"stacks"},
		Short:   "Manage stacks",
	}
	addOutputFlag(cmd, &out)
	listCmd := newStackListCommand(out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		newStackGetCommand(out),
		newStackGetByNameCommand(out),
		newCatalogImportFromGitCommand("import", "Import stacks from Git", "/stacks/actions/import", operationColumns, out),
		newManifestValidateCommand("stack", out),
		newManifestCreateCommand("stack", out, stackColumns),
		newStackSettingsCommand(out),
		newStackRevisionCommand(out),
		newStackPublishDraftCommand(out),
		newStackUpdateFromGitCommand(out),
		newClusterActionCommand("update-service-revisions ID", "Update stack service revisions", "/stacks/%s/actions/update-service-revisions", out),
		newStackServiceUpdateChangelogCommand(out),
		newGetCommand("origin-sync-changelog ID", "Preview stack origin synchronization", "/stack-origin-sync-changelogs/", stackOriginChangelogColumns, out),
		newStackDuplicateCommand(out),
		newStackSyncOriginCommand(out),
		newStackServiceCommand(out),
	)
	return cmd
}

func newStackServiceUpdateChangelogCommand(out outputOptions) *cobra.Command {
	var stackServiceID string
	cmd := &cobra.Command{
		Use:   "service-update-changelog ID",
		Short: "Preview stack service revision updates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "stackServiceId", stackServiceID)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/stack-service-update-changelogs/%s", args[0]), query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, changelogColumns)
		},
	}
	cmd.Flags().StringVar(&stackServiceID, "stack-service", "", "Limit the preview to one stack service ID")
	return cmd
}

func newStackListCommand(out outputOptions) *cobra.Command {
	var orgID, projectIDs, search string
	var page, pageSize int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stacks",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "search", search)
			addPagination(query, page, pageSize)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/stacks", query, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) != outputJSON {
				enrichStackRevisionSummary(cmd.Context(), client, normalizeItems(result))
			}
			return printClientResult(cmd, client, out, result, stackColumns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	cmd.Flags().StringVar(&search, "search", "", "Search query")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	return cmd
}

func newStackGetCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintStack(cmd, out, "/stacks/"+url.PathEscape(args[0]), nil)
		},
	}
}

func newStackGetByNameCommand(out outputOptions) *cobra.Command {
	var revNumber int
	cmd := &cobra.Command{
		Use:   "get-by-name NAME",
		Short: "Get stack by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			if revNumber != 0 {
				query.Set("revNumber", strconv.Itoa(revNumber))
			}
			return getAndPrintStack(cmd, out, stackByNamePath(args[0]), query)
		},
	}
	cmd.Flags().IntVar(&revNumber, "rev", 0, "Revision number")
	return cmd
}

func getAndPrintStack(cmd *cobra.Command, out outputOptions, path string, query url.Values) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, query, &result); err != nil {
		return err
	}
	if outputFormat(cmd, out) != outputJSON {
		enrichStackRevisionSummary(cmd.Context(), client, normalizeItem(result))
		enrichStackServicesSummary(cmd.Context(), client, normalizeItem(result))
	}
	return printClientGetResult(cmd, client, out, result, stackGetColumns)
}

func enrichStackRevisionSummary(ctx context.Context, client *rest.Client, value interface{}) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}

	cache := make(map[string]map[string]interface{})
	for _, row := range rows {
		if formatCurrentRevisionNumber(row) != "" && formatColumnValue(row, "currentVersion") != "" {
			continue
		}
		revID := firstScalarPath(row, "revId", "stackRevId", "currentRevId", "currentStackRevId", "rev.id", "stackRev.id", "stackRevision.id")
		if revID == "" {
			continue
		}
		revision, ok := cache[revID]
		if !ok {
			var result interface{}
			if err := client.Get(ctx, "/stack-revisions/"+url.PathEscape(revID), nil, &result); err != nil {
				cache[revID] = nil
				continue
			}
			revisions := responseRows(result)
			if len(revisions) == 0 {
				cache[revID] = nil
				continue
			}
			revision = revisions[0]
			cache[revID] = revision
		}
		if revision != nil {
			row["stackRevision"] = revision
		}
	}
}

func enrichStackServicesSummary(ctx context.Context, client *rest.Client, value interface{}) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}

	row := rows[0]
	if services := firstNonNilPath(row, "services", "stackServices"); services != nil {
		row["services"] = summarizeStackServices(responseRows(services))
		return
	}

	revID := firstScalarPath(row, "revId", "stackRevId", "currentRevId", "currentStackRevId", "rev.id", "stackRev.id", "stackRevision.id")
	if revID == "" {
		return
	}

	query := url.Values{"stackRevId": []string{revID}}
	var result interface{}
	if err := client.Get(ctx, "/stack-services", query, &result); err != nil {
		return
	}
	row["services"] = summarizeStackServices(responseRows(result))
}

func summarizeStackServices(rows []map[string]interface{}) string {
	disabled := 0
	for _, row := range rows {
		if truthyPath(row, "disabled") {
			disabled++
		}
	}
	return fmt.Sprintf("%s (%d disabled)", pluralizeCount(len(rows), "service", "services"), disabled)
}

func newStackSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage stack settings",
	}
	cmd.AddCommand(newRawBodyPutCommand("update ID", "Update stack settings", "/stacks/settings/%s", stackColumns, out))
	return cmd
}

func newStackRevisionCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "revision",
		Aliases: []string{"revisions"},
		Short:   "Read stack revisions",
	}
	servicesCmd := &cobra.Command{
		Use:   "services ID",
		Short: "List stack revision services",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/stack-revisions/"+url.PathEscape(args[0])+"/services", nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackServiceColumns)
		},
	}
	cmd.AddCommand(
		newGetCommand("get ID", "Get stack revision", "/stack-revisions/", stackRevisionColumns, out),
		servicesCmd,
	)
	return cmd
}

func newStackPublishDraftCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "publish-draft ID",
		Short: "Publish stack draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stacks/"+url.PathEscape(args[0])+"/actions/publish-draft", nil, nil, &result); err != nil {
				return err
			}
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				return printClientResult(cmd, client, out, result, taskColumns)
			}
			return printClientResult(cmd, client, out, result, stackColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newStackUpdateFromGitCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var gitRef, gitRefType string
	cmd := &cobra.Command{
		Use:   "update-from-git ID",
		Short: "Update stack from Git",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(gitRef, "--git-ref"); err != nil {
					return err
				}
				if err := requireFlag(gitRefType, "--git-ref-type"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{
					"gitRef":     gitRef,
					"gitRefType": gitRefType,
				}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stacks/"+url.PathEscape(args[0])+"/actions/update-from-git", nil, requestBody, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&gitRef, "git-ref", "", "Git ref")
	cmd.Flags().StringVar(&gitRefType, "git-ref-type", "", "Git ref type")
	return cmd
}

func newStackDuplicateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID string
	cmd := &cobra.Command{
		Use:   "duplicate ID",
		Short: "Duplicate stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			if !hasBody {
				values := map[string]interface{}{}
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
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stacks/"+url.PathEscape(args[0])+"/actions/duplicate", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Target organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Target project ID")
	return cmd
}

func newStackSyncOriginCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "sync-origin ID",
		Short: "Sync stack with origin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				requestBody = changedStackSyncOptions(cmd)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stacks/"+url.PathEscape(args[0])+"/actions/sync-origin", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addStackSyncFlags(cmd)
	return cmd
}

func addStackSyncFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("delete-helm-values", false, "Delete stack Helm values missing from origin")
	cmd.Flags().Bool("delete-env-vars", false, "Delete stack environment variables missing from origin")
	cmd.Flags().Bool("delete-tokens", false, "Delete stack tokens missing from origin")
	cmd.Flags().Bool("delete-annotations", false, "Delete stack annotations missing from origin")
	cmd.Flags().Bool("delete-services", false, "Delete stack services missing from origin")
	cmd.Flags().Bool("delete-service-config", false, "Delete stack service configuration missing from origin")
}

func changedStackSyncOptions(cmd *cobra.Command) interface{} {
	values := map[string]interface{}{}
	for flagName, bodyName := range map[string]string{
		"delete-helm-values":    "deleteStackHelmValues",
		"delete-env-vars":       "deleteStackEnvVars",
		"delete-tokens":         "deleteStackTokens",
		"delete-annotations":    "deleteStackAnnotations",
		"delete-services":       "deleteStackServices",
		"delete-service-config": "deleteStackServicesConfiguration",
	} {
		if value, ok := changedBool(cmd, flagName); ok {
			values[bodyName] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func newCatalogImportFromGitCommand(use string, short string, path string, columns []string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var orgID, projectID, integrationID, remoteGitRepoID, gitRef, gitRefType string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(integrationID, "--integration"); err != nil {
					return err
				}
				if err := requireFlag(remoteGitRepoID, "--remote-git-repo"); err != nil {
					return err
				}
				if err := requireFlag(gitRef, "--git-ref"); err != nil {
					return err
				}
				if err := requireFlag(gitRefType, "--git-ref-type"); err != nil {
					return err
				}
				values := map[string]interface{}{
					"remoteGitRepoId": remoteGitRepoID,
					"gitRef":          gitRef,
					"gitRefType":      gitRefType,
				}
				if err := addOptionalInt(values, "integrationId", integrationID, "--integration"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", orgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), path, nil, requestBody, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if len(columns) != 0 {
				resultColumns = columns
			}
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&integrationID, "integration", "", "Git integration ID")
	cmd.Flags().StringVar(&remoteGitRepoID, "remote-git-repo", "", "Remote Git repository ID")
	cmd.Flags().StringVar(&gitRef, "git-ref", "", "Git ref")
	cmd.Flags().StringVar(&gitRefType, "git-ref-type", "", "Git ref type")
	return cmd
}

func newStackServiceCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service",
		Aliases: []string{"services", "stack-service", "stack-services"},
		Short:   "Manage stack services",
	}

	var stackID, stackRevID, stackSelectorID string
	listCmd := &cobra.Command{
		Use:   "list [STACK_REV_ID|STACK_ID]",
		Short: "List stack services",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				stackSelectorID = args[0]
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedStackRevID, err := resolveStackServiceListStackRevID(cmd.Context(), client, stackID, stackRevID, stackSelectorID)
			if err != nil {
				return err
			}
			query := url.Values{"stackRevId": []string{resolvedStackRevID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/stack-services", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackServiceColumns)
		},
	}
	listCmd.Flags().StringVar(&stackID, "stack", "", "Stack ID; uses the stack current revision")
	listCmd.Flags().StringVar(&stackRevID, "stack-rev", "", "Stack revision ID")
	defaultToList(cmd, listCmd)

	cmd.AddCommand(
		listCmd,
		newStackServiceCreateCommand(out),
		newStackServiceUpdateCommand(out),
		newDeleteCommand("delete ID", "Delete stack service", "/stack-services/", stackServiceColumns, out),
		newStackServiceEnvVarCommand(out),
		newStackServiceHelmValueCommand(out),
		newStackServiceTokenCommand(out),
		newStackServiceAnnotationCommand(out),
		newStackServiceIntegrationCommand(out),
		newStackServiceLinkCommand(out),
		newStackServiceVolumeCommand(out),
		newStackServiceSettingCommand(out),
		newStackServiceConfigCommand(out),
		newStackServiceResourcesCommand(out),
		newStackServiceOptionsCommand(out),
		newStackServiceCronScheduleCommand(out),
	)
	return cmd
}

func resolveStackServiceListStackRevID(ctx context.Context, client *rest.Client, stackID string, stackRevID string, stackSelectorID string) (string, error) {
	selectors := 0
	for _, value := range []string{stackID, stackRevID, stackSelectorID} {
		if value != "" {
			selectors++
		}
	}
	if selectors > 1 {
		return "", errors.New("use only one of ID argument, --stack, or --stack-rev")
	}
	if stackRevID != "" {
		return stackRevID, nil
	}
	if stackID != "" {
		return currentStackRevID(ctx, client, stackID)
	}
	if stackSelectorID != "" {
		resolvedStackRevID, err := currentStackRevID(ctx, client, stackSelectorID)
		if err == nil {
			return resolvedStackRevID, nil
		}
		if isNotFoundAPIError(err) {
			return stackSelectorID, nil
		}
		return "", err
	}
	return "", errors.New("one of ID argument, --stack, or --stack-rev is required")
}

func currentStackRevID(ctx context.Context, client *rest.Client, stackID string) (string, error) {
	var stack interface{}
	if err := client.Get(ctx, "/stacks/"+url.PathEscape(stackID), nil, &stack); err != nil {
		return "", err
	}
	return stackCurrentRevID(stack, stackID)
}

func resolveAppID(ctx context.Context, client *rest.Client, app string, orgID string) (string, error) {
	return resolveIDOrName(ctx, client, app, "--app", "/apps/by-name/%s", orgID, "id", "appId")
}

func resolveEnvID(ctx context.Context, client *rest.Client, env string, orgID string) (string, error) {
	return resolveIDOrName(ctx, client, env, "--env", "/envs/by-name/%s", orgID, "id", "envId", "environmentId")
}

func resolveClusterID(ctx context.Context, client *rest.Client, cluster string, orgID string) (string, error) {
	return resolveIDOrName(ctx, client, cluster, "--cluster", "/clusters/by-name/%s", orgID, "id", "clusterId")
}

func resolveIDOrName(ctx context.Context, client *rest.Client, value string, flag string, byNamePath string, orgID string, idPaths ...string) (string, error) {
	if _, err := strconv.Atoi(value); err == nil {
		return value, nil
	}

	query := url.Values{}
	addQuery(query, "orgId", orgID)
	var result interface{}
	if err := client.Get(ctx, escapedPath(byNamePath, value), query, &result); err != nil {
		return "", errors.Wrapf(err, "resolve %s %q", flag, value)
	}
	rows := responseRows(result)
	if len(rows) == 0 {
		return "", errors.Errorf("%s %q response did not include an item", flag, value)
	}
	id := firstScalarPath(rows[0], idPaths...)
	if id == "" {
		return "", errors.Errorf("%s %q response did not include an id", flag, value)
	}
	return id, nil
}

func resolveCreateStackRevID(ctx context.Context, client *rest.Client, stack string, stackRevID string, orgID string) (string, error) {
	if stackRevID != "" {
		return stackRevID, nil
	}
	if stack == "" {
		return "", errors.New("one of --stack or --stack-rev is required")
	}
	if _, err := strconv.Atoi(stack); err == nil {
		resolvedStackRevID, err := currentStackRevID(ctx, client, stack)
		if err != nil {
			return "", errors.Wrapf(err, "resolve --stack %q", stack)
		}
		return resolvedStackRevID, nil
	}

	return stackNameCurrentRevID(ctx, client, stack, orgID)
}

func stackNameCurrentRevID(ctx context.Context, client *rest.Client, stack string, orgID string) (string, error) {
	result, err := getStackByName(ctx, client, stack, orgID)
	if err == nil {
		return stackCurrentRevID(result, stack)
	}
	if !isNotFoundAPIError(err) || strings.Contains(stack, "/") {
		return "", err
	}

	orgName, resolvedOrgID, orgErr := currentOrgName(ctx, client, orgID)
	if orgErr != nil {
		return "", errors.Wrap(orgErr, "resolve current organization for --stack prefix")
	}
	prefixedStack := orgName + "/" + stack
	result, err = getStackByName(ctx, client, prefixedStack, firstScalar(resolvedOrgID, orgID))
	if err != nil {
		return "", err
	}
	return stackCurrentRevID(result, prefixedStack)
}

func getStackByName(ctx context.Context, client *rest.Client, stack string, orgID string) (interface{}, error) {
	query := url.Values{}
	addQuery(query, "orgId", orgID)
	var result interface{}
	if err := client.Get(ctx, stackByNamePath(stack), query, &result); err != nil {
		return nil, errors.Wrapf(err, "resolve --stack %q", stack)
	}
	return result, nil
}

func currentOrgName(ctx context.Context, client *rest.Client, orgID string) (string, string, error) {
	var result interface{}
	if orgID != "" {
		if err := client.Get(ctx, "/orgs/"+url.PathEscape(orgID), nil, &result); err != nil {
			return "", "", errors.Wrapf(err, "get organization %q", orgID)
		}
	} else {
		if err := client.Get(ctx, "/orgs", nil, &result); err != nil {
			return "", "", errors.Wrap(err, "list organizations")
		}
	}

	rows := responseRows(result)
	if orgID == "" {
		if len(rows) == 0 {
			return "", "", errors.New("no organization is available for the current credentials")
		}
		if len(rows) > 1 {
			return "", "", errors.New("multiple organizations are available; pass --org explicitly")
		}
	}
	if len(rows) == 0 {
		return "", "", errors.Errorf("organization %q response did not include an item", orgID)
	}

	name := firstScalarPath(rows[0], "name")
	if name == "" {
		return "", "", errors.Errorf("organization %q response did not include a name", orgID)
	}
	resolvedOrgID := firstScalarPath(rows[0], "id")
	return name, resolvedOrgID, nil
}

func stackCurrentRevID(stack interface{}, selector string) (string, error) {
	rows := asRows(normalizeItem(stack))
	if len(rows) == 0 {
		return "", errors.Errorf("stack %q response did not include a current revision", selector)
	}
	resolvedStackRevID := firstScalarPath(rows[0], "revId", "stackRevId", "currentRevId", "currentStackRevId", "rev.id", "stackRev.id", "stackRevision.id", "currentRev.id", "currentStackRev.id", "currentStackRevision.id")
	if resolvedStackRevID == "" {
		return "", errors.Errorf("stack %q response did not include revId", selector)
	}
	return resolvedStackRevID, nil
}

func isNotFoundAPIError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "404") || strings.Contains(strings.ToLower(message), "not found")
}

type jsonFlagKind string

const (
	jsonFlagString jsonFlagKind = "string"
	jsonFlagInt    jsonFlagKind = "int"
	jsonFlagBool   jsonFlagKind = "bool"
)

type jsonFlagSpec struct {
	name           string
	jsonName       string
	usage          string
	kind           jsonFlagKind
	required       bool
	always         bool
	requireChanged bool
}

func stringJSONFlag(name string, jsonName string, usage string, required bool) jsonFlagSpec {
	return jsonFlagSpec{name: name, jsonName: jsonName, usage: usage, kind: jsonFlagString, required: required}
}

func intJSONFlag(name string, jsonName string, usage string, required bool) jsonFlagSpec {
	return jsonFlagSpec{name: name, jsonName: jsonName, usage: usage, kind: jsonFlagInt, required: required}
}

func boolJSONFlag(name string, jsonName string, usage string, always bool) jsonFlagSpec {
	return jsonFlagSpec{name: name, jsonName: jsonName, usage: usage, kind: jsonFlagBool, always: always}
}

func requiredChangedBoolJSONFlag(name string, jsonName string, usage string) jsonFlagSpec {
	return jsonFlagSpec{name: name, jsonName: jsonName, usage: usage, kind: jsonFlagBool, requireChanged: true}
}

func addJSONFlagSpecs(cmd *cobra.Command, specs []jsonFlagSpec) {
	for _, spec := range specs {
		switch spec.kind {
		case jsonFlagString:
			cmd.Flags().String(spec.name, "", spec.usage)
		case jsonFlagInt:
			cmd.Flags().Int(spec.name, 0, spec.usage)
		case jsonFlagBool:
			cmd.Flags().Bool(spec.name, false, spec.usage)
		}
	}
}

func readBodyOrJSONFlags(cmd *cobra.Command, opts bodyOptions, specs []jsonFlagSpec, requireAnyChanged bool) (interface{}, error) {
	requestBody, hasBody, err := readBody(opts)
	if err != nil {
		return nil, err
	}
	if hasBody {
		return requestBody, nil
	}
	return bodyFromJSONFlagSpecs(cmd, specs, requireAnyChanged)
}

func bodyFromJSONFlagSpecs(cmd *cobra.Command, specs []jsonFlagSpec, requireAnyChanged bool) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	changed := false
	for _, spec := range specs {
		if spec.jsonName == "" {
			spec.jsonName = spec.name
		}
		flagName := "--" + spec.name
		if spec.requireChanged && !cmd.Flags().Changed(spec.name) {
			return nil, errors.Errorf("%s is required", flagName)
		}
		include := spec.always || spec.required || cmd.Flags().Changed(spec.name)
		if !include {
			continue
		}
		if cmd.Flags().Changed(spec.name) || spec.always || spec.required {
			changed = true
		}

		switch spec.kind {
		case jsonFlagString:
			value, _ := cmd.Flags().GetString(spec.name)
			if spec.required {
				if err := requireFlag(value, flagName); err != nil {
					return nil, err
				}
			}
			values[spec.jsonName] = value
		case jsonFlagInt:
			value, _ := cmd.Flags().GetInt(spec.name)
			if spec.required {
				if err := requireIntFlag(value, flagName); err != nil {
					return nil, err
				}
			}
			values[spec.jsonName] = value
		case jsonFlagBool:
			value, _ := cmd.Flags().GetBool(spec.name)
			values[spec.jsonName] = value
		}
	}
	if requireAnyChanged && !changed {
		return nil, errors.New("pass at least one update flag or provide --data/--file")
	}
	return values, nil
}

func newTitleUpdateCommand(use string, short string, pathPrefix string, columns []string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				title, _ := cmd.Flags().GetString("title")
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"title": title}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), pathPrefix+url.PathEscape(args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().String("title", "", "Resource title")
	return cmd
}

func newGetByNameCommand(use string, short string, pathPattern string, columns []string, out outputOptions, withOrg bool, withRev bool) *cobra.Command {
	var orgID string
	var revNumber int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{}
			if withOrg {
				addQuery(query, "orgId", orgID)
			}
			if withRev && revNumber != 0 {
				query.Set("revNumber", strconv.Itoa(revNumber))
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath(pathPattern, args[0]), query, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, columns)
		},
	}
	if withOrg {
		cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	}
	if withRev {
		cmd.Flags().IntVar(&revNumber, "rev", 0, "Revision number")
	}
	return cmd
}

func newRawBodyPutCommand(use string, short string, pathPattern string, columns []string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(strings.Count(pathPattern, "%s")),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				return errors.New("--data or --file is required")
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath(pathPattern, args...), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	return cmd
}

func newRawBodyPostCommand(use string, short string, pathPattern string, columns []string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(strings.Count(pathPattern, "%s")),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				return errors.New("--data or --file is required")
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath(pathPattern, args...), nil, requestBody, &result); err != nil {
				return err
			}
			resultColumns := columns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	return cmd
}

func parseNameValueInputs(values []string, flag string) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		name, raw, ok := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, errors.Errorf("invalid %s %q; expected NAME=VALUE", flag, value)
		}
		result = append(result, map[string]interface{}{
			"name":  name,
			"value": raw,
		})
	}
	return result, nil
}

func addOptionalNameValueInputs(values map[string]interface{}, key string, inputs []string, flag string) error {
	if len(inputs) == 0 {
		return nil
	}
	parsed, err := parseNameValueInputs(inputs, flag)
	if err != nil {
		return err
	}
	values[key] = parsed
	return nil
}

func filterResponseItems(value interface{}, keep func(map[string]interface{}) bool) interface{} {
	rows := responseRows(value)
	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		if keep(row) {
			filtered = append(filtered, row)
		}
	}

	if wrapper, ok := value.(map[string]interface{}); ok && looksLikeResponseWrapper(wrapper) {
		clone := make(map[string]interface{}, len(wrapper))
		for key, item := range wrapper {
			clone[key] = item
		}
		for _, key := range []string{"items", "results", "data"} {
			if _, ok := clone[key]; ok {
				clone[key] = filtered
				if _, ok := clone["totalCount"]; ok {
					clone["totalCount"] = len(filtered)
				}
				return clone
			}
		}
	}

	return filtered
}

func printOptionResult(cmd *cobra.Command, client *rest.Client, out outputOptions, value interface{}, columns []string) error {
	items := normalizeItems(value)
	if rows, ok := scalarOptionRows(items); ok {
		value = rows
		if len(columns) == 0 {
			columns = []string{"value"}
		}
	}
	return printClientResult(cmd, client, out, value, columns)
}

func scalarOptionRows(value interface{}) ([]map[string]interface{}, bool) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	rows := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if _, ok := item.(map[string]interface{}); ok {
			return nil, false
		}
		rows = append(rows, map[string]interface{}{"value": item})
	}
	return rows, true
}

func escapedPath(pattern string, values ...string) string {
	escaped := make([]interface{}, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, url.PathEscape(value))
	}
	return fmt.Sprintf(pattern, escaped...)
}

func stackByNamePath(name string) string {
	parts := strings.Split(name, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return "/stacks/by-name/" + strings.Join(parts, "/")
}

func newServiceChildListCommand(use string, short string, pathPattern string, columns []string, out outputOptions) *cobra.Command {
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
			if err := client.Get(cmd.Context(), escapedPath(pathPattern, args[0]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
}

func newServiceChildCreateCommand(use string, short string, pathPattern string, columns []string, out outputOptions, specs []jsonFlagSpec) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, false)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath(pathPattern, args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	return cmd
}

func newServiceChildUpdateCommand(use string, short string, pathPattern string, columns []string, out outputOptions, specs []jsonFlagSpec) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, true)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath(pathPattern, args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	return cmd
}

func newServiceChildSetCommand(use string, short string, pathPattern string, columns []string, out outputOptions, specs []jsonFlagSpec) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, true)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath(pathPattern, args[0], args[1]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	return cmd
}

func newServiceChildDirectSetCommand(use string, short string, pathPattern string, columns []string, out outputOptions, specs []jsonFlagSpec) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, true)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath(pathPattern, args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	return cmd
}

func newServiceChildDeleteCommand(short string, pathPattern string, out outputOptions) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, short+"?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), escapedPath(pathPattern, args[0]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newStackServiceCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var stackID, serviceID, name, title string
	var replicas int
	var required bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create stack service",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(stackID, "--stack"); err != nil {
					return err
				}
				if err := requireFlag(serviceID, "--service"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				values := map[string]interface{}{
					"name":     name,
					"title":    title,
					"required": required,
					"replicas": replicas,
				}
				if value, ok := changedBool(cmd, "service-rev-pinned"); ok {
					values["serviceRevPinned"] = value
				}
				if err := addOptionalInt(values, "stackId", stackID, "--stack"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "serviceId", serviceID, "--service"); err != nil {
					return err
				}
				requestBody = bodyFromMap(values)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stack-services", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackServiceColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&stackID, "stack", "", "Stack ID")
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID")
	cmd.Flags().StringVar(&name, "name", "", "Stack service machine name")
	cmd.Flags().StringVar(&title, "title", "", "Stack service title")
	cmd.Flags().BoolVar(&required, "required", false, "Make stack service required")
	cmd.Flags().IntVar(&replicas, "replicas", 1, "Replica count")
	cmd.Flags().Bool("service-rev-pinned", false, "Pin the current service revision")
	return cmd
}

func newStackServiceUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update stack service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "title", "replicas", "required", "disabled", "main", "service-rev-pinned") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				values := map[string]interface{}{}
				if value, ok := changedString(cmd, "title"); ok {
					values["title"] = value
				}
				if value, ok := changedInt(cmd, "replicas"); ok {
					values["replicas"] = value
				}
				if value, ok := changedBool(cmd, "required"); ok {
					values["required"] = value
				}
				if value, ok := changedBool(cmd, "disabled"); ok {
					values["disabled"] = value
				}
				if value, ok := changedBool(cmd, "main"); ok {
					values["main"] = value
				}
				if value, ok := changedBool(cmd, "service-rev-pinned"); ok {
					values["serviceRevPinned"] = value
				}
				requestBody = bodyFromMap(values)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/stack-services/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackServiceColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().String("title", "", "Stack service title")
	cmd.Flags().Int("replicas", 0, "Replica count")
	cmd.Flags().Bool("required", false, "Set required state")
	cmd.Flags().Bool("disabled", false, "Set disabled state")
	cmd.Flags().Bool("main", false, "Set main service state")
	cmd.Flags().Bool("service-rev-pinned", false, "Set current service revision pin state")
	return cmd
}

func newStackServiceEnvVarCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "env-var",
		Aliases: []string{"env-vars"},
		Short:   "Manage stack service environment variables",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service environment variables", "/stack-services/%s/env-vars", stackServiceEnvColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service environment variable", "/stack-services/%s/env-vars", stackServiceEnvColumns, out, []jsonFlagSpec{
			stringJSONFlag("workload", "workload", "Workload name", false),
			stringJSONFlag("container", "container", "Container name", false),
			stringJSONFlag("name", "name", "Environment variable name", true),
			stringJSONFlag("value", "value", "Environment variable value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update stack service environment variable", "/stack-service-env-vars/%s", stackServiceEnvColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Environment variable value", true),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
		}),
		newServiceChildDeleteCommand("Delete stack service environment variable", "/stack-service-env-vars/%s", out),
	)
	return cmd
}

func newStackServiceHelmValueCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "helm-value",
		Aliases: []string{"helm-values"},
		Short:   "Manage stack service Helm values",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service Helm values", "/stack-services/%s/helm-values", stackServiceValueColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service Helm value", "/stack-services/%s/helm-values", stackServiceValueColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Helm value name", true),
			stringJSONFlag("value", "value", "Helm value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update stack service Helm value", "/stack-service-helm-values/%s", stackServiceValueColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Helm value", true),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
		}),
		newServiceChildDeleteCommand("Delete stack service Helm value", "/stack-service-helm-values/%s", out),
	)
	return cmd
}

func newStackServiceTokenCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "token",
		Aliases: []string{"tokens"},
		Short:   "Manage stack service tokens",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service tokens", "/stack-services/%s/tokens", stackServiceTokenColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service token", "/stack-services/%s/tokens", stackServiceTokenColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Token name", true),
			stringJSONFlag("value", "value", "Token value", false),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
			stringJSONFlag("regex", "regex", "Token generation regex", false),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update stack service token", "/stack-service-tokens/%s", stackServiceTokenColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Token value", false),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
			stringJSONFlag("regex", "regex", "Token generation regex", false),
		}),
		newServiceChildDeleteCommand("Delete stack service token", "/stack-service-tokens/%s", out),
	)
	return cmd
}

func newStackServiceAnnotationCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "annotation",
		Aliases: []string{"annotations"},
		Short:   "Manage stack service annotations",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service annotations", "/stack-services/%s/annotations", stackServiceAnnotationColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service annotation", "/stack-services/%s/annotations", stackServiceAnnotationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Annotation name", true),
			stringJSONFlag("value", "value", "Annotation value", true),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildDeleteCommand("Delete stack service annotation", "/stack-service-annotations/%s", out),
	)
	return cmd
}

func newStackServiceIntegrationCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "integration",
		Aliases: []string{"integrations"},
		Short:   "Manage stack service integrations",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service integrations", "/stack-services/%s/integrations", stackServiceIntegrationColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service integration", "/stack-services/%s/integrations", stackServiceIntegrationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Integration slot name", true),
			intJSONFlag("integration", "integrationId", "Integration ID", true),
		}),
		newServiceChildDeleteCommand("Delete stack service integration", "/stack-service-integrations/%s", out),
	)
	return cmd
}

func newStackServiceLinkCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "link",
		Aliases: []string{"links"},
		Short:   "Manage stack service links",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service links", "/stack-services/%s/links", stackServiceLinkColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set stack service link", "/stack-services/%s/links/%s", operationColumns, out, []jsonFlagSpec{
			intJSONFlag("linked-service", "linkedStackServiceId", "Linked stack service ID", false),
		}),
	)
	return cmd
}

func newStackServiceVolumeCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"volumes"},
		Short:   "Manage stack service volumes",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service volumes", "/stack-services/%s/volumes", stackServiceVolumeColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set stack service volume", "/stack-services/%s/volumes/%s", operationColumns, out, []jsonFlagSpec{
			intJSONFlag("size", "size", "Volume size", false),
		}),
	)
	return cmd
}

func newStackServiceSettingCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "setting",
		Aliases: []string{"settings"},
		Short:   "Manage stack service settings",
	}
	cmd.AddCommand(
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set stack service setting", "/stack-services/%s/settings/%s", operationColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Setting value", true),
		}),
	)
	return cmd
}

func newStackServiceConfigCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configs"},
		Short:   "Manage stack service configs",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service configs", "/stack-services/%s/configs", stackServiceConfigColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set stack service config", "/stack-services/%s/configs/%s", operationColumns, out, []jsonFlagSpec{
			stringJSONFlag("config", "config", "Config override", true),
		}),
	)
	return cmd
}

func newStackServiceResourcesCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Manage stack service resources",
	}
	cmd.AddCommand(newServiceChildDirectSetCommand("set SERVICE_ID", "Set stack service resources", "/stack-services/%s/resources", operationColumns, out, resourceJSONFlags()))
	return cmd
}

func newStackServiceOptionsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "options",
		Short: "Manage stack service options",
	}
	cmd.AddCommand(newStackServiceOptionsSetCommand(out))
	return cmd
}

func newStackServiceOptionsSetCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var options []string
	cmd := &cobra.Command{
		Use:   "set SERVICE_ID",
		Short: "Set stack service options",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if len(options) == 0 {
					return errors.New("--option is required unless --data/--file is provided")
				}
				parsed, err := parseStackServiceOptions(options)
				if err != nil {
					return err
				}
				requestBody = map[string]interface{}{"options": parsed}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath("/stack-services/%s/options", args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringArrayVar(&options, "option", nil, "Option as VERSION:DEFAULT:DISABLED; repeatable")
	return cmd
}

func parseStackServiceOptions(values []string) ([]map[string]interface{}, error) {
	options := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) != 3 {
			return nil, errors.Errorf("invalid --option %q; expected VERSION:DEFAULT:DISABLED", value)
		}
		defaultValue, err := strconv.ParseBool(parts[1])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid default value in --option %q", value)
		}
		disabled, err := strconv.ParseBool(parts[2])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid disabled value in --option %q", value)
		}
		if strings.TrimSpace(parts[0]) == "" {
			return nil, errors.Errorf("invalid --option %q; version is required", value)
		}
		options = append(options, map[string]interface{}{
			"version":  parts[0],
			"default":  defaultValue,
			"disabled": disabled,
		})
	}
	return options, nil
}

func newStackServiceCronScheduleCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cron-schedule",
		Aliases: []string{"cron-schedules", "cron"},
		Short:   "Manage stack service cron schedules",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List stack service cron schedules", "/stack-services/%s/cron-schedules", stackServiceCronScheduleColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create stack service cron schedule", "/stack-services/%s/cron-schedules", stackServiceCronScheduleColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Cron schedule machine name", true),
			stringJSONFlag("title", "title", "Cron schedule title", true),
			stringJSONFlag("crontab", "crontab", "Crontab expression", true),
			stringJSONFlag("command", "command", "Command to run", true),
			stringJSONFlag("workload", "workload", "Workload name", false),
			boolJSONFlag("disabled", "disabled", "Create the schedule disabled", false),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update stack service cron schedule", "/stack-service-cron-schedules/%s", stackServiceCronScheduleColumns, out, []jsonFlagSpec{
			boolJSONFlag("disabled", "disabled", "Set disabled state", false),
			stringJSONFlag("title", "title", "Cron schedule title", false),
			stringJSONFlag("crontab", "crontab", "Crontab expression", false),
			stringJSONFlag("command", "command", "Command to run", false),
			stringJSONFlag("workload", "workload", "Workload name", false),
			stringJSONFlag("env-type", "envType", "Environment type", false),
		}),
		newServiceChildDeleteCommand("Delete stack service cron schedule", "/stack-service-cron-schedules/%s", out),
	)
	return cmd
}

func resourceJSONFlags() []jsonFlagSpec {
	return []jsonFlagSpec{
		stringJSONFlag("workload", "workload", "Workload name", false),
		stringJSONFlag("container", "container", "Container name", false),
		intJSONFlag("request-cpu", "requestCPU", "Requested CPU", false),
		intJSONFlag("request-mem", "requestMem", "Requested memory", false),
		intJSONFlag("limit-cpu", "limitCPU", "CPU limit", false),
		intJSONFlag("limit-mem", "limitMem", "Memory limit", false),
	}
}

func newServiceCommand() *cobra.Command {
	out := outputOptions{}
	cmd := newCatalogCommand("service", "services", "Manage services", "/services", catalogServiceColumns, false)
	cmd.AddCommand(
		newGetByNameCommand("get-by-name NAME", "Get service by name", "/services/by-name/%s", catalogServiceColumns, out, false, true),
		newCatalogImportFromGitCommand("import", "Import services from Git", "/services/actions/import", operationColumns, out),
		newManifestValidateCommand("service", out),
		newManifestCreateCommand("service", out, catalogServiceColumns),
		newServiceSettingsCommand(out),
		newGetCommand("revision ID", "Get service revision", "/service-revisions/", serviceRevisionColumns, out),
		newServiceOptionsCommand(out),
	)
	return cmd
}

func newServiceSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage service settings",
	}
	cmd.AddCommand(newRawBodyPutCommand("update ID", "Update service settings", "/services/settings/%s", catalogServiceColumns, out))
	return cmd
}

func newServiceOptionsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "options",
		Short: "Show service options",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "link-candidates NAME",
		Short: "List service link candidates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/services/%s/options/link-candidates", args[0]), nil, &result); err != nil {
				return err
			}
			return printOptionResult(cmd, client, out, result, nil)
		},
	})
	return cmd
}

func newCatalogCommand(use string, alias string, short string, path string, columns []string, excludePublic bool) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: []string{alias},
		Short:   short,
	}
	addOutputFlag(cmd, &out)
	listCmd := newCatalogListCommand("list", "List "+alias, path, columns, out, excludePublic)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get "+use, path+"/", columns, out))
	return cmd
}

func newCatalogListCommand(use string, short string, path string, columns []string, out outputOptions, excludePublic bool) *cobra.Command {
	var orgID, projectIDs, search string
	var page, pageSize int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "search", search)
			addPagination(query, page, pageSize)
			if excludePublic {
				addBoolQuery(cmd, query, "excludePublic", "exclude-public")
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	cmd.Flags().StringVar(&search, "search", "", "Search query")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	if excludePublic {
		cmd.Flags().Bool("exclude-public", false, "Exclude public resources")
	}
	return cmd
}

func newAppCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage apps",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List apps",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addBoolQuery(cmd, query, "clusterApp", "cluster-app")
			var result interface{}
			if err := client.Get(cmd.Context(), "/apps", query, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) != outputJSON {
				enrichAppStacksFromInstances(cmd.Context(), client, normalizeItems(result), query)
			}
			return printClientResult(cmd, client, out, result, appColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().Bool("cluster-app", false, "Filter cluster apps")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintWithInstances(cmd, out, "/apps/"+args[0], appGetColumns, "appId", args[0])
		},
	}
	statusCmd := &cobra.Command{
		Use:   "status ID",
		Short: "Show app status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			result, err := buildAppStatus(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, appStatusColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newGetByNameCommand("get-by-name NAME", "Get app by name", "/apps/by-name/%s", appGetColumns, out, true, false),
		statusCmd,
		newAppCreateCommand(out),
		newTitleUpdateCommand("update ID", "Update app", "/apps/", appColumns, out),
		newDeleteCommand("delete ID", "Delete app", "/apps/", appColumns, out),
	)
	cmd.AddCommand(newAppInstanceCommand("instance", "Manage app instances"))
	return cmd
}

func newAppCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var orgID, projectID, env, clusterID, stack, stackRevID, name, title, instanceName, instanceTitle, domain, ciIntegrationID, registryIntegrationID string
	var deferInitialDeployment bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create app",
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(instanceName, "--instance"); err != nil {
					return err
				}
				if err := requireFlag(env, "--env"); err != nil {
					return err
				}
				if err := requireFlag(clusterID, "--cluster"); err != nil {
					return err
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				resolvedEnvID, err := resolveEnvID(cmd.Context(), client, env, resolvedOrgID)
				if err != nil {
					return err
				}
				resolvedStackRevID, err := resolveCreateStackRevID(cmd.Context(), client, stack, stackRevID, resolvedOrgID)
				if err != nil {
					return err
				}
				resolvedClusterID, err := resolveClusterID(cmd.Context(), client, clusterID, resolvedOrgID)
				if err != nil {
					return err
				}
				values := map[string]interface{}{
					"name":         name,
					"instanceName": instanceName,
				}
				addOptionalString(values, "title", title)
				addOptionalString(values, "instanceTitle", instanceTitle)
				addOptionalString(values, "domain", domain)
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "envId", resolvedEnvID, "--env"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "stackRevId", resolvedStackRevID, "--stack-rev"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "clusterId", resolvedClusterID, "--cluster"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "ciIntegrationId", ciIntegrationID, "--ci-integration"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "registryIntegrationId", registryIntegrationID, "--registry-integration"); err != nil {
					return err
				}
				values["deferInitialDeployment"] = deferInitialDeployment
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/apps", nil, requestBody, &result); err != nil {
				return errors.Wrap(err, "create app")
			}
			if handled, err := printAppCreateTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			columns := resourceOrOperationColumns(result, appColumns)
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&env, "env", "", "Environment ID or name")
	cmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID or name")
	cmd.Flags().StringVar(&stack, "stack", "", "Stack ID or name; uses the current revision")
	cmd.Flags().StringVar(&stackRevID, "stack-rev", "", "Stack revision ID")
	cmd.Flags().StringVar(&name, "name", "", "App machine name")
	cmd.Flags().StringVar(&title, "title", "", "App title")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Initial app instance machine name")
	cmd.Flags().StringVar(&instanceName, "instance-name", "", "Deprecated alias for --instance")
	cmd.Flags().StringVar(&instanceTitle, "instance-title", "", "Initial app instance title")
	cmd.Flags().StringVar(&domain, "domain", "", "Initial app instance domain")
	cmd.Flags().StringVar(&ciIntegrationID, "ci-integration", "", "CI integration ID")
	cmd.Flags().StringVar(&registryIntegrationID, "registry-integration", "", "Registry integration ID")
	cmd.Flags().BoolVar(&deferInitialDeployment, "defer-initial-deployment", false, "Create the app without starting its initial deployment")
	cmd.Flags().Bool("cluster-app", false, "Deprecated: use --cluster when creating the initial cluster app instance")
	return cmd
}

func newAppInstanceCommand(use string, short string) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, appID, clusterID string
	var clusterApp bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List app instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "appId", appID)
			addQuery(query, "clusterId", clusterID)
			query.Set("clusterApp", strconv.FormatBool(clusterApp))
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-instances", query, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) != outputJSON {
				enrichInstanceLastDeployedAt(cmd.Context(), client, responseRows(result))
			}
			return printClientResult(cmd, client, out, result, instanceListColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&appID, "app", "", "App ID")
	listCmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID")
	listCmd.Flags().BoolVar(&clusterApp, "cluster-app", false, "Filter cluster app instances")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			result, err := buildInstanceStatus(cmd.Context(), client, args[0], nil)
			if err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, instanceGetColumns)
		},
	}
	statusCmd := &cobra.Command{
		Use:   "status ID",
		Short: "Show app instance status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			result, err := buildInstanceStatus(cmd.Context(), client, args[0], nil)
			if err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, instanceStatusColumns)
		},
	}

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		getCmd,
		newAppInstanceGetByNameCommand(out),
		statusCmd,
		newAppInstanceCreateCommand(out),
		newTitleUpdateCommand("update ID", "Update app instance", "/app-instances/", instanceColumns, out),
		newAppInstanceDeleteCommand(out),
		newAppInstanceSettingsCommand(out),
		newAppInstanceCICDSettingsCommand(out),
		newAppInstanceUpgradeStackCommand(out),
		newGetCommand("upgrade-stack-changelog ID", "Preview app instance stack upgrade", "/app-instance-stack-upgrade-changelogs/", appInstanceStackChangelogColumns, out),
		newAppAccessCommand(out),
	)
	cmd.AddCommand(newAppServiceCommand("service", []string{"services"}, "Manage app services", instanceFilterArg))
	cmd.AddCommand(newAppRouteCommand("route", []string{"routes"}, "Manage app instance routes", instanceFilterArg))
	cmd.AddCommand(newAppPortCommand("port", []string{"ports"}, "Manage app instance ports", instanceFilterArg))
	cmd.AddCommand(newAppCertCommand("cert", []string{"certs", "certificate", "certificates"}, "Manage app instance certificates", instanceFilterArg))
	cmd.AddCommand(newInstanceBuildCommand(), newInstanceDeploymentCommand(), newInstanceBackupCommand(), newInstanceImportCommand())
	return cmd
}

func newAppAccessCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Manage app instance access",
	}
	cmd.AddCommand(
		newGetCommand("get APP_INSTANCE_ID", "Get app instance access", "/app-instance-accesses/", appAccessColumns, out),
		newRawBodyPostCommand("create APP_INSTANCE_ID", "Create app instance access", "/app-instance-accesses/%s", []string{"access", "taskId"}, out),
		newRawBodyPostCommand("preflight", "Preflight app instance access", "/app-accesses/actions/preflight", []string{"valid"}, out),
		newRawBodyPutCommand("update ACCESS_ID", "Update app access", "/app-accesses/%s", []string{"access", "taskId"}, out),
		newServiceChildDeleteCommand("Delete app access", "/app-accesses/%s", out),
		newAppAccessCleanupsCommand(out),
		newClusterActionCommand("retry-cleanup CLEANUP_ID", "Retry app-access cleanup", "/app-access-cleanups/%s/actions/retry", out),
	)
	return cmd
}

func newAppAccessCleanupsCommand(out outputOptions) *cobra.Command {
	var appInstanceID, integrationID string
	cmd := &cobra.Command{
		Use:   "cleanups",
		Short: "List app-access cleanups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if (appInstanceID == "") == (integrationID == "") {
				return errors.New("exactly one of --app-instance or --integration is required")
			}
			query := url.Values{}
			addQuery(query, "appInstanceId", appInstanceID)
			addQuery(query, "integrationId", integrationID)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-access-cleanups", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appAccessCleanupColumns)
		},
	}
	cmd.Flags().StringVar(&appInstanceID, "app-instance", "", "App instance ID")
	cmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")
	return cmd
}

func newAppInstanceCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var orgID, app, env, clusterID, stack, stackRevID, name, title, instanceName, instanceTitle, domain, region, zone, ciIntegrationID, registryIntegrationID string
	var deferInitialDeployment bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create app instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(app, "--app"); err != nil {
					return err
				}
				if err := requireFlag(env, "--env"); err != nil {
					return err
				}
				if err := requireFlag(clusterID, "--cluster"); err != nil {
					return err
				}
				resolvedInstanceName := firstScalar(instanceName, name)
				resolvedInstanceTitle := firstScalar(instanceTitle, title)
				if err := requireFlag(resolvedInstanceName, "--instance"); err != nil {
					return err
				}
				resolvedAppID, err := resolveAppID(cmd.Context(), client, app, orgID)
				if err != nil {
					return err
				}
				resolvedEnvID, err := resolveEnvID(cmd.Context(), client, env, orgID)
				if err != nil {
					return err
				}
				resolvedStackRevID, err := resolveCreateStackRevID(cmd.Context(), client, stack, stackRevID, orgID)
				if err != nil {
					return err
				}
				resolvedClusterID, err := resolveClusterID(cmd.Context(), client, clusterID, orgID)
				if err != nil {
					return err
				}
				appIDNumber, err := strconv.Atoi(resolvedAppID)
				if err != nil {
					return errors.Wrap(err, "invalid --app")
				}
				envIDNumber, err := strconv.Atoi(resolvedEnvID)
				if err != nil {
					return errors.Wrap(err, "invalid --env")
				}
				clusterIDNumber, err := strconv.Atoi(resolvedClusterID)
				if err != nil {
					return errors.Wrap(err, "invalid --cluster")
				}
				values := map[string]interface{}{
					"appId":        appIDNumber,
					"envId":        envIDNumber,
					"clusterId":    clusterIDNumber,
					"instanceName": resolvedInstanceName,
				}
				if err := addOptionalInt(values, "stackRevId", resolvedStackRevID, "--stack-rev"); err != nil {
					return err
				}
				addOptionalString(values, "instanceTitle", resolvedInstanceTitle)
				addOptionalString(values, "domain", domain)
				if err := addOptionalInt(values, "ciIntegrationId", ciIntegrationID, "--ci-integration"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "registryIntegrationId", registryIntegrationID, "--registry-integration"); err != nil {
					return err
				}
				values["deferInitialDeployment"] = deferInitialDeployment
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-instances", nil, requestBody, &result); err != nil {
				return errors.Wrap(err, "create app instance")
			}
			if handled, err := printAppInstanceCreateTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			columns := resourceOrOperationColumns(result, instanceColumns)
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID for resolving names")
	cmd.Flags().StringVar(&app, "app", "", "App ID or name")
	cmd.Flags().StringVar(&env, "env", "", "Environment ID or name")
	cmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID or name")
	cmd.Flags().StringVar(&stack, "stack", "", "Stack ID or name; uses the current revision")
	cmd.Flags().StringVar(&stackRevID, "stack-rev", "", "Stack revision ID")
	cmd.Flags().StringVar(&name, "name", "", "Deprecated alias for --instance")
	cmd.Flags().StringVar(&title, "title", "", "Deprecated alias for --instance-title")
	cmd.Flags().StringVar(&instanceName, "instance", "", "App instance machine name")
	cmd.Flags().StringVar(&instanceName, "instance-name", "", "Deprecated alias for --instance")
	cmd.Flags().StringVar(&instanceTitle, "instance-title", "", "App instance title")
	cmd.Flags().StringVar(&domain, "domain", "", "App instance domain")
	cmd.Flags().StringVar(&region, "region", "", "Deprecated")
	cmd.Flags().StringVar(&zone, "zone", "", "Deprecated")
	cmd.Flags().StringVar(&ciIntegrationID, "ci-integration", "", "CI integration ID")
	cmd.Flags().StringVar(&registryIntegrationID, "registry-integration", "", "Registry integration ID")
	cmd.Flags().BoolVar(&deferInitialDeployment, "defer-initial-deployment", false, "Create the app instance without starting its initial deployment")
	cmd.Flags().Bool("cluster-app", false, "Deprecated: cluster app status is inferred from --cluster")
	_, _, _ = region, zone, title
	return cmd
}

func newAppInstanceGetByNameCommand(out outputOptions) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get-by-name APP_NAME INSTANCE_NAME",
		Short: "Get app instance by app and instance name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/app-instances/by-name/%s/%s", args[0], args[1]), query, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, instanceGetColumns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	return cmd
}

func newAppInstanceDeleteCommand(out outputOptions) *cobra.Command {
	var yes, force bool
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete app instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Delete app instance?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{}
			if cmd.Flags().Changed("force") {
				query.Set("force", strconv.FormatBool(force))
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), "/app-instances/"+url.PathEscape(args[0]), query, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "Force deletion")
	return cmd
}

func newAppInstanceSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage app instance settings",
	}
	cmd.AddCommand(newAppInstanceSettingsUpdateCommand(out))
	return cmd
}

func newAppInstanceSettingsUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app instance settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !cmd.Flags().Changed("auto-stack-upgrade-enabled") {
					return errors.New("--auto-stack-upgrade-enabled is required unless --data/--file is provided")
				}
				enabled, _ := cmd.Flags().GetBool("auto-stack-upgrade-enabled")
				autoStackUpgrade := map[string]interface{}{"enabled": enabled}
				if settings := changedStackUpgradeSettings(cmd, "upgrade-"); len(settings) != 0 {
					autoStackUpgrade["upgradeSettings"] = settings
				}
				timeWindow, changed, err := changedAutomationTimeWindow(cmd, "time-window-")
				if err != nil {
					return err
				}
				if changed {
					autoStackUpgrade["timeWindow"] = timeWindow
				}
				requestBody = map[string]interface{}{"autoStackUpgrade": autoStackUpgrade}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-instances/settings/"+url.PathEscape(args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, instanceColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().Bool("auto-stack-upgrade-enabled", false, "Enable automatic stack upgrades")
	addStackUpgradeFlags(cmd, "upgrade-", false)
	addAutomationTimeWindowFlags(cmd, "time-window-")
	return cmd
}

func newAppInstanceCICDSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cicd-settings",
		Short: "Manage app instance CI/CD settings",
	}
	cmd.AddCommand(
		newAppInstanceCICDSettingsGetCommand(out),
		newAppInstanceCICDSettingsUpdateCommand(out),
	)
	return cmd
}

func newAppInstanceCICDSettingsGetCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get app instance CI/CD settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-instances/cicd-settings/"+url.PathEscape(args[0]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, instanceCICDSettingsColumns)
		},
	}
}

func newAppInstanceCICDSettingsUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var ciIntegrationID, registryIntegrationID string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app instance CI/CD settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(ciIntegrationID, "--ci-integration"); err != nil {
					return err
				}
				if err := requireFlag(registryIntegrationID, "--registry-integration"); err != nil {
					return err
				}
				values := map[string]interface{}{}
				if err := addOptionalInt(values, "ciIntegrationId", ciIntegrationID, "--ci-integration"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "registryIntegrationId", registryIntegrationID, "--registry-integration"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-instances/cicd-settings/"+url.PathEscape(args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, instanceCICDSettingsColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&ciIntegrationID, "ci-integration", "", "CI integration ID; use 0 for built-in Wodby CI")
	cmd.Flags().StringVar(&registryIntegrationID, "registry-integration", "", "Registry integration ID; use 0 for built-in Wodby registry")
	return cmd
}

func newAppInstanceUpgradeStackCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "upgrade-stack ID",
		Short: "Upgrade app instance stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				requestBody = stackUpgradeSettings(cmd, "", true)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-instances/"+url.PathEscape(args[0])+"/actions/upgrade-stack", nil, requestBody, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	addStackUpgradeFlags(cmd, "", true)
	return cmd
}

func stackUpgradeFlagNames() []string {
	return []string{"versions", "replicas", "resources", "integrations", "services", "settings", "links", "tokens", "configs", "cron", "volumes", "main"}
}

func addStackUpgradeFlags(cmd *cobra.Command, prefix string, defaultValue bool) {
	for _, name := range stackUpgradeFlagNames() {
		cmd.Flags().Bool(prefix+name, defaultValue, "Include "+strings.ReplaceAll(name, "-", " ")+" during stack upgrade")
	}
}

func stackUpgradeSettings(cmd *cobra.Command, prefix string, includeUnchanged bool) map[string]interface{} {
	values := map[string]interface{}{}
	for _, name := range stackUpgradeFlagNames() {
		flagName := prefix + name
		if !includeUnchanged && !cmd.Flags().Changed(flagName) {
			continue
		}
		value, _ := cmd.Flags().GetBool(flagName)
		values[name] = value
	}
	return values
}

func changedStackUpgradeSettings(cmd *cobra.Command, prefix string) map[string]interface{} {
	return stackUpgradeSettings(cmd, prefix, false)
}

func addAutomationTimeWindowFlags(cmd *cobra.Command, prefix string) {
	cmd.Flags().Bool(prefix+"enabled", false, "Enable the automation time window")
	cmd.Flags().String(prefix+"start", "", "Automation time window start in HH:MM format")
	cmd.Flags().String(prefix+"end", "", "Automation time window end in HH:MM format")
	cmd.Flags().String(prefix+"time-zone", "", "Automation time window IANA time zone")
	cmd.Flags().StringArray(prefix+"day", nil, "Automation time window weekday; repeatable or comma-separated")
}

func changedAutomationTimeWindow(cmd *cobra.Command, prefix string) (map[string]interface{}, bool, error) {
	enabledFlag := prefix + "enabled"
	childFlags := []string{prefix + "start", prefix + "end", prefix + "time-zone", prefix + "day"}
	if !hasChangedFlags(cmd.Flags(), append([]string{enabledFlag}, childFlags...)...) {
		return nil, false, nil
	}
	if !cmd.Flags().Changed(enabledFlag) {
		return nil, false, errors.Errorf("--%s is required when configuring an automation time window", enabledFlag)
	}

	enabled, _ := cmd.Flags().GetBool(enabledFlag)
	values := map[string]interface{}{"enabled": enabled}
	if !enabled {
		if hasChangedFlags(cmd.Flags(), childFlags...) {
			return nil, false, errors.Errorf("automation time window options cannot be used when --%s=false", enabledFlag)
		}
		return values, true, nil
	}

	start, startChanged := changedString(cmd, prefix+"start")
	if !startChanged || strings.TrimSpace(start) == "" {
		return nil, false, errors.Errorf("--%s is required when --%s=true", prefix+"start", enabledFlag)
	}
	end, endChanged := changedString(cmd, prefix+"end")
	if !endChanged || strings.TrimSpace(end) == "" {
		return nil, false, errors.Errorf("--%s is required when --%s=true", prefix+"end", enabledFlag)
	}
	values["start"] = start
	values["end"] = end

	if timeZone, ok := changedString(cmd, prefix+"time-zone"); ok {
		if strings.TrimSpace(timeZone) == "" {
			return nil, false, errors.Errorf("--%s cannot be empty", prefix+"time-zone")
		}
		values["timeZone"] = timeZone
	}
	if cmd.Flags().Changed(prefix + "day") {
		days, _ := cmd.Flags().GetStringArray(prefix + "day")
		normalized, err := normalizeAutomationWeekdays(days, "--"+prefix+"day")
		if err != nil {
			return nil, false, err
		}
		values["days"] = normalized
	}

	return values, true, nil
}

func normalizeAutomationWeekdays(values []string, flag string) ([]string, error) {
	valid := map[string]bool{
		"MONDAY": true, "TUESDAY": true, "WEDNESDAY": true, "THURSDAY": true,
		"FRIDAY": true, "SATURDAY": true, "SUNDAY": true,
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			day := strings.ToUpper(strings.TrimSpace(part))
			if day == "" {
				continue
			}
			if !valid[day] {
				return nil, errors.Errorf("invalid %s value %q", flag, part)
			}
			if seen[day] {
				return nil, errors.Errorf("duplicate %s value %q", flag, part)
			}
			seen[day] = true
			result = append(result, day)
		}
	}
	if len(result) == 0 {
		return nil, errors.Errorf("%s must include at least one weekday", flag)
	}
	return result, nil
}

func buildAppStatus(ctx context.Context, client *rest.Client, appID string) (map[string]interface{}, error) {
	var app interface{}
	if err := client.Get(ctx, "/apps/"+appID, nil, &app); err != nil {
		return nil, err
	}
	row := cloneFirstRow(normalizeItem(app))
	if row == nil {
		return nil, errors.New("app response did not include an item")
	}

	query := url.Values{"appId": []string{appID}}
	addQuery(query, "orgId", firstScalarPath(row, "orgId", "org.id"))
	var instancesResult interface{}
	if err := client.Get(ctx, "/app-instances", query, &instancesResult); err != nil {
		return nil, err
	}

	instanceStatuses := make([]interface{}, 0)
	services := make([]map[string]interface{}, 0)
	routes := make([]map[string]interface{}, 0)
	builds := make([]map[string]interface{}, 0)
	deployments := make([]map[string]interface{}, 0)
	for _, instance := range responseRows(instancesResult) {
		instanceID := firstScalarPath(instance, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id")
		if instanceID == "" {
			continue
		}
		status, err := buildInstanceStatus(ctx, client, instanceID, instance)
		if err != nil {
			return nil, err
		}
		instanceStatuses = append(instanceStatuses, status)
		services = append(services, responseRows(status["services"])...)
		routes = append(routes, responseRows(status["routes"])...)
		builds = append(builds, responseRows(status["builds"])...)
		deployments = append(deployments, responseRows(status["deployments"])...)
	}

	row["instances"] = instanceStatuses
	row["services"] = services
	row["routes"] = routes
	row["builds"] = builds
	row["deployments"] = deployments
	row["serviceStatus"] = summarizeStatusRows(services, "services")
	row["routeStatus"] = summarizeStatusRows(routes, "routes")
	row["latestBuild"] = summarizeBuild(latestByTime(builds))
	row["latestDeployment"] = summarizeDeployment(latestByTime(deployments))
	row["needs"] = summarizeOperationalNeeds(services, routes)
	return row, nil
}

func buildInstanceStatus(ctx context.Context, client *rest.Client, instanceID string, base map[string]interface{}) (map[string]interface{}, error) {
	row := cloneRow(base)
	if row == nil {
		var instance interface{}
		if err := client.Get(ctx, "/app-instances/"+instanceID, nil, &instance); err != nil {
			return nil, err
		}
		row = cloneFirstRow(normalizeItem(instance))
		if row == nil {
			return nil, errors.New("app instance response did not include an item")
		}
	}

	services, err := fetchRows(ctx, client, "/app-services", url.Values{"appInstanceId": []string{instanceID}})
	if err != nil {
		return nil, err
	}
	routes, err := fetchRows(ctx, client, "/app-routes", url.Values{"appInstanceId": []string{instanceID}})
	if err != nil {
		return nil, err
	}
	ports, err := fetchRows(ctx, client, "/app-ports", url.Values{"appInstanceId": []string{instanceID}})
	if err != nil {
		return nil, err
	}
	builds, err := fetchRows(ctx, client, "/app-builds", url.Values{"appInstanceId": []string{instanceID}, "pageSize": []string{"1"}})
	if err != nil {
		return nil, err
	}
	deployments, err := fetchRows(ctx, client, "/app-deployments", url.Values{"appInstanceId": []string{instanceID}, "pageSize": []string{"1"}})
	if err != nil {
		return nil, err
	}

	row["services"] = services
	row["routes"] = routes
	row["ports"] = ports
	row["builds"] = builds
	row["deployments"] = deployments
	row["serviceStatus"] = summarizeStatusRows(services, "services")
	row["routeStatus"] = summarizeStatusRows(routes, "routes")
	row["portStatus"] = summarizeCountRows(ports, "ports")
	row["latestBuild"] = summarizeBuild(latestByTime(builds))
	row["latestDeployment"] = summarizeDeployment(latestByTime(deployments))
	row["needs"] = summarizeOperationalNeeds(services, routes)
	return row, nil
}

func enrichInstanceLastDeployedAt(ctx context.Context, client *rest.Client, rows []map[string]interface{}) {
	for _, row := range rows {
		if row == nil || firstScalarPath(row, "lastDeployedAt") != "" {
			continue
		}
		instanceID := firstScalarPath(row, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id")
		if instanceID == "" {
			continue
		}
		deployments, err := fetchRows(ctx, client, "/app-deployments", url.Values{"appInstanceId": []string{instanceID}, "pageSize": []string{"1"}})
		if err != nil {
			continue
		}
		deployment := latestByTime(deployments)
		deployedAt := firstNonNilPath(deployment, "endedAt", "deployedAt", "completedAt", "startedAt", "createdAt")
		if deployedAt != nil {
			row["lastDeployedAt"] = deployedAt
		}
	}
}

func fetchRows(ctx context.Context, client *rest.Client, path string, query url.Values) ([]map[string]interface{}, error) {
	var result interface{}
	if err := client.Get(ctx, path, query, &result); err != nil {
		return nil, err
	}
	return responseRows(result), nil
}

func cloneFirstRow(value interface{}) map[string]interface{} {
	rows := asRows(value)
	if len(rows) == 0 {
		return nil
	}
	return cloneRow(rows[0])
}

func cloneRow(row map[string]interface{}) map[string]interface{} {
	if row == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(row))
	for key, value := range row {
		clone[key] = value
	}
	return clone
}

func summarizeStatusRows(rows []map[string]interface{}, label string) string {
	if len(rows) == 0 {
		return "no " + label
	}

	counts := map[string]int{}
	for _, row := range rows {
		status := firstScalarPath(row, "status")
		if status == "" {
			status = "unknown"
		}
		if truthyPath(row, "disabled") {
			status = "disabled"
		}
		counts[status]++
	}

	if len(counts) == 1 {
		for status, count := range counts {
			return fmt.Sprintf("%d %s %s", count, label, status)
		}
	}

	keys := make([]string, 0, len(counts))
	for status := range counts {
		keys = append(keys, status)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, status := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}
	return fmt.Sprintf("%d %s (%s)", len(rows), label, strings.Join(parts, ", "))
}

func summarizeCountRows(rows []map[string]interface{}, label string) string {
	if len(rows) == 0 {
		return "no " + label
	}
	return fmt.Sprintf("%d %s", len(rows), label)
}

func summarizeBuild(row map[string]interface{}) string {
	if row == nil {
		return "none"
	}
	number := firstScalarPath(row, "number")
	status := firstScalarPath(row, "status")
	gitRef := firstScalarPath(row, "gitRef")
	commit := firstScalarPath(row, "commitHash")
	if len(commit) > 8 {
		commit = commit[:8]
	}
	parts := compactNonEmpty(prefixIfNotEmpty("#", number), status, gitRef, commit)
	if len(parts) == 0 {
		return firstScalarPath(row, "id")
	}
	return strings.Join(parts, " ")
}

func summarizeDeployment(row map[string]interface{}) string {
	if row == nil {
		return "none"
	}
	number := firstScalarPath(row, "number")
	status := firstScalarPath(row, "status")
	parts := compactNonEmpty(prefixIfNotEmpty("#", number), status, formatDurationColumn(row))
	if len(parts) == 0 {
		return firstScalarPath(row, "id")
	}
	return strings.Join(parts, " ")
}

func latestByTime(rows []map[string]interface{}) map[string]interface{} {
	var latest map[string]interface{}
	var latestTime time.Time
	for _, row := range rows {
		t, ok := parseDisplayTime(valueAtPath(row, "createdAt"))
		if !ok {
			t, ok = parseDisplayTime(valueAtPath(row, "startedAt"))
		}
		if latest == nil || (ok && t.After(latestTime)) {
			latest = row
			if ok {
				latestTime = t
			}
		}
	}
	return latest
}

func summarizeOperationalNeeds(services []map[string]interface{}, routes []map[string]interface{}) string {
	needs := make([]string, 0)
	addNeed := func(count int, label string) {
		if count == 0 {
			return
		}
		needs = append(needs, fmt.Sprintf("%d %s", count, label))
	}

	needsRebuild := 0
	needsRedeploy := 0
	configPending := 0
	disabledServices := 0
	for _, service := range services {
		if truthyPath(service, "needsRebuild") {
			needsRebuild++
		}
		if truthyPath(service, "needsRedeploy") {
			needsRedeploy++
		}
		if value := firstNonNilPath(service, "configurationReady"); value != nil && !truthyPath(service, "configurationReady") {
			configPending++
		}
		if truthyPath(service, "disabled") {
			disabledServices++
		}
	}

	disabledRoutes := 0
	for _, route := range routes {
		if truthyPath(route, "disabled") {
			disabledRoutes++
		}
	}

	addNeed(needsRebuild, "services need rebuild")
	addNeed(needsRedeploy, "services need redeploy")
	addNeed(configPending, "services config pending")
	addNeed(disabledServices, "services disabled")
	addNeed(disabledRoutes, "routes disabled")
	if len(needs) == 0 {
		return "none"
	}
	return strings.Join(needs, ", ")
}

func prefixIfNotEmpty(prefix string, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func newInstanceBuildCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "build",
		Aliases: []string{"builds"},
		Short:   "Manage app instance builds",
	}
	addOutputFlag(cmd, &out)
	listCmd := newInstancePaginatedListCommand("list INSTANCE_ID", "List builds", "/app-builds", buildListColumns, out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get build", "/app-builds/", buildColumns, out), newBuildDeployCommand(out))
	return cmd
}

func newInstanceDeploymentCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "deployment",
		Aliases: []string{"deployments"},
		Short:   "Manage app instance deployments",
	}
	addOutputFlag(cmd, &out)

	waitCmd := &cobra.Command{
		Use:   "wait ID",
		Short: "Wait for deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			result, err := waitForDeployment(cmd.Context(), client, args[0], timeout)
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	waitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")

	listCmd := newInstancePaginatedListCommand("list INSTANCE_ID", "List deployments", "/app-deployments", deploymentListColumns, out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get deployment", "/app-deployments/", deploymentColumns, out), waitCmd, newDeploymentCreateCommand(out), newDeploymentRedeployCommand(out))
	return cmd
}

func newInstanceBackupCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "backup",
		Aliases: []string{"backups"},
		Short:   "Manage app instance backups",
	}
	addOutputFlag(cmd, &out)
	listCmd := newInstanceFilteredListCommand("list INSTANCE_ID", "List backups", "/backups", backupColumns, out, true)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get backup", "/backups/", backupColumns, out), newBackupCreateCommand(out))
	return cmd
}

func newInstanceImportCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "import",
		Aliases: []string{"imports"},
		Short:   "Manage app instance imports",
	}
	addOutputFlag(cmd, &out)
	listCmd := newInstanceFilteredListCommand("list INSTANCE_ID", "List imports", "/imports", importListColumns, out, false)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get import", "/imports/", importColumns, out), newImportCreateCommand(out))
	return cmd
}

func newInstancePaginatedListCommand(use string, short string, path string, columns []string, out outputOptions) *cobra.Command {
	var page, pageSize int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"appInstanceId": []string{args[0]}}
			addPagination(query, page, pageSize)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	return cmd
}

func newInstanceFilteredListCommand(use string, short string, path string, columns []string, out outputOptions, backup bool) *cobra.Command {
	var serviceID, databaseID, databaseDBID, name string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"appInstanceId": []string{args[0]}}
			addQuery(query, "appServiceId", serviceID)
			addQuery(query, "databaseId", databaseID)
			addQuery(query, "databaseDbId", databaseDBID)
			if backup {
				addQuery(query, "backupName", name)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseID, "database", "", "Database ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "DB ID")
	if backup {
		cmd.Flags().StringVar(&name, "name", "", "Backup name")
	}
	return cmd
}

type instanceFilterMode int

const (
	instanceFilterFlag instanceFilterMode = iota
	instanceFilterArg
)

func newAppServiceCommand(use string, aliases []string, short string, mode instanceFilterMode) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	listCmd := &cobra.Command{
		Use:   instanceScopedListUse("list", mode),
		Short: "List app services",
		Args:  instanceScopedListArgs(mode),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode == instanceFilterArg {
				instanceID = args[0]
			}
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-services", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, serviceColumns)
		},
	}
	if mode == instanceFilterFlag {
		listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	}
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-services/"+args[0], serviceColumns)
		},
	}

	cmd.AddCommand(
		listCmd,
		getCmd,
		newAppServiceUpdateCommand(out),
		newAppServiceActionCommand(out),
		newAppServiceEnvVarCommand(out),
		newAppServiceHelmValueCommand(out),
		newAppServiceTokenCommand(out),
		newAppServiceAnnotationCommand(out),
		newAppServiceIntegrationCommand(out),
		newAppServiceSettingCommand(out),
		newAppServiceConfigCommand(out),
		newAppServiceLinkCommand(out),
		newAppServiceContainerCommand(out),
		newAppServiceVolumeCommand(out),
		newAppServiceResourcesCommand(out),
		newAppServiceDatabaseCommand(out),
		newAppServiceCronScheduleCommand(out),
		newAppServiceCronJobCommand(out),
		newAppServiceLogStreamCommand(out),
	)
	return cmd
}

func newAppServiceUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "disabled", "main", "replicas", "version", "scalability-enabled", "scalability-average-cpu", "scalability-min-replicas", "scalability-max-replicas") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				values := map[string]interface{}{}
				if value, ok := changedBool(cmd, "disabled"); ok {
					values["disabled"] = value
				}
				if value, ok := changedBool(cmd, "main"); ok {
					values["main"] = value
				}
				if value, ok := changedInt(cmd, "replicas"); ok {
					values["replicas"] = value
				}
				if value, ok := changedString(cmd, "version"); ok {
					values["version"] = value
				}
				scalability, changed, err := changedAppServiceScalability(cmd)
				if err != nil {
					return err
				}
				if changed {
					values["scalability"] = scalability
				}
				requestBody = values
			}

			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-services/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, serviceColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().Bool("disabled", false, "Set disabled state")
	cmd.Flags().Bool("main", false, "Set main service state")
	cmd.Flags().Int("replicas", 0, "Set replicas")
	cmd.Flags().String("version", "", "Set service version")
	cmd.Flags().Bool("scalability-enabled", false, "Enable app service autoscaling")
	cmd.Flags().Int("scalability-average-cpu", 0, "Target average CPU utilization percentage")
	cmd.Flags().Int("scalability-min-replicas", 0, "Minimum autoscaling replicas")
	cmd.Flags().Int("scalability-max-replicas", 0, "Maximum autoscaling replicas")
	return cmd
}

func changedAppServiceScalability(cmd *cobra.Command) (map[string]interface{}, bool, error) {
	enabledFlag := "scalability-enabled"
	childFlags := []string{"scalability-average-cpu", "scalability-min-replicas", "scalability-max-replicas"}
	if !hasChangedFlags(cmd.Flags(), append([]string{enabledFlag}, childFlags...)...) {
		return nil, false, nil
	}
	if !cmd.Flags().Changed(enabledFlag) {
		return nil, false, errors.New("--scalability-enabled is required when configuring autoscaling")
	}

	enabled, _ := cmd.Flags().GetBool(enabledFlag)
	values := map[string]interface{}{"enabled": enabled}
	if !enabled {
		if hasChangedFlags(cmd.Flags(), childFlags...) {
			return nil, false, errors.New("autoscaling options cannot be used when --scalability-enabled=false")
		}
		return values, true, nil
	}

	for _, spec := range []struct {
		flag string
		key  string
	}{
		{flag: "scalability-average-cpu", key: "averageCPU"},
		{flag: "scalability-min-replicas", key: "minReplicas"},
		{flag: "scalability-max-replicas", key: "maxReplicas"},
	} {
		value, ok := changedInt(cmd, spec.flag)
		if !ok {
			return nil, false, errors.Errorf("--%s is required when --scalability-enabled=true", spec.flag)
		}
		values[spec.key] = value
	}

	return values, true, nil
}

func newAppServiceActionCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "action ID ACTION",
		Short: "Run app service action",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), fmt.Sprintf("/app-services/%s/actions/%s", args[0], args[1]), nil, nil, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newAppServiceEnvVarCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "env-var",
		Aliases: []string{"env-vars"},
		Short:   "Manage app service environment variables",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service environment variables", "/app-services/%s/env-vars", appServiceEnvColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service environment variable", "/app-services/%s/env-vars", appServiceEnvColumns, out, []jsonFlagSpec{
			stringJSONFlag("workload", "workload", "Workload name", false),
			stringJSONFlag("container", "container", "Container name", false),
			stringJSONFlag("name", "name", "Environment variable name", true),
			stringJSONFlag("value", "value", "Environment variable value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
			boolJSONFlag("runtime", "runtime", "Expose at runtime", false),
			boolJSONFlag("build", "build", "Expose during builds", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update app service environment variable", "/app-service-env-vars/%s", appServiceEnvColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Environment variable value", false),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
			boolJSONFlag("runtime", "runtime", "Expose at runtime", false),
			boolJSONFlag("build", "build", "Expose during builds", false),
		}),
		newServiceChildDeleteCommand("Delete app service environment variable", "/app-service-env-vars/%s", out),
	)
	return cmd
}

func newAppServiceHelmValueCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "helm-value",
		Aliases: []string{"helm-values"},
		Short:   "Manage app service Helm values",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service Helm values", "/app-services/%s/helm-values", appServiceValueColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service Helm value", "/app-services/%s/helm-values", appServiceValueColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Helm value name", true),
			stringJSONFlag("value", "value", "Helm value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
		}),
		newServiceChildUpdateCommand("update ID", "Update app service Helm value", "/app-service-helm-values/%s", appServiceValueColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Helm value", true),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
		}),
		newServiceChildDeleteCommand("Delete app service Helm value", "/app-service-helm-values/%s", out),
	)
	return cmd
}

func newAppServiceTokenCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "token",
		Aliases: []string{"tokens"},
		Short:   "Manage app service tokens",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service tokens", "/app-services/%s/tokens", appServiceTokenColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service token", "/app-services/%s/tokens", appServiceTokenColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Token name", true),
			stringJSONFlag("value", "value", "Token value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
		}),
		newServiceChildUpdateCommand("update ID", "Update app service token", "/app-service-tokens/%s", appServiceTokenColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Token value", true),
			requiredChangedBoolJSONFlag("secret", "secret", "Whether the value is secret"),
		}),
		newServiceChildDeleteCommand("Delete app service token", "/app-service-tokens/%s", out),
	)
	return cmd
}

func newAppServiceAnnotationCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "annotation",
		Aliases: []string{"annotations"},
		Short:   "Manage app service annotations",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service annotations", "/app-services/%s/annotations", appServiceAnnotationColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service annotation", "/app-services/%s/annotations", appServiceAnnotationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Annotation name", true),
			stringJSONFlag("value", "value", "Annotation value", true),
		}),
		newServiceChildDeleteCommand("Delete app service annotation", "/app-service-annotations/%s", out),
	)
	return cmd
}

func newAppServiceIntegrationCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "integration",
		Aliases: []string{"integrations"},
		Short:   "Manage app service integrations",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service integrations", "/app-services/%s/integrations", appServiceIntegrationColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service integration", "/app-services/%s/integrations", appServiceIntegrationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Integration slot name", true),
			intJSONFlag("integration", "integrationId", "Integration ID", true),
		}),
		newServiceChildDeleteCommand("Delete app service integration", "/app-service-integrations/%s", out),
	)
	return cmd
}

func newAppServiceSettingCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "setting",
		Aliases: []string{"settings"},
		Short:   "Manage app service settings",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service settings", "/app-services/%s/settings", appServiceSettingColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set app service setting", "/app-services/%s/settings/%s", appServiceSettingColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Setting value", true),
		}),
	)
	return cmd
}

func newAppServiceConfigCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configs"},
		Short:   "Manage app service configs",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service configs", "/app-services/%s/configs", appServiceConfigColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set app service config", "/app-services/%s/configs/%s", operationColumns, out, []jsonFlagSpec{
			stringJSONFlag("config", "config", "Config override", false),
			boolJSONFlag("disabled", "disabled", "Set disabled state", false),
		}),
	)
	return cmd
}

func newAppServiceLinkCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "link",
		Aliases: []string{"links"},
		Short:   "Manage app service links",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service links", "/app-services/%s/links", appServiceLinkColumns, out),
		newServiceChildSetCommand("set SERVICE_ID NAME", "Set app service link", "/app-services/%s/links/%s", operationColumns, out, []jsonFlagSpec{
			intJSONFlag("linked-service", "linkedAppServiceId", "Linked app service ID", false),
		}),
	)
	return cmd
}

func newAppServiceContainerCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "container",
		Aliases: []string{"containers"},
		Short:   "List app service containers",
	}
	cmd.AddCommand(newServiceChildListCommand("list SERVICE_ID", "List app service containers", "/app-services/%s/containers", appServiceContainerColumns, out))
	return cmd
}

func newAppServiceVolumeCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"volumes"},
		Short:   "Manage app service volumes",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service volumes", "/app-services/%s/volumes", appServiceVolumeColumns, out),
		newServiceChildCreateCommand("add SERVICE_ID", "Add an optional app service volume", "/app-services/%s/volumes", operationColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Volume name", true),
			intJSONFlag("size", "size", "Volume size in GiB", false),
			stringJSONFlag("storage-class", "storageClassName", "Kubernetes storage class", false),
		}),
		newServiceChildListCommand("storage-classes SERVICE_ID", "List app service volume storage-class state", "/app-services/%s/options/volume-storage-classes", []string{"volumeId", "configuredStorageClassName", "effectiveStorageClassNames", "status"}, out),
	)
	return cmd
}

func newAppServiceResourcesCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Manage app service resources",
	}
	cmd.AddCommand(newServiceChildDirectSetCommand("set SERVICE_ID", "Set app service resources", "/app-services/%s/resources", operationColumns, out, resourceJSONFlags()))
	return cmd
}

func newAppServiceDatabaseCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "database",
		Short: "Manage app service database binding",
	}
	cmd.AddCommand(newServiceChildDirectSetCommand("set SERVICE_ID", "Set app service database binding", "/app-services/%s/database", serviceColumns, out, []jsonFlagSpec{
		intJSONFlag("database-db", "databaseDbId", "Database DB ID", false),
		intJSONFlag("database-user", "databaseUserId", "Database user ID", false),
	}))
	return cmd
}

func newAppServiceCronScheduleCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cron-schedule",
		Aliases: []string{"cron-schedules", "cron"},
		Short:   "Manage app service cron schedules",
	}
	cmd.AddCommand(
		newServiceChildListCommand("list SERVICE_ID", "List app service cron schedules", "/app-services/%s/cron-schedules", appServiceCronScheduleColumns, out),
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service cron schedule", "/app-services/%s/cron-schedules", appServiceCronScheduleColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Stable cron schedule name", false),
			stringJSONFlag("title", "title", "Cron schedule title", true),
			stringJSONFlag("crontab", "crontab", "Crontab expression", true),
			stringJSONFlag("command", "command", "Command to run", true),
			stringJSONFlag("workload", "workload", "Workload name", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update app service cron schedule", "/app-service-cron-schedules/%s", appServiceCronScheduleColumns, out, []jsonFlagSpec{
			boolJSONFlag("disabled", "disabled", "Set disabled state", false),
			stringJSONFlag("title", "title", "Cron schedule title", false),
			stringJSONFlag("crontab", "crontab", "Crontab expression", false),
			stringJSONFlag("command", "command", "Command to run", false),
			stringJSONFlag("workload", "workload", "Workload name", false),
		}),
		newServiceChildDeleteCommand("Delete app service cron schedule", "/app-service-cron-schedules/%s", out),
		newAppServiceCronScheduleRunCommand(out),
	)
	return cmd
}

func newAppServiceCronScheduleRunCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "run ID",
		Short: "Run app service cron schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/app-service-cron-schedules/%s/run", args[0]), nil, nil, &result); err != nil {
				return err
			}
			if wait.wait {
				taskID := firstTaskID(result)
				if taskID == "" {
					rows := asRows(normalizeItem(result))
					if len(rows) != 0 {
						taskID = firstScalarPath(rows[0], "id")
					}
				}
				if taskID != "" {
					result, err = waitForTask(cmd.Context(), client, taskID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newAppServiceCronJobCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cron-job",
		Aliases: []string{"cron-jobs"},
		Short:   "Read app service cron jobs",
	}
	var scheduleID string
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list SERVICE_ID",
		Short: "List app service cron jobs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{"appServiceId": []string{args[0]}}
			addQuery(query, "scheduleId", scheduleID)
			addPagination(query, page, pageSize)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-service-cron-jobs", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appServiceCronJobColumns)
		},
	}
	listCmd.Flags().StringVar(&scheduleID, "schedule", "", "Cron schedule ID")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get app service cron job", "/app-service-cron-jobs/", appServiceCronJobColumns, out))
	return cmd
}

func newAppServiceLogStreamCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log-stream",
		Aliases: []string{"log-streams"},
		Short:   "Manage app service log streams",
	}
	cmd.AddCommand(
		newServiceChildCreateCommand("create SERVICE_ID", "Create app service log stream", "/app-services/%s/log-streams", logStreamColumns, out, []jsonFlagSpec{
			stringJSONFlag("workload", "workload", "Workload name", false),
			stringJSONFlag("container", "container", "Container name", false),
			stringJSONFlag("pod", "pod", "Pod name", false),
		}),
		newLogStreamActionCommand("start ID", "Start log stream", "/log-streams/%s/start", out),
		newLogStreamActionCommand("keep-alive ID", "Keep log stream alive", "/log-streams/%s/keep-alive", out),
		newLogStreamActionCommand("stop ID", "Stop log stream", "/log-streams/%s/stop", out),
	)
	return cmd
}

func newLogStreamActionCommand(use string, short string, pathPattern string, out outputOptions) *cobra.Command {
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
			if err := client.Post(cmd.Context(), escapedPath(pathPattern, args[0]), nil, nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
}

func newAppRouteCommand(use string, aliases []string, short string, mode instanceFilterMode) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	listCmd := &cobra.Command{
		Use:   instanceScopedListUse("list", mode),
		Short: "List app routes",
		Args:  instanceScopedListArgs(mode),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode == instanceFilterArg {
				instanceID = args[0]
			}
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-routes", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeListColumns)
		},
	}
	if mode == instanceFilterFlag {
		listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	}
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-routes/"+args[0], routeColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newAppRouteCreateCommand(out), newAppRouteUpdateCommand(out), newDeleteCommand("delete ID", "Delete app route", "/app-routes/", routeColumns, out))
	return cmd
}

func newAppPortCommand(use string, aliases []string, short string, mode instanceFilterMode) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	listCmd := &cobra.Command{
		Use:   instanceScopedListUse("list", mode),
		Short: "List app ports",
		Args:  instanceScopedListArgs(mode),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode == instanceFilterArg {
				instanceID = args[0]
			}
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-ports", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appPortListColumns)
		},
	}
	if mode == instanceFilterFlag {
		listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	}
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-ports/"+args[0], appPortColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd)
	return cmd
}

func newAppCertCommand(use string, aliases []string, short string, mode instanceFilterMode) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
	}
	addOutputFlag(cmd, &out)

	var orgID, instanceID, routeID, host, status string
	listCmd := &cobra.Command{
		Use:   instanceScopedListUse("list", mode),
		Short: "List app certificates",
		Args:  instanceScopedListArgs(mode),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode == instanceFilterArg {
				instanceID = args[0]
			}
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/certs", query, &result); err != nil {
				return err
			}
			result = filterCerts(result, instanceID, routeID, host, status)
			return printClientResult(cmd, client, out, result, certColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	if mode == instanceFilterFlag {
		listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	}
	listCmd.Flags().StringVar(&routeID, "route", "", "App route ID")
	listCmd.Flags().StringVar(&host, "host", "", "Certificate host")
	listCmd.Flags().StringVar(&status, "status", "", "Certificate status")
	defaultToList(cmd, listCmd)

	cmd.AddCommand(
		listCmd,
		newGetCommand("get ID", "Get app certificate", "/certs/", certColumns, out),
	)
	return cmd
}

func filterCerts(value interface{}, instanceID string, routeID string, host string, status string) interface{} {
	if instanceID == "" && routeID == "" && host == "" && status == "" {
		return value
	}
	return filterResponseItems(value, func(row map[string]interface{}) bool {
		if instanceID != "" && firstScalarPath(row, "appInstanceId", "instanceId", "appInstance.id", "instance.id") != instanceID {
			return false
		}
		if routeID != "" && firstScalarPath(row, "appRouteId", "routeId", "appRoute.id", "route.id") != routeID {
			return false
		}
		if host != "" && firstScalarPath(row, "host", "hostname", "domain", "commonName") != host {
			return false
		}
		if status != "" && firstScalarPath(row, "status") != status {
			return false
		}
		return true
	})
}

func instanceScopedListUse(base string, mode instanceFilterMode) string {
	if mode == instanceFilterArg {
		return base + " INSTANCE_ID"
	}
	return base
}

func instanceScopedListArgs(mode instanceFilterMode) cobra.PositionalArgs {
	if mode == instanceFilterArg {
		return cobra.ExactArgs(1)
	}
	return cobra.NoArgs
}

func newAppRouteCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var serviceID, host, path, pathType, action, authLogin, authPassword, redirectHost, redirectPath, redirectScheme string
	var port, authID, redirectStatusCode int
	var main, primary, letsencrypt bool
	var options []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create app route",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(serviceID, "--service"); err != nil {
					return err
				}
				if err := requireFlag(host, "--host"); err != nil {
					return err
				}
				if err := requireIntFlag(port, "--port"); err != nil {
					return err
				}
				appServiceID, err := strconv.Atoi(serviceID)
				if err != nil {
					return errors.WithStack(err)
				}
				values := map[string]interface{}{
					"appServiceId":   appServiceID,
					"host":           host,
					"port":           port,
					"main":           main,
					"primary":        primary,
					"letsencrypt":    letsencrypt,
					"path":           path,
					"pathType":       pathType,
					"action":         action,
					"authLogin":      authLogin,
					"authPassword":   authPassword,
					"redirectHost":   redirectHost,
					"redirectPath":   redirectPath,
					"redirectScheme": redirectScheme,
				}
				if authID != 0 {
					values["authId"] = authID
				}
				if redirectStatusCode != 0 {
					values["redirectStatusCode"] = redirectStatusCode
				}
				if err := addOptionalNameValueInputs(values, "options", options, "--option"); err != nil {
					return err
				}
				requestBody = bodyFromMap(values)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-routes", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&host, "host", "", "Route host")
	cmd.Flags().IntVar(&port, "port", 0, "Service port")
	cmd.Flags().BoolVar(&main, "main", false, "Make route the app instance main route")
	cmd.Flags().BoolVar(&primary, "primary", false, "Make route primary for the service endpoint")
	cmd.Flags().BoolVar(&letsencrypt, "letsencrypt", false, "Request Let's Encrypt certificate")
	cmd.Flags().StringVar(&path, "path", "", "Route path")
	cmd.Flags().StringVar(&pathType, "path-type", "", "Route path type: PREFIX or EXACT")
	cmd.Flags().StringVar(&action, "action", "", "Route action: SERVE or REDIRECT")
	cmd.Flags().IntVar(&authID, "auth", 0, "Route auth ID")
	cmd.Flags().StringVar(&authLogin, "auth-login", "", "Route auth login")
	cmd.Flags().StringVar(&authPassword, "auth-password", "", "Route auth password")
	cmd.Flags().StringVar(&redirectHost, "redirect-host", "", "Redirect host")
	cmd.Flags().StringVar(&redirectPath, "redirect-path", "", "Redirect path")
	cmd.Flags().StringVar(&redirectScheme, "redirect-scheme", "", "Redirect scheme")
	cmd.Flags().IntVar(&redirectStatusCode, "redirect-status-code", 0, "Redirect status code")
	cmd.Flags().StringArrayVar(&options, "option", nil, "Route option as NAME=VALUE; repeatable")
	return cmd
}

func newAppRouteUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "disabled", "main", "primary", "path", "path-type", "action", "redirect-host", "redirect-path", "redirect-scheme", "redirect-status-code", "option") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				values := map[string]interface{}{}
				if value, ok := changedBool(cmd, "disabled"); ok {
					values["disabled"] = value
				}
				if value, ok := changedBool(cmd, "main"); ok {
					values["main"] = value
				}
				if value, ok := changedBool(cmd, "primary"); ok {
					values["primary"] = value
				}
				if value, ok := changedString(cmd, "path"); ok {
					values["path"] = value
				}
				if value, ok := changedString(cmd, "path-type"); ok {
					values["pathType"] = value
				}
				if value, ok := changedString(cmd, "action"); ok {
					values["action"] = value
				}
				if value, ok := changedString(cmd, "redirect-host"); ok {
					values["redirectHost"] = value
				}
				if value, ok := changedString(cmd, "redirect-path"); ok {
					values["redirectPath"] = value
				}
				if value, ok := changedString(cmd, "redirect-scheme"); ok {
					values["redirectScheme"] = value
				}
				if value, ok := changedInt(cmd, "redirect-status-code"); ok {
					values["redirectStatusCode"] = value
				}
				options, _ := cmd.Flags().GetStringArray("option")
				if err := addOptionalNameValueInputs(values, "options", options, "--option"); err != nil {
					return err
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-routes/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().Bool("disabled", false, "Set disabled state")
	cmd.Flags().Bool("main", false, "Set main route state")
	cmd.Flags().Bool("primary", false, "Set primary route state")
	cmd.Flags().String("path", "", "Route path")
	cmd.Flags().String("path-type", "", "Route path type: PREFIX or EXACT")
	cmd.Flags().String("action", "", "Route action: SERVE or REDIRECT")
	cmd.Flags().String("redirect-host", "", "Redirect host")
	cmd.Flags().String("redirect-path", "", "Redirect path")
	cmd.Flags().String("redirect-scheme", "", "Redirect scheme")
	cmd.Flags().Int("redirect-status-code", 0, "Redirect status code")
	cmd.Flags().StringArray("option", nil, "Route option as NAME=VALUE; repeatable")
	return cmd
}

func newBuildCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Manage app builds",
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List builds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-builds", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, buildListColumns)
		},
	}
	listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get build",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-builds/"+args[0], buildColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newBuildCreateCommand(out), newBuildDeployCommand(out))
	return cmd
}

func newBuildCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var services []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create build",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				parsedServices, err := parseIntValues(services, "--service")
				if err != nil {
					return err
				}
				if len(parsedServices) == 0 {
					return errors.New("--service is required unless --data/--file is provided")
				}
				requestBody = map[string]interface{}{"appServiceIds": parsedServices}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-builds", nil, requestBody, &result); err != nil {
				return err
			}
			if handled, err := printBuildTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			columns := buildColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringArrayVar(&services, "service", nil, "App service ID to build; repeatable or comma-separated")
	return cmd
}

func newBuildDeployCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "deploy ID",
		Short: "Deploy build",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-builds/"+args[0]+"/deploy", nil, nil, &result); err != nil {
				return err
			}
			if handled, err := printDeploymentTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newDeploymentCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "deployment",
		Short: "Manage deployments",
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-deployments", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, deploymentListColumns)
		},
	}
	listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-deployments/"+args[0], deploymentColumns)
		},
	}
	waitCmd := &cobra.Command{
		Use:   "wait ID",
		Short: "Wait for deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			result, err := waitForDeployment(cmd.Context(), client, args[0], timeout)
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	waitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")

	cmd.AddCommand(listCmd, getCmd, waitCmd, newDeploymentCreateCommand(out), newDeploymentRedeployCommand(out))
	return cmd
}

func newDeploymentCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var services []string
	var force, skipPostDeploy, skipRollback bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if len(services) == 0 {
					return errors.New("--service is required")
				}
				serviceInputs := make([]map[string]interface{}, 0, len(services))
				for _, service := range services {
					serviceID, err := strconv.Atoi(service)
					if err != nil {
						return errors.WithStack(err)
					}
					serviceInputs = append(serviceInputs, map[string]interface{}{
						"appServiceId":       serviceID,
						"force":              force,
						"skipPostDeployment": skipPostDeploy,
					})
				}
				requestBody = map[string]interface{}{
					"services":     serviceInputs,
					"skipRollback": skipRollback,
				}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-deployments", nil, requestBody, &result); err != nil {
				return err
			}
			if handled, err := printDeploymentTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringArrayVar(&services, "service", nil, "App service ID to deploy; repeatable")
	cmd.Flags().BoolVar(&force, "force", false, "Force service deployment")
	cmd.Flags().BoolVar(&skipPostDeploy, "skip-post-deploy", false, "Skip post-deployment scripts")
	cmd.Flags().BoolVar(&skipRollback, "skip-rollback", false, "Skip rollback on failure")
	return cmd
}

func newDeploymentRedeployCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "redeploy ID",
		Short: "Redeploy deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-deployments/"+args[0]+"/redeploy", nil, nil, &result); err != nil {
				return err
			}
			if handled, err := printDeploymentTaskLogs(cmd.Context(), cmd, client, out, result); handled || err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newBackupCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups",
	}
	addOutputFlag(cmd, &out)
	listCmd := newFilteredListCommand("list", "List backups", "/backups", backupColumns, out, true)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get backup", "/backups/", backupColumns, out), newBackupCreateCommand(out))
	return cmd
}

func newBackupCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var serviceID, databaseDBID, bucket, backupName, storageClass string
	var integrationID int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireIntFlag(integrationID, "--integration"); err != nil {
					return err
				}
				if err := requireFlag(bucket, "--bucket"); err != nil {
					return err
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"integrationId": integrationID,
					"bucket":        bucket,
					"appServiceId":  optionalInt(serviceID),
					"databaseDbId":  optionalInt(databaseDBID),
					"backupName":    backupName,
					"storageClass":  storageClass,
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/backups", nil, requestBody, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().IntVar(&integrationID, "integration", 0, "Storage integration ID")
	cmd.Flags().StringVar(&bucket, "bucket", "", "Storage bucket")
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "DB ID")
	cmd.Flags().StringVar(&backupName, "name", "", "Backup name")
	cmd.Flags().StringVar(&storageClass, "storage-class", "", "Storage class")
	return cmd
}

func newImportCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Manage imports",
	}
	addOutputFlag(cmd, &out)
	listCmd := newFilteredListCommand("list", "List imports", "/imports", importListColumns, out, false)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(listCmd, newGetCommand("get ID", "Get import", "/imports/", importColumns, out), newImportCreateCommand(out))
	return cmd
}

func newImportCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var serviceID, databaseDBID, source, importURL, importName, backupID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create import",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(source, "--source"); err != nil {
					return err
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"appServiceId": optionalInt(serviceID),
					"databaseDbId": optionalInt(databaseDBID),
					"import": bodyFromMap(map[string]interface{}{
						"source":     source,
						"url":        importURL,
						"importName": importName,
						"backupId":   optionalInt(backupID),
					}),
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/imports", nil, requestBody, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "DB ID")
	cmd.Flags().StringVar(&source, "source", "", "Import source")
	cmd.Flags().StringVar(&importURL, "url", "", "Import archive URL")
	cmd.Flags().StringVar(&importName, "name", "", "Import name")
	cmd.Flags().StringVar(&backupID, "backup", "", "Backup ID")
	return cmd
}

func newTaskCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}
	addOutputFlag(cmd, &out)

	var scope, view, orgID, projectIDs, statuses, names, search, appID, instanceID, stackID, databaseID, clusterID, serviceID, integrationID, providerID string
	var withoutOrigin bool
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "scope", scope)
			addQuery(query, "view", view)
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "statuses", statuses)
			addQuery(query, "names", names)
			addQuery(query, "search", search)
			addQuery(query, "appId", appID)
			addQuery(query, "appInstanceId", instanceID)
			addQuery(query, "stackId", stackID)
			addQuery(query, "databaseId", databaseID)
			addQuery(query, "clusterId", clusterID)
			addQuery(query, "serviceId", serviceID)
			addQuery(query, "integrationId", integrationID)
			addQuery(query, "providerId", providerID)
			if withoutOrigin {
				query.Set("withoutOrigin", "true")
			}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/tasks", query, &result); err != nil {
				return err
			}
			if strings.EqualFold(view, "tree") && outputFormat(cmd, out) != outputJSON {
				result = taskTreeListDisplayResult(result)
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	listCmd.Flags().StringVar(&scope, "scope", "", "Task scope: project_and_org, org_only, or user_only")
	listCmd.Flags().StringVar(&view, "view", "", "Task view: flat or tree")
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&statuses, "statuses", "", "Comma-separated statuses")
	listCmd.Flags().StringVar(&names, "names", "", "Comma-separated exact task names")
	listCmd.Flags().StringVar(&search, "search", "", "Search query")
	listCmd.Flags().StringVar(&appID, "app", "", "App ID")
	listCmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	listCmd.Flags().StringVar(&stackID, "stack", "", "Stack ID")
	listCmd.Flags().StringVar(&databaseID, "database", "", "Database ID")
	listCmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID")
	listCmd.Flags().StringVar(&serviceID, "service", "", "Service ID")
	listCmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")
	listCmd.Flags().StringVar(&providerID, "provider", "", "Provider ID")
	listCmd.Flags().BoolVar(&withoutOrigin, "without-origin", false, "Only tasks without origin")
	_ = listCmd.Flags().MarkDeprecated("without-origin", "use --view tree")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	defaultToList(cmd, listCmd)

	getCmd := newTaskGetCommand(out)
	waitCmd := &cobra.Command{
		Use:   "wait ID",
		Short: "Wait for task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			result, err := waitForTask(cmd.Context(), client, args[0], timeout)
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	waitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")

	var logJob string
	var logAllJobs bool
	logsCmd := &cobra.Command{
		Use:   "logs ID",
		Short: "Show task step logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			return printTaskLogs(cmd.Context(), cmd, client, out, args[0], logJob, logAllJobs)
		},
	}
	logsCmd.Flags().StringVar(&logJob, "job", "", "Job ID or name to show logs for")
	logsCmd.Flags().BoolVar(&logAllJobs, "all-jobs", false, "Show logs for all jobs when a task has multiple jobs")

	cancelCmd := newTaskCancelCommand(out)
	repeatCmd := newTaskRepeatCommand(out)
	cmd.AddCommand(listCmd, getCmd, waitCmd, logsCmd, newTaskJobCommand(out), newTaskStepCommand(out), cancelCmd, repeatCmd)
	return cmd
}

// taskTreeListDisplayResult replaces paginated roots with a preorder display
// list built from REST treeItems. Older backends omit treeItems, in which case
// the original root-only response remains unchanged.
func taskTreeListDisplayResult(value interface{}) interface{} {
	response, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	treeItems := asRows(response["treeItems"])
	if len(treeItems) == 0 {
		return value
	}

	tasksByID := make(map[string]map[string]interface{}, len(treeItems))
	childIDsByParentID := make(map[string][]string)
	for _, item := range treeItems {
		tasks := asRows(item["task"])
		if len(tasks) == 0 {
			continue
		}
		task := tasks[0]
		taskID := firstScalarPath(task, "id")
		if taskID == "" {
			continue
		}
		if _, exists := tasksByID[taskID]; exists {
			continue
		}
		tasksByID[taskID] = task
		if parentID := firstScalarPath(item, "parentId", "parentID"); parentID != "" {
			childIDsByParentID[parentID] = append(childIDsByParentID[parentID], taskID)
		}
	}

	rootRows := asRows(response["items"])
	for _, root := range rootRows {
		rootID := firstScalarPath(root, "id")
		if rootID == "" {
			continue
		}
		if _, exists := tasksByID[rootID]; !exists {
			tasksByID[rootID] = root
		}
	}

	rows := make([]map[string]interface{}, 0, len(tasksByID))
	visited := make(map[string]struct{}, len(tasksByID))
	var appendTree func(string, int)
	appendTree = func(taskID string, depth int) {
		if _, seen := visited[taskID]; seen {
			return
		}
		task, exists := tasksByID[taskID]
		if !exists {
			return
		}
		visited[taskID] = struct{}{}

		row := cloneRow(task)
		if depth > 0 {
			if title := firstScalarPath(row, "title"); title != "" {
				row["title"] = strings.Repeat("  ", depth-1) + "↳ " + title
			}
		}
		rows = append(rows, row)
		for _, childID := range childIDsByParentID[taskID] {
			appendTree(childID, depth+1)
		}
	}
	for _, root := range rootRows {
		if rootID := firstScalarPath(root, "id"); rootID != "" {
			appendTree(rootID, 0)
		}
	}
	if len(rows) == 0 {
		return value
	}

	result := cloneRow(response)
	result["items"] = rows
	return result
}

func newTaskGetCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/tasks/"+args[0], nil, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, taskGetColumnsFor(result))
		},
	}
}

func taskGetColumnsFor(value interface{}) []string {
	rows := asRows(normalizeItem(value))
	if len(rows) == 0 {
		return taskGetColumns
	}

	columns := make([]string, 0, len(taskGetColumns))
	for _, column := range taskGetColumns {
		if isOptionalTaskRelationColumn(column) && !displayColumnPresent(rows[0], column) {
			continue
		}
		columns = append(columns, column)
	}
	return columns
}

func isOptionalTaskRelationColumn(column string) bool {
	switch column {
	case "app", "instance", "service", "database", "databaseDb", "projects", "originTask", "repeatedTask", "spawnedTasks":
		return true
	default:
		return false
	}
}

func displayColumnPresent(row map[string]interface{}, column string) bool {
	if formatColumnValue(row, column) != "" {
		return true
	}
	relation, ok := relationColumnFor(column)
	return ok && firstRelationID(row, relation) != ""
}

func newTaskJobCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "job",
		Aliases: []string{"jobs"},
		Short:   "Show task jobs",
	}

	listCmd := &cobra.Command{
		Use:   "list TASK_ID",
		Short: "List task jobs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			task, err := fetchTask(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return printResult(cmd, out, taskJobRows(task), taskJobColumns)
		},
	}
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get TASK_ID JOB",
		Short: "Get task job",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			task, err := fetchTask(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			job, ok := findTaskJob(taskLogJobs(task), args[1])
			if !ok {
				return errors.Errorf("job %q not found", args[1])
			}
			return printGetResult(cmd, out, taskJobRow(job), taskJobColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd)
	return cmd
}

func newTaskStepCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "step",
		Aliases: []string{"steps"},
		Short:   "Show task steps",
	}

	listCmd := &cobra.Command{
		Use:   "list TASK_ID",
		Short: "List task steps",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			task, err := fetchTask(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return printResult(cmd, out, taskStepRows(task), taskStepColumns)
		},
	}
	defaultToList(cmd, listCmd)

	getCmd := &cobra.Command{
		Use:   "get TASK_ID STEP",
		Short: "Get task step",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			task, err := fetchTask(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			step, job, ok := findTaskStep(taskLogJobs(task), args[1])
			if !ok {
				return errors.Errorf("step %q not found", args[1])
			}
			return printGetResult(cmd, out, taskStepRow(step, job), taskStepColumns)
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs STEP_ID",
		Short: "Show task step logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			result, err := fetchTaskStepLogs(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			lines := logLines(result)
			if len(lines) == 0 {
				printNoLogs(cmd, out)
				return nil
			}
			for _, line := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd, getCmd, logsCmd)
	return cmd
}

func fetchTask(ctx context.Context, client *rest.Client, id string) (interface{}, error) {
	var result interface{}
	if err := client.Get(ctx, "/tasks/"+id, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func taskJobRows(task interface{}) []map[string]interface{} {
	jobs := taskLogJobs(task)
	rows := make([]map[string]interface{}, 0, len(jobs))
	for _, job := range jobs {
		rows = append(rows, taskJobRow(job))
	}
	return rows
}

func taskJobRow(job taskLogJob) map[string]interface{} {
	return map[string]interface{}{
		"id":        job.id,
		"name":      jobLogNameOrFallback(job),
		"status":    job.status,
		"logStatus": job.logStatus,
		"system":    job.system,
		"startedAt": job.startedAt,
		"duration":  job.duration,
		"steps":     len(job.steps),
	}
}

func taskStepRows(task interface{}) []map[string]interface{} {
	jobs := taskLogJobs(task)
	rows := make([]map[string]interface{}, 0)
	for _, job := range jobs {
		for _, step := range job.steps {
			rows = append(rows, taskStepRow(step, job))
		}
	}
	return rows
}

func taskStepRow(step taskLogStep, job taskLogJob) map[string]interface{} {
	return map[string]interface{}{
		"id":        step.id,
		"name":      stepLogNameOrFallback(step),
		"status":    step.status,
		"logStatus": step.logStatus,
		"system":    step.system,
		"startedAt": step.startedAt,
		"duration":  step.duration,
		"job":       jobLogNameWithID(job),
	}
}

func findTaskJob(jobs []taskLogJob, filter string) (taskLogJob, bool) {
	filter = normalizeDisplayToken(filter)
	for _, job := range jobs {
		if normalizeDisplayToken(job.id) == filter || normalizeDisplayToken(job.name) == filter || normalizeDisplayToken(jobLogNameOrFallback(job)) == filter {
			return job, true
		}
	}
	return taskLogJob{}, false
}

func findTaskStep(jobs []taskLogJob, filter string) (taskLogStep, taskLogJob, bool) {
	filter = normalizeDisplayToken(filter)
	for _, job := range jobs {
		for _, step := range job.steps {
			if normalizeDisplayToken(step.id) == filter || normalizeDisplayToken(step.name) == filter || normalizeDisplayToken(stepLogNameOrFallback(step)) == filter {
				return step, job, true
			}
		}
	}
	return taskLogStep{}, taskLogJob{}, false
}

func newTaskCancelCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel ID",
		Short: "Cancel task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Cancel task?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/tasks/"+args[0]+"/cancel", nil, nil, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newTaskRepeatCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var force bool
	cmd := &cobra.Command{
		Use:   "repeat ID",
		Short: "Repeat task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				requestBody = map[string]interface{}{"force": force}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/tasks/"+args[0]+"/repeat", nil, requestBody, &result); err != nil {
				return err
			}
			columns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				columns = taskColumns
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVar(&force, "force", false, "Force repeat")
	return cmd
}

func newFilteredListCommand(use string, short string, path string, columns []string, out outputOptions, backup bool) *cobra.Command {
	var instanceID, serviceID, databaseID, databaseDBID, name string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if instanceID == "" && serviceID == "" && databaseID == "" && databaseDBID == "" {
				return errors.New("one of --instance, --service, --database, or --database-db is required")
			}
			query := url.Values{}
			addQuery(query, "appInstanceId", instanceID)
			addQuery(query, "appServiceId", serviceID)
			addQuery(query, "databaseId", databaseID)
			addQuery(query, "databaseDbId", databaseDBID)
			if backup {
				addQuery(query, "backupName", name)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVarP(&instanceID, "instance", "i", "", "App instance ID")
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseID, "database", "", "Database ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "DB ID")
	if backup {
		cmd.Flags().StringVar(&name, "name", "", "Backup name")
	}
	return cmd
}

func newDeleteCommand(use string, short string, pathPrefix string, columns []string, out outputOptions) *cobra.Command {
	var yes bool
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, short+"?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), pathPrefix+args[0], nil, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newGetCommand(use string, short string, pathPrefix string, columns []string, out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, pathPrefix+args[0], columns)
		},
	}
}

func getAndPrint(cmd *cobra.Command, out outputOptions, path string, columns []string) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, nil, &result); err != nil {
		return err
	}
	return printClientGetResult(cmd, client, out, result, columns)
}

func getAndPrintWithInstances(cmd *cobra.Command, out outputOptions, path string, columns []string, filterName string, filterValue string) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, nil, &result); err != nil {
		return err
	}
	if outputFormat(cmd, out) != outputJSON {
		enrichInstancesSummary(cmd.Context(), client, normalizeItem(result), filterName, filterValue)
	}
	return printClientGetResult(cmd, client, out, result, columns)
}

func enrichInstancesSummary(ctx context.Context, client *rest.Client, value interface{}, filterName string, filterValue string) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}
	if instances := firstNonNilPath(rows[0], "instances", "appInstances"); instances != nil {
		if filterName == "appId" && !hasStackReference(rows[0]) {
			enrichAppRowWithEmbeddedInstanceStack(rows[0], instances)
		}
		return
	}

	if filterValue == "" {
		filterValue = firstScalarPath(rows[0], "id")
	}
	if filterValue == "" {
		return
	}

	query := url.Values{filterName: []string{filterValue}}
	addQuery(query, "orgId", firstScalarPath(rows[0], "orgId", "org.id"))
	if filterName == "clusterId" {
		query.Set("clusterApp", "false")
	}

	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return
	}
	instances := normalizeItems(result)
	rows[0]["appInstances"] = instances
	if filterName == "appId" && !hasStackReference(rows[0]) {
		enrichAppRowsWithInstanceStacks(rows, responseRows(instances))
	}
}

func enrichAppStacksFromInstances(ctx context.Context, client *rest.Client, value interface{}, appQuery url.Values) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}

	needsInstances := false
	needsStack := false
	for _, row := range rows {
		instances := firstNonNilPath(row, "instances", "appInstances")
		if instances != nil {
			if !hasStackReference(row) {
				enrichAppRowWithEmbeddedInstanceStack(row, instances)
			}
			if count, ok := appInstanceCount(instances); ok {
				row["instances"] = count
			} else {
				needsInstances = true
			}
		} else {
			needsInstances = true
		}
		if !hasStackReference(row) {
			needsStack = true
		}
	}
	if !needsInstances && !needsStack {
		return
	}

	query := url.Values{}
	for _, name := range []string{"orgId", "projectIds", "clusterApp"} {
		if values, ok := appQuery[name]; ok {
			query[name] = append([]string{}, values...)
		}
	}

	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return
	}
	instances := responseRows(result)
	enrichAppRowsWithInstanceCounts(rows, instances)
	if needsStack {
		enrichAppRowsWithInstanceStacks(rows, instances)
	}
}

func appInstanceCount(value interface{}) (int, bool) {
	if isCollection(value) {
		return len(responseRows(value)), true
	}
	if count := scalarString(value); count != "" {
		parsed, err := strconv.Atoi(count)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func enrichAppRowWithEmbeddedInstanceStack(app map[string]interface{}, instances interface{}) bool {
	for _, instance := range responseRows(instances) {
		if stack := firstStackObject(instance); stack != nil {
			app["stack"] = stack
			return true
		}
		if stackID := firstRelationID(instance, relationColumns["stack"]); stackID != "" {
			app["stackId"] = stackID
			return true
		}
	}
	return false
}

func enrichAppRowsWithInstanceStacks(apps []map[string]interface{}, instances []map[string]interface{}) {
	stacksByAppID := map[string]interface{}{}
	stackIDsByAppID := map[string]string{}
	for _, instance := range instances {
		appID := firstRelationID(instance, relationColumns["app"])
		if appID == "" {
			continue
		}
		if stack := firstStackObject(instance); stack != nil {
			stacksByAppID[appID] = stack
			continue
		}
		if stackID := firstRelationID(instance, relationColumns["stack"]); stackID != "" {
			stackIDsByAppID[appID] = stackID
		}
	}

	for _, app := range apps {
		if hasStackReference(app) {
			continue
		}
		appID := firstScalarPath(app, "id", "appId", "app.id")
		if appID == "" {
			continue
		}
		if stack := stacksByAppID[appID]; stack != nil {
			app["stack"] = stack
			continue
		}
		if stackID := stackIDsByAppID[appID]; stackID != "" {
			app["stackId"] = stackID
		}
	}
}

func enrichAppRowsWithInstanceCounts(apps []map[string]interface{}, instances []map[string]interface{}) {
	countsByAppID := map[string]int{}
	for _, instance := range instances {
		if appID := firstRelationID(instance, relationColumns["app"]); appID != "" {
			countsByAppID[appID]++
		}
	}

	for _, app := range apps {
		appID := firstScalarPath(app, "id", "appId", "app.id")
		if appID == "" {
			continue
		}
		app["instances"] = countsByAppID[appID]
	}
}

func hasStackReference(row map[string]interface{}) bool {
	if formatColumnValue(row, "stack") != "" {
		return true
	}
	return firstRelationID(row, relationColumns["stack"]) != ""
}

func firstStackObject(row map[string]interface{}) map[string]interface{} {
	for _, path := range []string{
		"stack",
		"stackRev.stack",
		"stackRevision.stack",
		"app.stack",
		"app.stackRev.stack",
		"app.stackRevision.stack",
		"appInstance.stack",
		"appInstance.app.stack",
		"appInstance.app.stackRev.stack",
		"appInstance.app.stackRevision.stack",
		"instance.stack",
		"instance.app.stack",
		"instance.app.stackRev.stack",
		"instance.app.stackRevision.stack",
	} {
		if stack, ok := valueAtPath(row, path).(map[string]interface{}); ok {
			return stack
		}
	}
	return nil
}

func optionalInt(value string) interface{} {
	if value == "" {
		return nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return value
	}
	return number
}
