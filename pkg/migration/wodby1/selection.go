package wodby1

import (
	"fmt"
	"sort"
	"strings"
)

// SourceSelection is persisted in the reviewed plan so every source refresh
// uses the same app and instance set. Selectors supplied by the user are
// resolved to immutable Wodby 1 UUIDs before target discovery.
type SourceSelection struct {
	IncludedApps      []SelectedApp      `json:"includedApps"`
	ExcludedApps      []SelectedApp      `json:"excludedApps,omitempty"`
	ExcludedInstances []SelectedInstance `json:"excludedInstances,omitempty"`
}

type SelectedApp struct {
	UUID      string   `json:"uuid"`
	Name      string   `json:"name"`
	Instances []string `json:"instances"`
}

type SelectedInstance struct {
	AppUUID string `json:"appUuid"`
	AppName string `json:"appName"`
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
}

// SelectExport resolves exclusion selectors against a complete Wodby 1
// export, filters it, and recomputes its secret-bound configuration digest.
func SelectExport(export Export, sourceKind string, excludeApps, excludeInstances []string, authKey string) (Export, SourceSelection, error) {
	if err := export.ValidateSource(sourceKind, export.Source.UUID); err != nil {
		return Export{}, SourceSelection{}, err
	}
	if sourceKind == "instance" && (len(excludeApps) != 0 || len(excludeInstances) != 0) {
		return Export{}, SourceSelection{}, fmt.Errorf("instance migrations do not accept app or instance exclusions")
	}
	if sourceKind == "app" && len(excludeApps) != 0 {
		return Export{}, SourceSelection{}, fmt.Errorf("app migrations do not accept --exclude-app")
	}
	if sourceKind != "server" && sourceKind != "app" && sourceKind != "instance" {
		return Export{}, SourceSelection{}, fmt.Errorf("unsupported migration source kind %q", sourceKind)
	}

	apps := export.AppExports()
	excludedAppIDs := map[string]bool{}
	selection := SourceSelection{}
	for _, raw := range uniqueSelectors(excludeApps) {
		app, err := resolveSelectedApp(apps, raw)
		if err != nil {
			return Export{}, SourceSelection{}, fmt.Errorf("resolve --exclude-app %q: %w", raw, err)
		}
		excludedAppIDs[app.App.UUID] = true
		selection.ExcludedApps = append(selection.ExcludedApps, selectedApp(app))
	}

	excludedInstanceIDs := map[string]bool{}
	for _, raw := range uniqueSelectors(excludeInstances) {
		app, instance, err := resolveSelectedInstance(apps, sourceKind, raw)
		if err != nil {
			return Export{}, SourceSelection{}, fmt.Errorf("resolve --exclude-instance %q: %w", raw, err)
		}
		excludedInstanceIDs[instance.UUID] = true
		selection.ExcludedInstances = append(selection.ExcludedInstances, SelectedInstance{
			AppUUID: app.App.UUID,
			AppName: app.App.Name,
			UUID:    instance.UUID,
			Name:    instance.Name,
		})
	}

	selectedApps := make([]AppExport, 0, len(apps))
	for _, app := range apps {
		if excludedAppIDs[app.App.UUID] {
			continue
		}
		selectedInstances := make([]Instance, 0, len(app.Instances))
		for _, instance := range app.Instances {
			if excludedInstanceIDs[instance.UUID] {
				continue
			}
			selectedInstances = append(selectedInstances, instance)
		}
		if len(selectedInstances) == 0 {
			if !containsSelectedApp(selection.ExcludedApps, app.App.UUID) {
				selection.ExcludedApps = append(selection.ExcludedApps, selectedApp(app))
			}
			continue
		}
		app.Instances = selectedInstances
		selectedApps = append(selectedApps, app)
		selection.IncludedApps = append(selection.IncludedApps, selectedApp(app))
	}
	if len(selectedApps) == 0 {
		return Export{}, SourceSelection{}, fmt.Errorf("source selection is empty after applying exclusions")
	}

	canonicalizeSelection(&selection)
	filtered := filterExportApps(export, selectedApps)
	filtered.Issues = filterSelectionIssues(export.Issues, apps, selection)
	if err := recomputeSelectedExportDigests(&filtered, authKey); err != nil {
		return Export{}, SourceSelection{}, err
	}
	return filtered, selection, nil
}

