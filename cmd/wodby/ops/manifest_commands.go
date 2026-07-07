package ops

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var manifestValidationColumns = []string{"valid", "resource", "error"}

type manifestFileOptions struct {
	manifest  string
	orgID     string
	projectID string
	version   string
	includes  []string
}

func newManifestValidateCommand(kind string, out outputOptions) *cobra.Command {
	opts := manifestFileOptions{}
	path := "/" + kind + "s/actions/validate-manifest"
	cmd := &cobra.Command{
		Use:   "validate-manifest",
		Short: "Validate " + kind + " manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := manifestRequestBody(cmd, opts)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), path, nil, body, &result); err != nil {
				return err
			}
			if err := printClientResult(cmd, client, out, result, manifestValidationColumns); err != nil {
				return err
			}
			if manifestValidationFailed(result) {
				return errors.New(kind + " manifest is invalid")
			}
			return nil
		},
	}
	addManifestFileFlags(cmd, &opts)
	return cmd
}

func newManifestCreateCommand(kind string, out outputOptions, columns []string) *cobra.Command {
	opts := manifestFileOptions{}
	path := "/" + kind + "s/actions/create-from-manifest"
	cmd := &cobra.Command{
		Use:   "create-from-manifest",
		Short: "Create " + kind + " from manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := manifestRequestBody(cmd, opts)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), path, nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addManifestFileFlags(cmd, &opts)
	return cmd
}

func addManifestFileFlags(cmd *cobra.Command, opts *manifestFileOptions) {
	cmd.Flags().StringVarP(&opts.manifest, "manifest", "f", "", "Path to Wodby manifest YAML; use - for stdin")
	cmd.Flags().StringVar(&opts.orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&opts.projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&opts.version, "version", "", "Revision version label")
	cmd.Flags().StringArrayVar(&opts.includes, "include", nil, "Referenced file to include; use PATH or MANIFEST_PATH=LOCAL_PATH")
}

func manifestRequestBody(cmd *cobra.Command, opts manifestFileOptions) (map[string]interface{}, error) {
	if err := requireFlag(opts.manifest, "--manifest"); err != nil {
		return nil, err
	}
	manifestYAML, err := readTextFileOrStdin(cmd, opts.manifest)
	if err != nil {
		return nil, errors.Wrap(err, "read manifest")
	}
	files, err := readIncludedFiles(opts.includes)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{
		"manifestYaml": manifestYAML,
	}
	if err := addOptionalInt(body, "orgId", opts.orgID, "--org"); err != nil {
		return nil, err
	}
	if err := addOptionalInt(body, "projectId", opts.projectID, "--project"); err != nil {
		return nil, err
	}
	addOptionalString(body, "version", opts.version)
	if len(files) > 0 {
		body["files"] = files
	}
	return body, nil
}

func readIncludedFiles(includes []string) (map[string]string, error) {
	files := map[string]string{}
	for _, include := range includes {
		key, path := splitInclude(include)
		if strings.TrimSpace(key) == "" || strings.TrimSpace(path) == "" {
			return nil, errors.Errorf("invalid --include %q", include)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		files[filepath.ToSlash(key)] = string(content)
	}
	return files, nil
}

func splitInclude(value string) (string, string) {
	if before, after, ok := strings.Cut(value, "="); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return value, value
}

func readTextFileOrStdin(cmd *cobra.Command, path string) (string, error) {
	if path == "-" {
		content, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", errors.WithStack(err)
		}
		return string(content), nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errors.WithStack(err)
	}
	return string(content), nil
}

func manifestValidationFailed(result interface{}) bool {
	row, ok := normalizeItem(result).(map[string]interface{})
	if !ok {
		return false
	}
	valid, ok := row["valid"].(bool)
	return ok && !valid
}

func writeTextOutput(path string, content string) error {
	if path == "" {
		return nil
	}
	return errors.WithStack(os.WriteFile(path, []byte(content), 0o644))
}

func requiredStringField(value interface{}, field string) (string, error) {
	row, ok := normalizeItem(value).(map[string]interface{})
	if !ok {
		return "", errors.Errorf("response missing %q", field)
	}
	raw, ok := row[field]
	if !ok {
		return "", errors.Errorf("response missing %q", field)
	}
	text, ok := raw.(string)
	if !ok {
		return "", errors.Errorf("response field %q is not a string", field)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.Errorf("response field %q is empty", field)
	}
	return text, nil
}
