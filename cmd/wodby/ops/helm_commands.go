package ops

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var helmChartAnalysisColumns = []string{"chart", "release", "namespace", "resourceCount", "workloads", "services", "warnings"}

type helmChartOptions struct {
	sourceName string
	source     string
	chart      string
	version    string
	release    string
	namespace  string
	values     string
	valuesYAML string
}

type helmServiceScaffoldOptions struct {
	chart        helmChartOptions
	serviceName  string
	serviceTitle string
	serviceType  string
	icon         string
	out          string
}

type helmStackScaffoldOptions struct {
	chart        helmChartOptions
	serviceName  string
	serviceTitle string
	serviceType  string
	stackName    string
	stackTitle   string
	icon         string
	serviceOut   string
	stackOut     string
}

func newHelmCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "helm",
		Short: "Inspect Helm charts and scaffold Wodby manifests",
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(
		newHelmInspectCommand(out),
		newHelmScaffoldServiceCommand(out),
		newHelmScaffoldStackCommand(out),
	)
	return cmd
}

func newHelmInspectCommand(out outputOptions) *cobra.Command {
	opts := helmChartOptions{}
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect Helm chart",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := helmChartRequestBody(cmd, opts)
			if err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/helm-charts/actions/inspect", nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, helmChartAnalysisColumns)
		},
	}
	addHelmChartFlags(cmd, &opts)
	return cmd
}

func newHelmScaffoldServiceCommand(out outputOptions) *cobra.Command {
	opts := helmServiceScaffoldOptions{}
	cmd := &cobra.Command{
		Use:   "scaffold-service",
		Short: "Scaffold Wodby service manifest from Helm chart",
		RunE: func(cmd *cobra.Command, args []string) error {
			chart, err := helmChartRequestBody(cmd, opts.chart)
			if err != nil {
				return err
			}
			body := map[string]interface{}{"chart": chart}
			addOptionalString(body, "serviceName", opts.serviceName)
			addOptionalString(body, "serviceTitle", opts.serviceTitle)
			addOptionalString(body, "serviceType", opts.serviceType)
			addOptionalString(body, "icon", opts.icon)

			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/services/actions/scaffold-from-helm-chart", nil, body, &result); err != nil {
				return err
			}
			manifest, err := requiredStringField(result, "manifestYaml")
			if err != nil {
				return err
			}
			if err := writeTextOutput(opts.out, manifest); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printClientResult(cmd, client, out, result, helmChartAnalysisColumns)
			}
			if opts.out != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote service manifest to %s\n", opts.out)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), manifest)
			return nil
		},
	}
	addHelmChartFlags(cmd, &opts.chart)
	addHelmServiceScaffoldFlags(cmd, &opts)
	return cmd
}

func newHelmScaffoldStackCommand(out outputOptions) *cobra.Command {
	opts := helmStackScaffoldOptions{}
	cmd := &cobra.Command{
		Use:   "scaffold-stack",
		Short: "Scaffold Wodby service and stack manifests from Helm chart",
		RunE: func(cmd *cobra.Command, args []string) error {
			chart, err := helmChartRequestBody(cmd, opts.chart)
			if err != nil {
				return err
			}
			body := map[string]interface{}{"chart": chart}
			addOptionalString(body, "serviceName", opts.serviceName)
			addOptionalString(body, "serviceTitle", opts.serviceTitle)
			addOptionalString(body, "serviceType", opts.serviceType)
			addOptionalString(body, "stackName", opts.stackName)
			addOptionalString(body, "stackTitle", opts.stackTitle)
			addOptionalString(body, "icon", opts.icon)

			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/stacks/actions/scaffold-from-helm-chart", nil, body, &result); err != nil {
				return err
			}
			serviceManifest, err := requiredStringField(result, "serviceManifestYaml")
			if err != nil {
				return err
			}
			stackManifest, err := requiredStringField(result, "stackManifestYaml")
			if err != nil {
				return err
			}
			if err := writeTextOutput(opts.serviceOut, serviceManifest); err != nil {
				return err
			}
			if err := writeTextOutput(opts.stackOut, stackManifest); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printClientResult(cmd, client, out, result, helmChartAnalysisColumns)
			}
			if opts.serviceOut != "" || opts.stackOut != "" {
				if opts.serviceOut != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Wrote service manifest to %s\n", opts.serviceOut)
				}
				if opts.stackOut != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Wrote stack manifest to %s\n", opts.stackOut)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "# service.yml\n%s\n---\n# stack.yml\n%s", strings.TrimRight(serviceManifest, "\n"), stackManifest)
			return nil
		},
	}
	addHelmChartFlags(cmd, &opts.chart)
	addHelmStackScaffoldFlags(cmd, &opts)
	return cmd
}