// ApplySourceSelection reapplies a reviewed plan's UUID selection to a fresh
// source export. Missing apps or instances are treated as source drift.
func ApplySourceSelection(export Export, selection SourceSelection, authKey string) (Export, error) {
	apps := export.AppExports()
	appsByID := make(map[string]AppExport, len(apps))
	for _, app := range apps {
		appsByID[app.App.UUID] = app
	}
	selectedApps := make([]AppExport, 0, len(selection.IncludedApps))
	for _, selected := range selection.IncludedApps {
		app, found := appsByID[selected.UUID]
		if !found {
			return Export{}, fmt.Errorf("selected source app %q no longer exists", selected.UUID)
		}
		instancesByID := make(map[string]Instance, len(app.Instances))
		for _, instance := range app.Instances {
			instancesByID[instance.UUID] = instance
		}
		instances := make([]Instance, 0, len(selected.Instances))
		for _, instanceID := range selected.Instances {
			instance, found := instancesByID[instanceID]
			if !found {
				return Export{}, fmt.Errorf("selected source instance %q no longer exists in app %q", instanceID, selected.UUID)
			}
			instances = append(instances, instance)
		}
		app.Instances = instances
		selectedApps = append(selectedApps, app)
	}
	if len(selectedApps) == 0 {
		return Export{}, fmt.Errorf("reviewed source selection is empty")
	}
	filtered := filterExportApps(export, selectedApps)
	filtered.Issues = filterSelectionIssues(export.Issues, apps, selection)
	if err := recomputeSelectedExportDigests(&filtered, authKey); err != nil {
		return Export{}, err
	}
	return filtered, nil
}

func resolveSelectedApp(apps []AppExport, selector string) (AppExport, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return AppExport{}, fmt.Errorf("selector is empty")
	}
	matches := make([]AppExport, 0, 1)
	for _, app := range apps {
		if app.App.UUID == selector {
			return app, nil
		}
		if app.App.Name == selector {
			matches = append(matches, app)
		}
	}
	if len(matches) == 0 {
		return AppExport{}, fmt.Errorf("app was not found by exact UUID or name")
	}
	if len(matches) > 1 {
		return AppExport{}, fmt.Errorf("app name is ambiguous; use its UUID")
	}
	return matches[0], nil
}

func resolveSelectedInstance(apps []AppExport, sourceKind, selector string) (AppExport, Instance, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return AppExport{}, Instance{}, fmt.Errorf("selector is empty")
	}
	for _, app := range apps {
		for _, instance := range app.Instances {
			if instance.UUID == selector {
				return app, instance, nil
			}
		}
	}
	var targetApps []AppExport
	instanceSelector := selector
	if appSelector, scopedInstance, found := strings.Cut(selector, "/"); found {
		app, err := resolveSelectedApp(apps, appSelector)
		if err != nil {
			return AppExport{}, Instance{}, err
		}
		targetApps = []AppExport{app}
		instanceSelector = scopedInstance
	} else {
		if sourceKind == "server" {
			return AppExport{}, Instance{}, fmt.Errorf("server instance names must use APP/INSTANCE; UUIDs can be used directly")
		}
		targetApps = apps
	}
	type match struct {
		app      AppExport
		instance Instance
	}
	matches := make([]match, 0, 1)
	for _, app := range targetApps {
		for _, instance := range app.Instances {
			if instance.UUID == instanceSelector {
				return app, instance, nil
			}
			if instance.Name == instanceSelector {
				matches = append(matches, match{app: app, instance: instance})
			}
		}
	}
	if len(matches) == 0 {
		return AppExport{}, Instance{}, fmt.Errorf("instance was not found by exact UUID or name")
	}
	if len(matches) > 1 {
		return AppExport{}, Instance{}, fmt.Errorf("instance name is ambiguous; use its UUID")
	}
	return matches[0].app, matches[0].instance, nil
}

func selectedApp(app AppExport) SelectedApp {
	result := SelectedApp{UUID: app.App.UUID, Name: app.App.Name, Instances: make([]string, 0, len(app.Instances))}
	for _, instance := range app.Instances {
		result.Instances = append(result.Instances, instance.UUID)
	}
	sort.Strings(result.Instances)
	return result
}

