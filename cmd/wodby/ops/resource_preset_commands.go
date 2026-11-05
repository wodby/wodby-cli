package ops

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var resourcePresetColumns = []string{"id", "name", "title", "system", "requestCPU", "requestMem", "limitCPU", "limitMem"}

// resourcePresetValueSpecs are a custom preset's CPU (millicores) and memory (MiB) values.
func resourcePresetValueSpecs() []jsonFlagSpec {
	return []jsonFlagSpec{
		intJSONFlag("request-cpu", "requestCPU", "Requested CPU in millicores, a multiple of 10", false),
		intJSONFlag("request-mem", "requestMem", "Requested memory in MiB, a multiple of 16", false),
		intJSONFlag("limit-cpu", "limitCPU", "CPU limit in millicores, a multiple of 10", false),
		intJSONFlag("limit-mem", "limitMem", "Memory limit in MiB, a multiple of 16", false),
	}
}

// newResourcePresetCommand lists Wodby's resource presets and manages an organization's custom ones. A preset is
// a named set of container CPU and memory values that resources commands accept with --preset.
func newResourcePresetCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "resource-preset", Aliases: []string{"resource-presets"}, Short: "Manage resource presets"}
	addOutputFlag(cmd, &out)
	list := newOrgListCommand("list", "List Wodby's and the organization's resource presets", "/resource-presets", resourcePresetColumns, out)
	defaultToList(cmd, list)
	cmd.AddCommand(
		list,
		newResourcePresetWriteCommand("create", "Create custom resource preset", http.MethodPost, out),
		newResourcePresetWriteCommand("update ID", "Update custom resource preset", http.MethodPut, out),
		newDeleteCommand("delete ID", "Delete custom resource preset", "/resource-presets/", resourcePresetColumns, out),
	)
	return cmd
}

// newResourcePresetWriteCommand creates a custom preset, or updates one. The API replaces every field, so an update
// keeps the current name, title and values for any of them not given as flags.
func newResourcePresetWriteCommand(use string, short string, method string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title string
	args := cobra.NoArgs
	if method == http.MethodPut {
		args = cobra.ExactArgs(1)
	}
	specs := resourcePresetValueSpecs()
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
				values, err := bodyFromJSONFlagSpecs(cmd, specs, false)
				if err != nil {
					return err
				}
				if cmd.Flags().Changed("name") {
					values["name"] = name
				}
				if cmd.Flags().Changed("title") {
					values["title"] = title
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if method == http.MethodPut {
					if err := fillResourcePresetUpdateDefaults(cmd, client, resolvedOrgID, args[0], values); err != nil {
						return err
					}
				} else if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
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
				err = client.Post(cmd.Context(), "/resource-presets", nil, requestBody, &result)
			} else {
				err = client.Put(cmd.Context(), escapedPath("/resource-presets/%s", args[0]), nil, requestBody, &result)
			}
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, resourcePresetColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&name, "name", "", "Preset name: lowercase letters, digits and dashes, not the name of a Wodby preset")
	cmd.Flags().StringVar(&title, "title", "", "Preset title")
	return cmd
}

// fillResourcePresetUpdateDefaults copies the preset's current name, title and values into values for flags left
// out. The preset is found in the organization's list, which has no single-preset route.
func fillResourcePresetUpdateDefaults(cmd *cobra.Command, client *rest.Client, orgID string, id string, values map[string]interface{}) error {
	var presets []map[string]interface{}
	if err := client.Get(cmd.Context(), "/resource-presets", url.Values{"orgId": []string{orgID}}, &presets); err != nil {
		return err
	}
	var current map[string]interface{}
	for _, preset := range presets {
		if formatValue(preset["id"]) == id {
			current = preset
		}
	}
	if current == nil {
		if _, err := strconv.Atoi(id); err != nil {
			return errors.Errorf("invalid resource preset ID %q", id)
		}
		return errors.Errorf("resource preset %s is not a custom preset of organization %s", id, orgID)
	}
	for _, key := range []string{"name", "title", "requestCPU", "requestMem", "limitCPU", "limitMem"} {
		if _, given := values[key]; !given && current[key] != nil {
			values[key] = current[key]
		}
	}
	return nil
}