func addHelmChartFlags(cmd *cobra.Command, opts *helmChartOptions) {
	cmd.Flags().StringVar(&opts.chart, "chart", "", "Helm chart reference")
	cmd.Flags().StringVar(&opts.source, "source", "", "Helm repository or OCI source URL")
	cmd.Flags().StringVar(&opts.sourceName, "source-name", "", "Chart source name to use in generated manifests")
	cmd.Flags().StringVar(&opts.version, "version", "", "Helm chart version")
	cmd.Flags().StringVar(&opts.release, "release", "", "Helm release name used for rendering")
	cmd.Flags().StringVar(&opts.namespace, "namespace", "", "Kubernetes namespace used for rendering")
	cmd.Flags().StringVar(&opts.values, "values", "", "Path to Helm values JSON object")
	cmd.Flags().StringVar(&opts.valuesYAML, "values-yaml", "", "Path to Helm values YAML; use - for stdin")
}

func addHelmServiceScaffoldFlags(cmd *cobra.Command, opts *helmServiceScaffoldOptions) {
	cmd.Flags().StringVar(&opts.serviceName, "service-name", "", "Generated Wodby service name")
	cmd.Flags().StringVar(&opts.serviceTitle, "service-title", "", "Generated Wodby service title")
	cmd.Flags().StringVar(&opts.serviceType, "service-type", "", "Generated Wodby service type")
	cmd.Flags().StringVar(&opts.icon, "icon", "", "Generated service icon")
	cmd.Flags().StringVar(&opts.out, "out", "", "Write generated service manifest YAML to path")
}

func addHelmStackScaffoldFlags(cmd *cobra.Command, opts *helmStackScaffoldOptions) {
	cmd.Flags().StringVar(&opts.serviceName, "service-name", "", "Generated Wodby service name")
	cmd.Flags().StringVar(&opts.serviceTitle, "service-title", "", "Generated Wodby service title")
	cmd.Flags().StringVar(&opts.serviceType, "service-type", "", "Generated Wodby service type")
	cmd.Flags().StringVar(&opts.stackName, "stack-name", "", "Generated Wodby stack name")
	cmd.Flags().StringVar(&opts.stackTitle, "stack-title", "", "Generated Wodby stack title")
	cmd.Flags().StringVar(&opts.icon, "icon", "", "Generated service and stack icon")
	cmd.Flags().StringVar(&opts.serviceOut, "service-out", "", "Write generated service manifest YAML to path")
	cmd.Flags().StringVar(&opts.stackOut, "stack-out", "", "Write generated stack manifest YAML to path")
}

func helmChartRequestBody(cmd *cobra.Command, opts helmChartOptions) (map[string]interface{}, error) {
	if err := requireFlag(opts.chart, "--chart"); err != nil {
		return nil, err
	}
	if opts.values != "" && opts.valuesYAML != "" {
		return nil, errors.New("use either --values or --values-yaml, not both")
	}
	body := map[string]interface{}{
		"chart": opts.chart,
	}
	addOptionalString(body, "sourceName", opts.sourceName)
	addOptionalString(body, "source", opts.source)
	addOptionalString(body, "version", opts.version)
	addOptionalString(body, "release", opts.release)
	addOptionalString(body, "namespace", opts.namespace)
	if opts.values != "" {
		values, err := readJSONMapFileOrStdin(cmd, opts.values)
		if err != nil {
			return nil, errors.Wrap(err, "read values")
		}
		body["values"] = values
	}
	if opts.valuesYAML != "" {
		valuesYAML, err := readTextFileOrStdin(cmd, opts.valuesYAML)
		if err != nil {
			return nil, errors.Wrap(err, "read values yaml")
		}
		body["valuesYaml"] = valuesYAML
	}
	return body, nil
}

func readJSONMapFileOrStdin(cmd *cobra.Command, path string) (map[string]interface{}, error) {
	content, err := readTextFileOrStdin(cmd, path)
	if err != nil {
		return nil, err
	}
	var value map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.WithStack(err)
	}
	if value == nil {
		value = map[string]interface{}{}
	}
	return value, nil
}
