package wodby1

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	versionActionPreserve = "preserve"
	versionActionUpgrade  = "upgrade"
	versionActionDefault  = "target_default"
	versionActionOverride = "override"
)

type targetStackManifest struct {
	Services []targetStackManifestService `json:"services"`
}

type targetStackManifestService struct {
	Name    string                             `json:"name"`
	Options []targetStackManifestServiceOption `json:"options,omitempty"`
}

type targetStackManifestServiceOption struct {
	Version  string `json:"version"`
	Default  bool   `json:"default,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type targetVersionOption struct {
	Version  string
	Default  bool
	Disabled bool
	EOL      time.Time
}

func targetStackOptionOverrides(raw, serviceName string) (map[string]targetStackManifestServiceOption, error) {
	result := map[string]targetStackManifestServiceOption{}
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}
	var manifest targetStackManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("decode target stack revision manifest: %w", err)
	}
	for _, service := range manifest.Services {
		if service.Name != serviceName {
			continue
		}
		for _, option := range service.Options {
			version := strings.TrimSpace(option.Version)
			if version == "" {
				return nil, fmt.Errorf("target stack service %q has a version option without a version", serviceName)
			}
			if _, exists := result[version]; exists {
				return nil, fmt.Errorf("target stack service %q repeats version option %q", serviceName, version)
			}
			result[version] = option
		}
		return result, nil
	}
	return result, nil
}

func effectiveTargetVersionOptions(
	inspection TargetStackServiceInspection,
	stackManifest string,
) ([]targetVersionOption, error) {
	manifest := inspection.ServiceRevision.Manifest
	if manifest == nil || len(manifest.Options) == 0 {
		return nil, nil
	}
	overrides, err := targetStackOptionOverrides(stackManifest, inspection.StackService.Name)
	if err != nil {
		return nil, err
	}
	items := make([]targetVersionOption, 0, len(manifest.Options))
	seen := map[string]bool{}
	stackDefaultSelected := false
	for _, override := range overrides {
		if override.Default {
			stackDefaultSelected = true
			break
		}
	}
	for _, option := range manifest.Options {
		version := strings.TrimSpace(option.Version)
		if version == "" {
			return nil, fmt.Errorf("target service %q has a version option without a version", inspection.StackService.Name)
		}
		if seen[version] {
			return nil, fmt.Errorf("target service %q repeats version option %q", inspection.StackService.Name, version)
		}
		seen[version] = true
		item := targetVersionOption{Version: version, Default: option.Default && !stackDefaultSelected, EOL: option.EOL}
		if override, exists := overrides[version]; exists {
			item.Default = override.Default
			item.Disabled = override.Disabled
		}
		items = append(items, item)
	}
	return items, nil
}

func resolveServiceVersion(
	servicePlan *ServicePlan,
	inspection TargetStackServiceInspection,
	stackManifest string,
	now time.Time,
	pinned bool,
) (ReviewItem, bool, error) {
	options, err := effectiveTargetVersionOptions(inspection, stackManifest)
	if err != nil {
		return ReviewItem{}, false, err
	}
	if len(options) == 0 {
		if servicePlan.VersionExplicit {
			return ReviewItem{
				Severity: SeverityBlocking,
				Subject:  "service " + servicePlan.SourceName + " version",
				Message:  fmt.Sprintf("target service %q does not expose selectable versions; remove its --target-version-map override", servicePlan.TargetName),
			}, true, nil
		}
		servicePlan.TargetVersion = ""
		servicePlan.VersionAction = ""
		return ReviewItem{}, false, nil
	}

	byVersion := make(map[string]targetVersionOption, len(options))
	for _, option := range options {
		byVersion[option.Version] = option
	}
	selected := targetVersionOption{}
	if servicePlan.VersionExplicit || (pinned && strings.TrimSpace(servicePlan.TargetVersion) != "") {
		selected = byVersion[strings.TrimSpace(servicePlan.TargetVersion)]
		if selected.Version == "" || selected.Disabled {
			return ReviewItem{
				Severity: SeverityBlocking,
				Subject:  "service " + servicePlan.SourceName + " version",
				Message: fmt.Sprintf(
					"requested target version %q is not available for service %q; available versions: %s",
					servicePlan.TargetVersion,
					servicePlan.TargetName,
					availableVersionList(options),
				),
			}, true, nil
		}
		servicePlan.TargetVersion = selected.Version
		if !pinned || servicePlan.VersionAction == "" {
			servicePlan.VersionAction = versionActionOverride
		}
	} else if source := strings.TrimSpace(servicePlan.SourceVersion); source != "" {
		candidate := byVersion[source]
		if candidate.Version != "" && !candidate.Disabled && !versionOptionEOL(candidate, now) {
			selected = candidate
			servicePlan.TargetVersion = selected.Version
			servicePlan.VersionAction = versionActionPreserve
			return ReviewItem{}, false, nil
		}
		selected, _ = highestNonEOLVersion(options, now)
		if selected.Version == "" {
			return ReviewItem{
				Severity: SeverityBlocking,
				Subject:  "service " + servicePlan.SourceName + " version",
				Message: fmt.Sprintf(
					"source version %q is unavailable or EOL and target service %q has no enabled non-EOL version; use --target-version-map after reviewing supported versions",
					source,
					servicePlan.TargetName,
				),
			}, true, nil
		}
		servicePlan.TargetVersion = selected.Version
		servicePlan.VersionAction = versionActionUpgrade
		return ReviewItem{
			Severity: SeverityConfirmation,
			Subject:  "service " + servicePlan.SourceName + " version",
			Message: fmt.Sprintf(
				"IMPORTANT: source version %q is unavailable or EOL; migration selected highest supported non-EOL target version %q. Review application compatibility or override with --target-version-map",
				source,
				selected.Version,
			),
		}, true, nil
	} else {
		selected, _ = defaultNonEOLVersion(options, now)
		if selected.Version == "" {
			return ReviewItem{
				Severity: SeverityBlocking,
				Subject:  "service " + servicePlan.SourceName + " version",
				Message:  fmt.Sprintf("source version is unavailable and target service %q has no enabled non-EOL default; use --target-version-map", servicePlan.TargetName),
			}, true, nil
		}
		servicePlan.TargetVersion = selected.Version
		servicePlan.VersionAction = versionActionDefault
		return ReviewItem{
			Severity: SeverityConfirmation,
			Subject:  "service " + servicePlan.SourceName + " version",
			Message: fmt.Sprintf(
				"IMPORTANT: Wodby 1 did not report a source version; target version %q was selected. Review it or override with --target-version-map",
				selected.Version,
			),
		}, true, nil
	}

	if versionOptionEOL(selected, now) {
		return ReviewItem{
			Severity: SeverityConfirmation,
			Subject:  "service " + servicePlan.SourceName + " version",
			Message: fmt.Sprintf(
				"IMPORTANT: explicitly selected target version %q is EOL; use a supported version unless this is a temporary compatibility requirement",
				selected.Version,
			),
		}, true, nil
	}
	return ReviewItem{}, false, nil
}

func defaultNonEOLVersion(options []targetVersionOption, now time.Time) (targetVersionOption, bool) {
	for _, option := range options {
		if option.Default && !option.Disabled && !versionOptionEOL(option, now) {
			return option, true
		}
	}
	return highestNonEOLVersion(options, now)
}

func highestNonEOLVersion(options []targetVersionOption, now time.Time) (targetVersionOption, bool) {
	items := append([]targetVersionOption(nil), options...)
	sort.SliceStable(items, func(i, j int) bool {
		return compareVersionParts(items[i].Version, items[j].Version) > 0
	})
	for _, option := range items {
		if !option.Disabled && !versionOptionEOL(option, now) {
			return option, true
		}
	}
	return targetVersionOption{}, false
}

func versionOptionEOL(option targetVersionOption, now time.Time) bool {
	return !option.EOL.IsZero() && !option.EOL.After(now)
}

func availableVersionList(options []targetVersionOption) string {
	items := make([]string, 0, len(options))
	for _, option := range options {
		if !option.Disabled {
			items = append(items, option.Version)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return compareVersionParts(items[i], items[j]) < 0 })
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func compareVersionParts(left, right string) int {
	lparts := strings.FieldsFunc(left, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	rparts := strings.FieldsFunc(right, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	count := len(lparts)
	if len(rparts) > count {
		count = len(rparts)
	}
	for index := 0; index < count; index++ {
		lpart, rpart := "", ""
		if index < len(lparts) {
			lpart = lparts[index]
		}
		if index < len(rparts) {
			rpart = rparts[index]
		}
		li, lerr := strconv.Atoi(lpart)
		ri, rerr := strconv.Atoi(rpart)
		if lerr == nil && rerr == nil {
			if li < ri {
				return -1
			}
			if li > ri {
				return 1
			}
			continue
		}
		if lpart < rpart {
			return -1
		}
		if lpart > rpart {
			return 1
		}
	}
	return 0
}
