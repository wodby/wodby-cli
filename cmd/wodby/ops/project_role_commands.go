package ops

import (
	"net/http"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	projectRoleColumns       = []string{"id", "role", "name", "title", "system", "assignableToTeams", "membershipCount"}
	projectRoleGetColumns    = []string{"id", "role", "name", "title", "description", "system", "projectPermissions", "envPermissions", "assignableToTeams", "membershipCount", "createdAt", "updatedAt"}
	projectPermissionColumns = []string{"name", "title", "scope", "alwaysGranted", "description"}
)

// newProjectRoleCommand lists the system project roles and manages an organization's custom ones. It is meant for the
// project command, whose --output flag it uses.
func newProjectRoleCommand() *cobra.Command {
	out := outputOptions{output: outputTable}
	cmd := &cobra.Command{Use: "role", Aliases: []string{"roles"}, Short: "Manage project roles"}
	list := newOrgListCommand("list", "List the system project roles and the organization's custom ones", "/project-roles", projectRoleColumns, out)
	defaultToList(cmd, list)
	cmd.AddCommand(
		list,
		newGetCommand("get ID", "Get custom project role", "/project-roles/", projectRoleGetColumns, out),
		newProjectRoleWriteCommand("create", "Create custom project role", http.MethodPost, out),
		newProjectRoleWriteCommand("update ID", "Update custom project role; every membership holding it changes at once", http.MethodPut, out),
		newDeleteCommand("delete ID", "Delete custom project role", "/project-roles/", projectRoleColumns, out),
	)
	return cmd
}

// newProjectPermissionsCommand lists every permission a project role can grant. It is meant for the project command,
// whose --output flag it uses.
func newProjectPermissionsCommand() *cobra.Command {
	out := outputOptions{output: outputTable}
	return &cobra.Command{
		Use:   "permissions",
		Short: "List the permissions a project role can grant",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, "/project-permissions", nil, projectPermissionColumns)
		},
	}
}

// newProjectRoleWriteCommand creates a custom project role, or updates one. The API replaces the whole role, so an
// update keeps the current name, title, description and permissions for any of them not given as flags.
func newProjectRoleWriteCommand(use string, short string, method string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title, description string
	var projectPermissions, envPermissions []string
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
				values := map[string]interface{}{}
				if cmd.Flags().Changed("name") {
					values["name"] = name
				}
				if cmd.Flags().Changed("title") {
					values["title"] = title
				}
				if cmd.Flags().Changed("description") {
					values["description"] = description
				}
				if cmd.Flags().Changed("project-permission") {
					values["projectPermissions"] = projectPermissionValues(projectPermissions)
				}
				if cmd.Flags().Changed("env-permission") {
					parsed, err := projectRoleEnvPermissions(envPermissions)
					if err != nil {
						return err
					}
					values["envPermissions"] = parsed
				}
				if method == http.MethodPut {
					if err := fillProjectRoleUpdateDefaults(cmd, client, args[0], values); err != nil {
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
				// The API requires both permission fields; a role without extra permissions sends them empty.
				if _, ok := values["projectPermissions"]; !ok {
					values["projectPermissions"] = []string{}
				}
				if _, ok := values["envPermissions"]; !ok {
					values["envPermissions"] = map[string][]string{}
				}
				requestBody = values
			}
			var result interface{}
			if method == http.MethodPost {
				err = client.Post(cmd.Context(), "/project-roles", nil, requestBody, &result)
			} else {
				err = client.Put(cmd.Context(), escapedPath("/project-roles/%s", args[0]), nil, requestBody, &result)
			}
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, projectRoleColumns)
		},
	}
	addBodyFlags(cmd, &body)
	keep := ""
	if method == http.MethodPost {
		cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	} else {
		keep = "; replaces the current list, which is kept when left out"
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name: lowercase letters, digits, dashes or underscores, unique in the organization")
	cmd.Flags().StringVar(&title, "title", "", "Role title")
	cmd.Flags().StringVar(&description, "description", "", "Role description")
	cmd.Flags().StringArrayVar(&projectPermissions, "project-permission", nil,
		"Project permission, such as project.manage; repeatable or comma-separated; see project permissions"+keep)
	cmd.Flags().StringArrayVar(&envPermissions, "env-permission", nil,
		"Environment permissions as ENV_TYPE=PERMISSION[,PERMISSION], for the environment types prod, staging, test, dev and feature; repeatable"+keep)
	return cmd
}

// projectPermissionValues splits repeated or comma-separated permission flags into a list without empty entries.
func projectPermissionValues(raw []string) []string {
	values := []string{}
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				values = append(values, part)
			}
		}
	}
	return values
}

// projectRoleEnvPermissions parses ENV_TYPE=PERMISSION[,PERMISSION] flags into permissions by environment type.
// Repeating an environment type adds to its permissions; ENV_TYPE= alone grants it nothing beyond what every role has.
func projectRoleEnvPermissions(raw []string) (map[string][]string, error) {
	result := map[string][]string{}
	for _, value := range raw {
		envType, permissions, ok := strings.Cut(value, "=")
		envType = strings.TrimSpace(envType)
		if !ok || envType == "" {
			return nil, errors.Errorf("invalid --env-permission %q: use ENV_TYPE=PERMISSION[,PERMISSION]", value)
		}
		if result[envType] == nil {
			result[envType] = []string{}
		}
		result[envType] = append(result[envType], projectPermissionValues([]string{permissions})...)
	}
	return result, nil
}

// fillProjectRoleUpdateDefaults copies the role's current name, title, description and permissions into values for
// flags left out.
func fillProjectRoleUpdateDefaults(cmd *cobra.Command, client *rest.Client, id string, values map[string]interface{}) error {
	keys := map[string]string{
		"name":               "name",
		"title":              "title",
		"description":        "description",
		"projectPermissions": "project-permission",
		"envPermissions":     "env-permission",
	}
	missing := false
	for _, flag := range keys {
		if !cmd.Flags().Changed(flag) {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	var current map[string]interface{}
	if err := client.Get(cmd.Context(), escapedPath("/project-roles/%s", id), nil, &current); err != nil {
		return err
	}
	for key, flag := range keys {
		if !cmd.Flags().Changed(flag) && current[key] != nil {
			values[key] = current[key]
		}
	}
	return nil
}
