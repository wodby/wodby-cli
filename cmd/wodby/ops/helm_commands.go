package ops

import (
	"encoding/json"
	"fmt"
	"net/url"
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
		Use:   "inspect [CHART]",
		Short: "Inspect Helm chart",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := helmChartRequestBody(cmd, args, opts)
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
			if outputFormat(cmd, out) != outputJSON {
				return printHelmChartInspection(cmd, result)
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
		Use:   "scaffold-service [CHART]",
		Short: "Scaffold Wodby service manifest from Helm chart",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chart, err := helmChartRequestBody(cmd, args, opts.chart)
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
			printHelmScaffoldWarnings(cmd, result)
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
		Use:   "scaffold-stack [CHART]",
		Short: "Scaffold Wodby service and stack manifests from Helm chart",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chart, err := helmChartRequestBody(cmd, args, opts.chart)
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
			printHelmScaffoldWarnings(cmd, result)
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

func helmChartRequestBody(cmd *cobra.Command, args []string, opts helmChartOptions) (map[string]interface{}, error) {
	chart, source, err := resolveHelmChartReference(args, opts)
	if err != nil {
		return nil, err
	}
	if opts.values != "" && opts.valuesYAML != "" {
		return nil, errors.New("use either --values or --values-yaml, not both")
	}
	body := map[string]interface{}{
		"chart": chart,
	}
	addOptionalString(body, "sourceName", opts.sourceName)
	addOptionalString(body, "source", source)
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

func resolveHelmChartReference(args []string, opts helmChartOptions) (string, string, error) {
	positional := ""
	if len(args) > 0 {
		positional = strings.TrimSpace(args[0])
	}
	explicitChart := strings.TrimSpace(opts.chart)
	explicitSource := strings.TrimSpace(opts.source)
	if positional != "" && explicitChart != "" {
		return "", "", errors.New("use either CHART or --chart, not both")
	}

	ref := explicitChart
	if positional != "" {
		ref = positional
	}
	if ref == "" {
		if strings.HasPrefix(explicitSource, "oci://") {
			return explicitSource, "", nil
		}
		return "", "", errors.New("CHART or --chart is required")
	}
	if explicitSource != "" && helmChartRefIncludesSource(ref) {
		return "", "", errors.New("do not combine --source with a URL or OCI chart reference")
	}

	chart, inferredSource, err := splitHelmChartReference(ref)
	if err != nil {
		return "", "", err
	}
	if explicitSource != "" {
		return chart, explicitSource, nil
	}
	return chart, inferredSource, nil
}

func helmChartRefIncludesSource(ref string) bool {
	return strings.HasPrefix(ref, "oci://") || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

func splitHelmChartReference(ref string) (string, string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "oci://") {
		return ref, "", nil
	}
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return ref, "", nil
	}

	parsed, err := url.Parse(ref)
	if err != nil {
		return "", "", errors.WithStack(err)
	}
	if strings.HasSuffix(parsed.Path, ".tgz") {
		return ref, "", nil
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 || segments[len(segments)-1] == "" {
		return ref, "", nil
	}

	chart := segments[len(segments)-1]
	parsed.Path = "/" + strings.Join(segments[:len(segments)-1], "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	source := strings.TrimRight(parsed.String(), "/")
	return chart, source, nil
}

func printHelmChartInspection(cmd *cobra.Command, value interface{}) error {
	rows := responseRows(value)
	if len(rows) == 0 {
		return errors.New("response missing Helm chart analysis")
	}
	analysis := rows[0]
	out := cmd.OutOrStdout()
	chart := asRows(firstNonNilPath(analysis, "chart"))
	chartRow := map[string]interface{}{}
	if len(chart) > 0 {
		chartRow = chart[0]
	}

	chartName := firstScalarPath(chartRow, "name", "chart")
	chartVersion := firstScalarPath(chartRow, "version")
	appVersion := firstScalarPath(chartRow, "appVersion")
	chartRef := firstScalarPath(chartRow, "chart")
	source := firstScalarPath(chartRow, "source")
	if chartName == "" {
		chartName = chartRef
	}
	if chartName == "" {
		chartName = "Helm chart"
	}
	fmt.Fprintf(out, "%s\n", chartName)
	if chartVersion != "" || appVersion != "" {
		fmt.Fprintf(out, "  Version: %s\n", helmJoinNonEmpty(" / app ", chartVersion, appVersion))
	}
	if chartRef != "" {
		fmt.Fprintf(out, "  Chart: %s\n", chartRef)
	}
	if source != "" {
		fmt.Fprintf(out, "  Source: %s\n", source)
	}
	fmt.Fprintf(out, "  Release: %s\n", firstScalarPath(analysis, "release"))
	fmt.Fprintf(out, "  Namespace: %s\n", firstScalarPath(analysis, "namespace"))
	if resourceCount := firstScalarPath(analysis, "resourceCount"); resourceCount != "" {
		fmt.Fprintf(out, "  Rendered resources: %s\n", resourceCount)
	}

	printHelmWorkloads(out, asRows(firstNonNilPath(analysis, "workloads")))
	printHelmServices(out, asRows(firstNonNilPath(analysis, "services")))
	printHelmVolumeClaims(out, asRows(firstNonNilPath(analysis, "volumeClaims")))
	printHelmResourceList(out, "CRDs", asRows(firstNonNilPath(analysis, "crds")))
	printHelmResourceList(out, "Cluster resources", asRows(firstNonNilPath(analysis, "clusterResources")))
	printHelmResourceList(out, "Hooks", asRows(firstNonNilPath(analysis, "hooks")))
	printHelmStringList(out, "Unsupported kinds", helmStringList(firstNonNilPath(analysis, "unsupportedKinds")))
	printHelmStringList(out, "Warnings", helmStringList(firstNonNilPath(analysis, "warnings")))

	return nil
}

func printHelmWorkloads(out interface{ Write([]byte) (int, error) }, workloads []map[string]interface{}) {
	if len(workloads) == 0 {
		fmt.Fprintln(out, "\nWorkloads: none")
		return
	}
	fmt.Fprintf(out, "\nWorkloads: %d\n", len(workloads))
	for _, workload := range workloads {
		kind := firstScalarPath(workload, "kind")
		name := firstScalarPath(workload, "name")
		fmt.Fprintf(out, "  - %s %s\n", helmLower(kind), name)
		printHelmContainers(out, "containers", asRows(firstNonNilPath(workload, "containers")))
		printHelmContainers(out, "init containers", asRows(firstNonNilPath(workload, "initContainers")))
		if volumes := asRows(firstNonNilPath(workload, "volumes")); len(volumes) > 0 {
			fmt.Fprintf(out, "    volumes: %s\n", strings.Join(helmVolumeClaimLabels(volumes), ", "))
		}
	}
}

func printHelmContainers(out interface{ Write([]byte) (int, error) }, label string, containers []map[string]interface{}) {
	if len(containers) == 0 {
		return
	}
	for _, container := range containers {
		name := firstScalarPath(container, "name")
		image := firstScalarPath(container, "image")
		details := make([]string, 0, 3)
		if image != "" {
			details = append(details, image)
		}
		if ports := helmPortLabels(asRows(firstNonNilPath(container, "ports"))); len(ports) > 0 {
			details = append(details, "ports "+strings.Join(ports, ", "))
		}
		if envCount := helmListLen(firstNonNilPath(container, "env")); envCount > 0 {
			details = append(details, fmt.Sprintf("%d env vars", envCount))
		}
		fmt.Fprintf(out, "    %s: %s", label, name)
		if len(details) > 0 {
			fmt.Fprintf(out, " (%s)", strings.Join(details, "; "))
		}
		fmt.Fprintln(out)
	}
}

func printHelmServices(out interface{ Write([]byte) (int, error) }, services []map[string]interface{}) {
	if len(services) == 0 {
		fmt.Fprintln(out, "\nServices: none")
		return
	}
	fmt.Fprintf(out, "\nServices: %d\n", len(services))
	for _, service := range services {
		name := firstScalarPath(service, "name")
		details := make([]string, 0, 2)
		if helmBool(firstNonNilPath(service, "headless")) {
			details = append(details, "headless")
		}
		if ports := helmPortLabels(asRows(firstNonNilPath(service, "ports"))); len(ports) > 0 {
			details = append(details, "ports "+strings.Join(ports, ", "))
		}
		fmt.Fprintf(out, "  - %s", name)
		if len(details) > 0 {
			fmt.Fprintf(out, " (%s)", strings.Join(details, "; "))
		}
		fmt.Fprintln(out)
	}
}

func printHelmVolumeClaims(out interface{ Write([]byte) (int, error) }, volumes []map[string]interface{}) {
	if len(volumes) == 0 {
		return
	}
	fmt.Fprintf(out, "\nStorage: %d volume claims\n", len(volumes))
	for _, label := range helmVolumeClaimLabels(volumes) {
		fmt.Fprintf(out, "  - %s\n", label)
	}
}

func printHelmResourceList(out interface{ Write([]byte) (int, error) }, title string, resources []map[string]interface{}) {
	if len(resources) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s: %d\n", title, len(resources))
	for _, resource := range resources {
		kind := firstScalarPath(resource, "kind")
		name := firstScalarPath(resource, "name")
		apiVersion := firstScalarPath(resource, "apiVersion")
		if apiVersion != "" {
			fmt.Fprintf(out, "  - %s %s (%s)\n", kind, name, apiVersion)
			continue
		}
		fmt.Fprintf(out, "  - %s %s\n", kind, name)
	}
}

func printHelmStringList(out interface{ Write([]byte) (int, error) }, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(out, "  - %s\n", value)
	}
}

func helmPortLabels(ports []map[string]interface{}) []string {
	labels := make([]string, 0, len(ports))
	for _, port := range ports {
		number := firstScalarPath(port, "number")
		if number == "" {
			number = firstScalarPath(port, "targetPort")
		}
		if number == "" {
			continue
		}
		protocol := helmLower(firstScalarPath(port, "protocol"))
		name := firstScalarPath(port, "name")
		label := number
		if protocol != "" {
			label += "/" + protocol
		}
		if name != "" {
			label = name + ":" + label
		}
		labels = append(labels, label)
	}
	return labels
}

func helmVolumeClaimLabels(volumes []map[string]interface{}) []string {
	labels := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		name := firstScalarPath(volume, "name")
		details := make([]string, 0, 3)
		if size := firstScalarPath(volume, "size"); size != "" {
			details = append(details, size)
		}
		if class := firstScalarPath(volume, "storageClassName"); class != "" {
			details = append(details, "class "+class)
		}
		if modes := helmStringList(firstNonNilPath(volume, "accessModes")); len(modes) > 0 {
			details = append(details, strings.Join(modes, ","))
		}
		if len(details) > 0 {
			name += " (" + strings.Join(details, "; ") + ")"
		}
		labels = append(labels, name)
	}
	return labels
}

func helmStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if scalar := scalarString(item); scalar != "" {
				result = append(result, scalar)
			}
		}
		return result
	default:
		return nil
	}
}

func helmListLen(value interface{}) int {
	switch typed := value.(type) {
	case []string:
		return len(typed)
	case []interface{}:
		return len(typed)
	case []map[string]interface{}:
		return len(typed)
	default:
		return 0
	}
}

func helmBool(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func helmLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func helmJoinNonEmpty(separator string, values ...string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, strings.TrimSpace(value))
		}
	}
	return strings.Join(filtered, separator)
}

func printHelmScaffoldWarnings(cmd *cobra.Command, value interface{}) {
	warnings := helmScaffoldWarnings(value)
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Warnings:")
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", warning)
	}
}

func helmScaffoldWarnings(value interface{}) []string {
	row, ok := normalizeItem(value).(map[string]interface{})
	if !ok {
		return nil
	}
	warnings := make([]string, 0)
	seen := make(map[string]bool)
	add := func(values []string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			warnings = append(warnings, value)
		}
	}
	add(helmStringList(firstNonNilPath(row, "warnings")))
	if analysis := asRows(firstNonNilPath(row, "analysis")); len(analysis) > 0 {
		add(helmStringList(firstNonNilPath(analysis[0], "warnings")))
	}
	return warnings
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