func filterExportApps(export Export, apps []AppExport) Export {
	export.Apps = apps
	if export.Schema == ExportSchemaV1 {
		export.App = &apps[0].App
		export.Instances = apps[0].Instances
		export.Apps = nil
	}
	export.Digest = ""
	export.ResponseDigest = ""
	export.ConfigMAC = ""
	return export
}

func recomputeSelectedExportDigests(export *Export, authKey string) error {
	var err error
	if strings.TrimSpace(authKey) != "" {
		export.ConfigMAC, err = export.AuthenticatedConfigDigest(authKey)
		if err != nil {
			return fmt.Errorf("compute selected source configuration digest: %w", err)
		}
	}
	export.Digest, err = export.ContentDigest()
	if err != nil {
		return fmt.Errorf("compute selected source export digest: %w", err)
	}
	return nil
}

func filterSelectionIssues(issues []ExportIssue, allApps []AppExport, selection SourceSelection) []ExportIssue {
	includedApps := map[string]bool{}
	includedInstances := map[string]bool{}
	for _, app := range selection.IncludedApps {
		includedApps[app.UUID] = true
		for _, instance := range app.Instances {
			includedInstances[instance] = true
		}
	}
	allAppIDs := map[string]bool{}
	allInstanceIDs := map[string]bool{}
	for _, app := range allApps {
		allAppIDs[app.App.UUID] = true
		for _, instance := range app.Instances {
			allInstanceIDs[instance.UUID] = true
		}
	}
	result := make([]ExportIssue, 0, len(issues))
	for _, issue := range issues {
		matchedInstance, includedInstance := false, false
		for id := range allInstanceIDs {
			if selectionPathContains(issue.Path, id) {
				matchedInstance = true
				includedInstance = includedInstance || includedInstances[id]
			}
		}
		if matchedInstance && !includedInstance {
			continue
		}
		if !matchedInstance {
			matchedApp, includedApp := false, false
			for id := range allAppIDs {
				if selectionPathContains(issue.Path, id) {
					matchedApp = true
					includedApp = includedApp || includedApps[id]
				}
			}
			if matchedApp && !includedApp {
				continue
			}
		}
		result = append(result, issue)
	}
	return result
}

func selectionPathContains(path, id string) bool {
	for _, separator := range []string{".", "[", "]", "/", ":"} {
		path = strings.ReplaceAll(path, separator, " ")
	}
	for _, token := range strings.Fields(path) {
		if token == id {
			return true
		}
	}
	return false
}

func uniqueSelectors(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func containsSelectedApp(items []SelectedApp, uuid string) bool {
	for _, item := range items {
		if item.UUID == uuid {
			return true
		}
	}
	return false
}

func canonicalizeSelection(selection *SourceSelection) {
	sort.Slice(selection.IncludedApps, func(i, j int) bool { return selection.IncludedApps[i].UUID < selection.IncludedApps[j].UUID })
	sort.Slice(selection.ExcludedApps, func(i, j int) bool { return selection.ExcludedApps[i].UUID < selection.ExcludedApps[j].UUID })
	sort.Slice(selection.ExcludedInstances, func(i, j int) bool {
		if selection.ExcludedInstances[i].AppUUID != selection.ExcludedInstances[j].AppUUID {
			return selection.ExcludedInstances[i].AppUUID < selection.ExcludedInstances[j].AppUUID
		}
		return selection.ExcludedInstances[i].UUID < selection.ExcludedInstances[j].UUID
	})
}

func sourceSelectionForPlan(export Export, provided *SourceSelection) SourceSelection {
	if provided != nil {
		selection := *provided
		selection.IncludedApps = append([]SelectedApp(nil), provided.IncludedApps...)
		selection.ExcludedApps = append([]SelectedApp(nil), provided.ExcludedApps...)
		selection.ExcludedInstances = append([]SelectedInstance(nil), provided.ExcludedInstances...)
		for i := range selection.IncludedApps {
			selection.IncludedApps[i].Instances = append([]string(nil), selection.IncludedApps[i].Instances...)
		}
		canonicalizeSelection(&selection)
		return selection
	}
	selection := SourceSelection{IncludedApps: make([]SelectedApp, 0, len(export.AppExports()))}
	for _, app := range export.AppExports() {
		selection.IncludedApps = append(selection.IncludedApps, selectedApp(app))
	}
	canonicalizeSelection(&selection)
	return selection
}
