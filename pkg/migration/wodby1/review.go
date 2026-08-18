package wodby1

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func PrintReview(w io.Writer, plan Plan, prepared ...PreparedMigration) {
	review := reviewItemsWithPromotedCommonScope(plan.Review, plan.Apps)
	fmt.Fprintf(w, "%s\n", migrationColor(w, ansiBold+ansiCyan, "Wodby 1 to Wodby 2 migration plan"))
	printReviewTable(w, "  ", []string{"Field", "Value"}, reviewOverviewRows(plan))
	fmt.Fprintf(w, "\nSummary:\n")
	printReviewTable(w, "  ", []string{"Item", "Count"}, [][]string{
		{"Apps", strconv.Itoa(plan.Summary.Apps)},
		{"Instances", strconv.Itoa(plan.Summary.Instances)},
		{"Services", strconv.Itoa(plan.Summary.Services)},
		{"Routes", strconv.Itoa(plan.Summary.Routes)},
		{"Env vars", strconv.Itoa(plan.Summary.EnvVars)},
		{"Cron jobs", strconv.Itoa(plan.Summary.CronJobs)},
		{"Imports", strconv.Itoa(plan.Summary.Imports)},
		{"Migration notes", strconv.Itoa(plan.Summary.Migrations)},
		{"Warnings", strconv.Itoa(plan.Summary.Confirmation + plan.Summary.ServiceWarnings)},
		{"Blocking", strconv.Itoa(plan.Summary.Blocking)},
		{"Manual follow-up", strconv.Itoa(plan.Summary.Manual)},
		{"Intentionally skipped", strconv.Itoa(plan.Summary.Intentionally)},
	})

	for appIndex, app := range plan.Apps {
		targetAppName := app.Name
		if plan.Target.AppID > 0 {
			targetAppName = plan.Target.AppName
		}
		fmt.Fprintf(
			w,
			"\n%s\n",
			migrationColor(w, ansiBold+ansiCyan, fmt.Sprintf("App %d/%d: %s → %s", appIndex+1, len(plan.Apps), firstNonEmpty(app.Title, app.Name), targetAppName)),
		)

		preparedApp, hasPreparedApp := preparedMigrationForReview(prepared, app.SourceUUID)
		settingItems := []ReviewItem{}
		if hasPreparedApp {
			settingItems = preparedStackSettingReviewItems(preparedApp.StackConfiguration)
		}
		stackRows, sharedStack := sharedAppStackRows(app)
		serviceRows := appServiceOverviewRows(app)
		stackEnvRows := preparedStackEnvVarReviewRows(preparedApp.StackConfiguration)
		stackCapacityRows := preparedStackCapacityReviewRows(preparedApp.StackConfiguration)
		sharedCronRows, sharedCrons := sharedAppCronRows(app)
		appMigrationItems := reviewItemsForScope(review, SeverityMigration, app.Name, "")
		if len(settingItems) != 0 {
			appMigrationItems = reviewItemsWithoutSubject(appMigrationItems, "Drupal app settings")
			appMigrationItems = append(appMigrationItems, settingItems...)
		}
		if len(stackEnvRows) != 0 {
			appMigrationItems = reviewItemsWithoutMessageFragment(appMigrationItems, "custom environment variable(s) will be reconciled")
		}
		if len(serviceRows) != 0 {
			appMigrationItems = append(appMigrationItems, ReviewItem{Subject: "Services", Message: appServiceMigrationSummary(app, len(serviceRows))})
		}
		if len(appMigrationItems) != 0 || app.Repository != nil || len(app.Integrations) != 0 || preparedHasCodeStrategy(preparedApp) || sharedStack || sharedCrons || len(stackEnvRows) != 0 || len(stackCapacityRows) != 0 {
			printColoredSectionHeading(w, "  ", "Migrations", ansiGreen)
			printReviewItemDetails(w, "    ", appMigrationItems, ansiGreen)
			if sharedStack {
				fmt.Fprintln(w, "    Target stack (shared by all instances):")
				printReviewTableColor(w, "      ", []string{"Action", "Wodby 1 stack", "Wodby 2 stack", "Revision"}, stackRows, ansiGreen)
			}
			printRepositoryMigration(w, "    ", plan, app.Repository, preparedApp)
			printIntegrationMigrations(w, "    ", app.Integrations)
			if len(stackEnvRows) != 0 {
				fmt.Fprintln(w, "    Stack service environment variables:")
				printReviewTableColor(w, "      ", []string{"Service", "Variable", "Value", "Applies to"}, stackEnvRows, ansiGreen)
			}
			if len(stackCapacityRows) != 0 {
				fmt.Fprintln(w, "    Stack service capacity (shared by all instances):")
				printReviewTableColor(w, "      ", []string{"Service", "Replicas", "Resources", "Target container"}, stackCapacityRows, ansiGreen)
			}
			if sharedCrons {
				fmt.Fprintln(w, "    Cron jobs → cron schedules (shared by all instances):")
				printReviewTableColor(w, "      ", []string{"Source service", "Target service", "Title", "Schedule", "Command", "Target state"}, sharedCronRows, ansiGreen)
			}
		}
		printScopedNonMigrationSections(w, "  ", review, app.Name, "")

		if len(serviceRows) != 0 {
			fmt.Fprintln(w, "  Service mapping overview:")
			printServiceOverview(w, "    ", serviceRows)
		}
		for instanceIndex, instance := range app.Instances {
			fmt.Fprintf(
				w,
				"\n  %s\n",
				migrationColor(w, ansiBold+ansiCyan, fmt.Sprintf(
					"Instance %d/%d: %s → %s (%s → %s)",
					instanceIndex+1,
					len(app.Instances),
					firstNonEmpty(instance.Title, instance.Name),
					instance.Name,
					emptyDash(instance.SourceType),
					emptyDash(instance.TargetEnv),
				)),
			)
			instanceMigrationItems := reviewItemsForScope(review, SeverityMigration, app.Name, instance.Name)
			preparedInstance, _ := preparedInstanceForReview(preparedApp, instance.SourceUUID)
			capacityRows := preparedInstanceCapacityReviewRows(preparedInstance)
			versionRows := preparedInstanceVersionReviewRows(preparedInstance)
			envRows := preparedInstanceEnvVarReviewRows(preparedInstance)
			if len(stackEnvRows) != 0 {
				instanceMigrationItems = reviewItemsWithoutMessageFragment(instanceMigrationItems, "custom environment variable(s) will be reconciled")
			}
			cronRows := reviewCronRows(instance)
			if sharedCrons {
				cronRows = nil
			}
			routeRows := reviewRouteRows(plan, instance)
			importRows := reviewImportRows(instance)
			if len(instanceMigrationItems) != 0 || !sharedStack || instance.BasicAuth.Enabled || len(cronRows) != 0 || len(routeRows) != 0 || len(importRows) != 0 || len(capacityRows) != 0 || len(versionRows) != 0 || len(envRows) != 0 {
				printColoredSectionHeading(w, "    ", "Migrations", ansiGreen)
				printReviewItemDetails(w, "      ", instanceMigrationItems, ansiGreen)
				if !sharedStack {
					fmt.Fprintln(w, "      Target stack:")
					printReviewTableColor(w, "        ", []string{"Action", "Wodby 1 stack", "Wodby 2 stack", "Revision"}, [][]string{reviewStackRow(instance.Stack)}, ansiGreen)
				}
				printBasicAuthMigration(w, "      ", instance)
				if len(cronRows) != 0 {
					fmt.Fprintln(w, "      Cron jobs → cron schedules:")
					printReviewTableColor(w, "        ", []string{"Source service", "Target service", "Title", "Schedule", "Command", "Target state"}, cronRows, ansiGreen)
				}
				if len(capacityRows) != 0 {
					fmt.Fprintln(w, "      App-service capacity overrides:")
					printReviewTableColor(w, "        ", []string{"Source service", "Target service", "Replicas", "Resources", "Target container"}, capacityRows, ansiGreen)
				}
				if len(versionRows) != 0 {
					fmt.Fprintln(w, "      App-service version overrides:")
					printReviewTableColor(w, "        ", []string{"Source service", "Target service", "Version"}, versionRows, ansiGreen)
				}
				if len(envRows) != 0 {
					fmt.Fprintln(w, "      App-service environment variable overrides:")
					printReviewTableColor(w, "        ", []string{"Source service", "Target service", "Variable", "Value"}, envRows, ansiGreen)
				}
				if len(routeRows) != 0 {
					fmt.Fprintln(w, "      Custom domains:")
					printReviewTableColor(w, "        ", []string{"Domain", "Action", "Target", "Target state", "Options"}, routeRows, ansiGreen)
				}
				if len(importRows) != 0 {
					fmt.Fprintln(w, "      Data imports:")
					printReviewTableColor(w, "        ", []string{"Component", "Action", "Target", "Backup", "Backup created", "Size"}, importRows, ansiGreen)
				}
			}
			printScopedNonMigrationSections(w, "    ", review, app.Name, instance.Name)
		}
	}

	if reviewScopeHasItems(review, "", "") {
		fmt.Fprintf(w, "\n%s\n", migrationColor(w, ansiBold+ansiCyan, "Migration-wide"))
		printScopedReviewSections(w, "  ", review, "", "")
	}
}

func preparedInstanceForReview(prepared PreparedMigration, sourceUUID string) (PreparedInstance, bool) {
	for _, instance := range prepared.Instances {
		if instance.Source.UUID == sourceUUID {
			return instance, true
		}
	}
	return PreparedInstance{}, false
}

func preparedStackCapacityReviewRows(configuration PreparedStackConfiguration) [][]string {
	rows := [][]string{}
	for _, serviceName := range sortedPreparedStackServiceNames(configuration) {
		service := configuration.Services[serviceName]
		if service.Replicas == nil && service.Resources == nil {
			continue
		}
		rows = append(rows, []string{
			serviceName,
			optionalIntLabel(service.Replicas),
			serviceResourcesSummary(service.Resources),
			serviceResourceTarget(service.Resources),
		})
	}
	return rows
}

func preparedInstanceCapacityReviewRows(instance PreparedInstance) [][]string {
	rows := [][]string{}
	for _, source := range instance.Source.Services {
		service, ok := instance.Services[source.Name]
		if !ok || (service.Replicas == nil && service.Resources == nil) {
			continue
		}
		rows = append(rows, []string{
			source.Name,
			service.Target.StackService.Name,
			optionalIntLabel(service.Replicas),
			serviceResourcesSummary(service.Resources),
			serviceResourceTarget(service.Resources),
		})
	}
	return rows
}

func preparedInstanceVersionReviewRows(instance PreparedInstance) [][]string {
	rows := [][]string{}
	for _, source := range instance.Source.Services {
		service, ok := instance.Services[source.Name]
		if !ok || strings.TrimSpace(service.InstanceVersion) == "" {
			continue
		}
		rows = append(rows, []string{source.Name, service.Target.StackService.Name, service.InstanceVersion})
	}
	return rows
}

func preparedInstanceEnvVarReviewRows(instance PreparedInstance) [][]string {
	rows := [][]string{}
	for _, source := range instance.Source.Services {
		service, ok := instance.Services[source.Name]
		if !ok {
			continue
		}
		for _, variable := range service.InstanceEnvVars {
			value := strconv.Quote(migratedEnvironmentValue(service.Source, variable, serviceTargetNamesFromPrepared(instance)))
			if variable.Secret || variable.Protected {
				value = "protected value transferred in memory"
			}
			rows = append(rows, []string{source.Name, service.Target.StackService.Name, variable.Name, value})
		}
	}
	return rows
}

func optionalIntLabel(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}

func serviceResourceTarget(resources *PreparedServiceResources) string {
	if resources == nil {
		return "-"
	}
	return resources.Workload + "/" + resources.Container
}

func preparedMigrationForReview(prepared []PreparedMigration, sourceUUID string) (PreparedMigration, bool) {
	for _, migration := range prepared {
		if app, found := migration.ForApp(sourceUUID); found {
			return app, true
		}
	}
	return PreparedMigration{}, false
}

func preparedStackSettingReviewItems(configuration PreparedStackConfiguration) []ReviewItem {
	items := []ReviewItem{}
	for _, serviceName := range sortedPreparedStackServiceNames(configuration) {
		for _, mapping := range configuration.Services[serviceName].SettingMappings {
			items = append(items, ReviewItem{
				Severity: SeverityMigration,
				Subject:  "setting " + serviceName + "." + mapping.Name,
				Message:  fmt.Sprintf("%s → %q; %s", mapping.Source, mapping.Value, mapping.Action),
			})
		}
	}
	return items
}

func preparedStackEnvVarReviewRows(configuration PreparedStackConfiguration) [][]string {
	rows := [][]string{}
	for _, serviceName := range sortedPreparedStackServiceNames(configuration) {
		for _, variable := range configuration.Services[serviceName].EnvVars {
			if variable.Name == wodby1LegacyEnvVarsMarker {
				continue
			}
			value := strconv.Quote(variable.Value)
			if variable.Secret {
				value = "protected value transferred in memory"
			}
			scope := "all instances"
			if variable.EnvType != nil && strings.TrimSpace(*variable.EnvType) != "" {
				scope = strings.ToUpper(strings.TrimSpace(*variable.EnvType)) + " instances"
			}
			rows = append(rows, []string{serviceName, variable.Name, value, scope})
		}
	}
	return rows
}

func reviewItemsWithoutSubject(items []ReviewItem, subject string) []ReviewItem {
	result := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		if item.Subject != subject {
			result = append(result, item)
		}
	}
	return result
}

func reviewItemsWithoutMessageFragment(items []ReviewItem, fragment string) []ReviewItem {
	result := make([]ReviewItem, 0, len(items))
	for _, item := range items {
		if !strings.Contains(item.Message, fragment) {
			result = append(result, item)
		}
	}
	return result
}

func reviewScopeHasItems(items []ReviewItem, app, instance string) bool {
	for _, item := range items {
		if item.App == app && item.Instance == instance {
			return true
		}
	}
	return false
}

func reviewItemsWithPromotedCommonScope(items []ReviewItem, apps []AppPlan) []ReviewItem {
	result := append([]ReviewItem(nil), items...)
	for _, app := range apps {
		if len(app.Instances) < 2 {
			continue
		}
		instanceNames := map[string]bool{}
		for _, instance := range app.Instances {
			instanceNames[instance.Name] = true
		}
		type commonItem struct {
			instances map[string]bool
			first     ReviewItem
		}
		common := map[string]*commonItem{}
		for _, item := range result {
			if item.App != app.Name || !instanceNames[item.Instance] {
				continue
			}
			key := strings.Join([]string{item.Severity, item.Subject, item.Message}, "\x00")
			group := common[key]
			if group == nil {
				group = &commonItem{instances: map[string]bool{}, first: item}
				common[key] = group
			}
			group.instances[item.Instance] = true
		}
		promoted := map[string]ReviewItem{}
		for key, group := range common {
			if len(group.instances) != len(instanceNames) {
				continue
			}
			item := group.first
			item.Instance = ""
			item.Message = "All migrated instances: " + item.Message
			promoted[key] = item
		}
		if len(promoted) == 0 {
			continue
		}
		next := make([]ReviewItem, 0, len(result)-len(promoted)*(len(instanceNames)-1))
		added := map[string]bool{}
		for _, item := range result {
			key := strings.Join([]string{item.Severity, item.Subject, item.Message}, "\x00")
			promotedItem, found := promoted[key]
			if !found || item.App != app.Name || !instanceNames[item.Instance] {
				next = append(next, item)
				continue
			}
			if !added[key] {
				next = append(next, promotedItem)
				added[key] = true
			}
		}
		result = next
	}
	return result
}

func reviewItemsForScope(items []ReviewItem, severity, app, instance string) []ReviewItem {
	result := make([]ReviewItem, 0)
	for _, item := range items {
		if item.Severity == severity && item.App == app && item.Instance == instance {
			result = append(result, item)
		}
	}
	return result
}

func reviewItemDetailRows(items []ReviewItem) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Subject, item.Message})
	}
	return rows
}

func printReviewItemDetails(w io.Writer, indent string, items []ReviewItem, color string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(w, indent+"Details:")
	printReviewTableColor(w, indent+"  ", []string{"Subject", "Details"}, reviewItemDetailRows(items), color)
}

func printColoredSectionHeading(w io.Writer, indent, title, color string) {
	fmt.Fprintf(w, "%s%s\n", indent, migrationColor(w, ansiBold+color, title+":"))
}

func printScopedReviewSections(w io.Writer, indent string, items []ReviewItem, app, instance string) {
	migrations := reviewItemsForScope(items, SeverityMigration, app, instance)
	if len(migrations) != 0 {
		printColoredSectionHeading(w, indent, "Migrations", ansiGreen)
		printReviewItemDetails(w, indent+"  ", migrations, ansiGreen)
	}
	printScopedNonMigrationSections(w, indent, items, app, instance)
}

func printScopedNonMigrationSections(w io.Writer, indent string, items []ReviewItem, app, instance string) {
	sections := []struct {
		severity string
		title    string
	}{
		{SeverityConfirmation, "Warnings"},
		{SeverityServiceWarning, "Enabled services not migrated"},
		{SeverityBlocking, "Blocking"},
		{SeverityManual, "Manual follow-up"},
		{SeveritySkipped, "Intentionally skipped"},
	}
	for _, section := range sections {
		scoped := reviewItemsForScope(items, section.severity, app, instance)
		if len(scoped) == 0 {
			continue
		}
		color := reviewSeverityColor(section.severity)
		printColoredSectionHeading(w, indent, fmt.Sprintf("%s (%d)", section.title, len(scoped)), color)
		printReviewTableColor(w, indent+"  ", []string{"Subject", "Details"}, reviewItemDetailRows(scoped), color)
	}
}

func reviewStackRow(stack StackPlan) []string {
	action := "use mapped stack"
	target := firstNonEmpty(stack.Target, stack.Name)
	if stack.CreateTarget {
		action = "create and configure"
		target = "new from catalog " + firstNonEmpty(stack.CatalogName, stack.Target)
	} else if stack.ExplicitMapping {
		action = "use and configure existing"
	}
	return []string{action, emptyDash(stack.Name), target, emptyDash(stack.TargetVersion)}
}

func sharedAppStackRows(app AppPlan) ([][]string, bool) {
	if len(app.Instances) == 0 {
		return nil, false
	}
	first := reviewStackRow(app.Instances[0].Stack)
	for _, instance := range app.Instances[1:] {
		if !sameReviewRow(first, reviewStackRow(instance.Stack)) {
			return nil, false
		}
	}
	return [][]string{first}, true
}

func serviceOverviewRows(services []ServicePlan) [][]string {
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		target := emptyDash(service.TargetName)
		state := "disabled"
		if service.Enabled {
			state = "enabled"
		}
		stackChange := "reuse"
		if service.AddToStack {
			stackChange = "add service"
		}
		rows = append(rows, []string{
			service.SourceName,
			target,
			emptyDash(service.SourceVersion),
			emptyDash(service.TargetVersion),
			emptyDash(service.VersionAction),
			state,
			service.Action,
			stackChange,
			strconv.Itoa(service.EnvVars),
			strconv.Itoa(service.CronJobs),
			strconv.Itoa(service.Settings),
		})
	}
	return rows
}

func appServiceMigrationSummary(app AppPlan, patterns int) string {
	services := map[string]bool{}
	for _, instance := range app.Instances {
		for _, service := range instance.Services {
			services[service.SourceName] = true
		}
	}
	return fmt.Sprintf("%d source service(s) across %d instance(s), consolidated into %d mapping pattern(s)", len(services), len(app.Instances), patterns)
}

func printServiceOverview(w io.Writer, indent string, rows [][]string) {
	printReviewTable(w, indent, []string{"Source", "Target", "Source version", "Target version", "Version action", "State", "Action", "Stack change", "Env vars", "Cron jobs", "Source settings", "Applies to"}, rows)
}

func appServiceOverviewRows(app AppPlan) [][]string {
	rowByKey := map[string][]string{}
	instancesByKey := map[string]map[int]bool{}
	keys := []string{}
	for instanceIndex, instance := range app.Instances {
		for _, row := range serviceOverviewRows(instance.Services) {
			key := strings.Join(row, "\x00")
			if _, exists := rowByKey[key]; !exists {
				rowByKey[key] = row
				instancesByKey[key] = map[int]bool{}
				keys = append(keys, key)
			}
			instancesByKey[key][instanceIndex] = true
		}
	}
	rows := make([][]string, 0, len(keys))
	for _, key := range keys {
		row := append([]string(nil), rowByKey[key]...)
		row = append(row, appServiceOverviewScope(app, instancesByKey[key]))
		rows = append(rows, row)
	}
	return rows
}

func appServiceOverviewScope(app AppPlan, selected map[int]bool) string {
	if len(selected) == len(app.Instances) {
		return "all instances"
	}
	byEnv := map[string][]int{}
	for index, instance := range app.Instances {
		envType := strings.ToUpper(strings.TrimSpace(instance.TargetEnvType))
		if envType != "" {
			byEnv[envType] = append(byEnv[envType], index)
		}
	}
	envTypes := []string{}
	covered := 0
	for envType, indexes := range byEnv {
		allSelected := true
		for _, index := range indexes {
			if !selected[index] {
				allSelected = false
				break
			}
		}
		if allSelected {
			envTypes = append(envTypes, envType)
			covered += len(indexes)
		}
	}
	if covered == len(selected) && len(envTypes) != 0 {
		sort.Strings(envTypes)
		return strings.Join(envTypes, "/") + " instances"
	}
	instances := []string{}
	for index, instance := range app.Instances {
		if selected[index] {
			instances = append(instances, firstNonEmpty(instance.Title, instance.Name))
		}
	}
	return "instance " + strings.Join(instances, ", ")
}

func sharedAppCronRows(app AppPlan) ([][]string, bool) {
	if len(app.Instances) < 2 {
		return nil, false
	}
	first := reviewCronRows(app.Instances[0])
	if len(first) == 0 {
		return nil, false
	}
	for _, instance := range app.Instances[1:] {
		if !sameReviewRows(first, reviewCronRows(instance)) {
			return nil, false
		}
	}
	return first, true
}

func sameReviewRows(left, right [][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameReviewRow(left[index], right[index]) {
			return false
		}
	}
	return true
}

func sameReviewRow(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func printRepositoryMigration(w io.Writer, indent string, plan Plan, repository *RepositoryPlan, prepared PreparedMigration) {
	if repository == nil && !preparedHasCodeStrategy(prepared) {
		return
	}
	fmt.Fprintln(w, indent+"Repository and CI:")
	ci := preparedCISummary(plan, prepared)
	action := "unlinked"
	repositoryName := "-"
	buildService := preparedBuildServiceSummary(prepared)
	gitIntegration := "-"
	repositoryCheck := "not linked"
	if repository != nil {
		action = repository.Action
		repositoryName = emptyDash(repository.RepositoryName)
		if strings.TrimSpace(repository.TargetService) != "" {
			buildService = repository.TargetService
		}
		if repository.GitIntegrationID > 0 {
			gitIntegration = fmt.Sprintf("ID %d", repository.GitIntegrationID)
		}
		if strings.TrimSpace(repository.RemoteGitRepoID) != "" {
			repositoryCheck = "exact match found"
		}
	}
	printReviewTableColor(w, indent+"  ", []string{"Action", "CI", "Git integration", "Target repository", "Repository check", "Git ref used", "Build service"}, [][]string{{
		action,
		ci,
		gitIntegration,
		repositoryName,
		repositoryCheck,
		preparedGitRefSummary(prepared),
		emptyDash(buildService),
	}}, ansiGreen)
}

func preparedHasCodeStrategy(prepared PreparedMigration) bool {
	for _, instance := range prepared.Instances {
		if instance.BuildSource != nil || instance.SkipCode {
			return true
		}
	}
	return false
}

func preparedCISummary(plan Plan, prepared PreparedMigration) string {
	usesWodbyCI := false
	usesExternalCI := false
	for _, instance := range prepared.Instances {
		usesWodbyCI = usesWodbyCI || instance.UsesWodbyCI
		usesExternalCI = usesExternalCI || instance.ExternalCIOnly
	}
	if usesWodbyCI && usesExternalCI {
		return "Wodby CI and Custom CI (per instance)"
	}
	if usesExternalCI {
		if plan.Target.CIIntegrationID > 0 {
			return fmt.Sprintf("third-party CI (ID %d)", plan.Target.CIIntegrationID)
		}
		return "Custom CI (automatic)"
	}
	return "Wodby CI (default)"
}

func preparedBuildServiceSummary(prepared PreparedMigration) string {
	names := map[string]bool{}
	for _, instance := range prepared.Instances {
		if instance.BuildSource != nil && strings.TrimSpace(instance.BuildSource.ServiceName) != "" {
			names[instance.BuildSource.ServiceName] = true
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return strings.Join(result, ", ")
}

func preparedGitRefSummary(prepared PreparedMigration) string {
	type gitRef struct {
		instance string
		refType  string
		ref      string
	}
	refs := []gitRef{}
	for _, instance := range prepared.Instances {
		if instance.BuildSource == nil || instance.BuildSource.Input.GitRef == nil || instance.BuildSource.Input.GitRefType == nil {
			continue
		}
		ref := strings.TrimSpace(*instance.BuildSource.Input.GitRef)
		refType := strings.ToLower(strings.TrimSpace(*instance.BuildSource.Input.GitRefType))
		if ref == "" || refType == "" {
			continue
		}
		refs = append(refs, gitRef{
			instance: firstNonEmpty(instance.Source.Title, instance.Source.Name),
			refType:  refType,
			ref:      ref,
		})
	}
	if len(refs) == 0 {
		return "-"
	}
	allSame := true
	for _, ref := range refs[1:] {
		if ref.refType != refs[0].refType || ref.ref != refs[0].ref {
			allSame = false
			break
		}
	}
	if allSame {
		return fmt.Sprintf("%s %q", refs[0].refType, refs[0].ref)
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, fmt.Sprintf("%s: %s %q", ref.instance, ref.refType, ref.ref))
	}
	return strings.Join(parts, "; ")
}

func printIntegrationMigrations(w io.Writer, indent string, integrations []IntegrationPlan) {
	if len(integrations) == 0 {
		return
	}
	fmt.Fprintln(w, indent+"Integrations:")
	rows := make([][]string, 0, len(integrations))
	for _, integration := range integrations {
		provider := integration.ProviderName
		if integration.ProviderID > 0 {
			provider = fmt.Sprintf("%s (ID %d, revision %d)", provider, integration.ProviderID, integration.ProviderRevID)
		}
		rows = append(rows, []string{
			integration.Kind, provider, emptyDash(integration.Service), integration.Action,
			emptyDash(strings.Join(integration.Variables, ", ")),
		})
	}
	printReviewTableColor(w, indent+"  ", []string{"Kind", "Provider", "Service", "Action", "Variables"}, rows, ansiGreen)
}

func printBasicAuthMigration(w io.Writer, indent string, instance InstancePlan) {
	if !instance.BasicAuth.Enabled {
		return
	}
	status := "enabled"
	if instance.BasicAuth.SecretRedacted {
		status = "enabled, password redacted"
	}
	protectedRoutes := 0
	for _, route := range instance.Routes {
		if route.BasicAuth {
			protectedRoutes++
		}
	}
	fmt.Fprintln(w, indent+"Basic auth:")
	printReviewTableColor(w, indent+"  ", []string{"Source", "Target", "Protected domains", "Login", "Password"}, [][]string{{
		status,
		"create Wodby 2 route auths",
		strconv.Itoa(protectedRoutes),
		emptyDash(instance.BasicAuth.Login),
		"transferred only in memory",
	}}, ansiGreen)
}

func reviewCronRows(instance InstancePlan) [][]string {
	rows := [][]string{}
	for _, service := range instance.Services {
		for _, schedule := range service.CronSchedules {
			rows = append(rows, []string{
				service.SourceName,
				emptyDash(service.TargetName),
				schedule.Title,
				schedule.Schedule,
				schedule.Command,
				schedule.TargetState,
			})
		}
	}
	return rows
}

func reviewRouteRows(plan Plan, instance InstancePlan) [][]string {
	rows := [][]string{}
	for _, route := range instance.Routes {
		if strings.EqualFold(strings.TrimSpace(route.Type), "technical") {
			continue
		}
		rows = append(rows, []string{
			route.Host,
			reviewRouteAction(route),
			reviewRouteTarget(route),
			reviewRouteTargetState(plan, route),
			emptyDash(routeFlags(route)),
		})
	}
	return rows
}

func reviewImportRows(instance InstancePlan) [][]string {
	rows := make([][]string, 0, len(instance.Imports))
	for _, item := range instance.Imports {
		target := "-"
		if item.TargetService != "" || item.TargetImport != "" {
			target = emptyDash(item.TargetService) + ":" + emptyDash(item.TargetImport)
		}
		rows = append(rows, []string{
			item.Component,
			item.Action,
			target,
			emptyDash(firstNonEmpty(item.BackupUUID, item.SourceUUID)),
			reviewBackupCreated(item.BackupCreated),
			reviewByteSize(item.Size),
		})
	}
	return rows
}

func reviewSeverityColor(severity string) string {
	switch severity {
	case SeverityBlocking:
		return ansiRed
	case SeverityConfirmation, SeverityServiceWarning, SeverityManual:
		return ansiOrange
	case SeveritySkipped:
		return ansiGray
	default:
		return ansiCyan
	}
}

func reviewBackupCreated(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).UTC().Format("2 Jan 2006, 15:04 UTC")
}

func reviewByteSize(size int64) string {
	if size < 0 {
		return "-"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := "B"
	for _, candidate := range units {
		value /= 1024
		unit = candidate
		if value < 1024 {
			break
		}
	}
	precision := 1
	if math.Abs(value) >= 10 {
		precision = 0
	}
	return fmt.Sprintf("%.*f %s", precision, value, unit)
}

func reviewOverviewRows(plan Plan) [][]string {
	rows := make([][]string, 0)
	instanceCount := plan.Summary.Instances
	if instanceCount == 0 {
		for _, app := range plan.Apps {
			instanceCount += len(app.Instances)
		}
	}
	if len(plan.Apps) == 1 && len(plan.Apps[0].Instances) == 1 {
		rows = append(rows, []string{"Source", reviewSourceLabel(plan.Apps[0], plan.Apps[0].Instances[0])})
	} else if len(plan.Apps) != 0 {
		rows = append(rows, []string{"Source", fmt.Sprintf("%d app(s), %d instance(s)", len(plan.Apps), instanceCount)})
	} else {
		rows = append(rows, []string{"Source", "Wodby 1 " + emptyDash(plan.Source.Kind)})
	}

	rows = append(rows, []string{
		"Target organization",
		firstNonEmpty(plan.Target.OrgName, plan.Target.Org),
	})
	if plan.Target.AppID > 0 {
		rows = append(rows, []string{
			"Target app",
			fmt.Sprintf("%s (existing, ID %d)", firstNonEmpty(plan.Target.AppTitle, plan.Target.AppName), plan.Target.AppID),
		})
	}
	if plan.Target.ProjectID > 0 || strings.TrimSpace(plan.Target.Project) != "" {
		project := firstNonEmpty(plan.Target.ProjectName, plan.Target.Project)
		if plan.Target.ProjectID > 0 && strings.TrimSpace(plan.Target.Project) == "" {
			project += " (cluster owner default)"
		}
		rows = append(rows, []string{
			"Target project",
			project,
		})
	}
	rows = append(rows, []string{
		"Target cluster",
		firstNonEmpty(plan.Target.ClusterName, plan.Target.Cluster),
	})
	targetCI := "Wodby CI (built-in)"
	if plan.Target.CIIntegrationID != 0 {
		targetCI = "Selected third-party CI integration"
	} else {
		for _, app := range plan.Apps {
			for _, integration := range app.Integrations {
				if integration.Kind == "ci" {
					targetCI = "Wodby CI by default; Custom CI for source ci deployments"
				}
			}
		}
	}
	rows = append(rows, []string{"Target CI", targetCI})
	if plan.Target.Subscription != nil && plan.Target.Subscription.Plan != nil {
		subscriptionPlan := plan.Target.Subscription.Plan
		rows = append(rows, []string{
			"Target plan",
			firstNonEmpty(subscriptionPlan.Title, subscriptionPlan.Name),
		})
		if strings.EqualFold(strings.TrimSpace(subscriptionPlan.Name), "developer") {
			rows = append(rows, []string{
				"App-service usage",
				fmt.Sprintf("%.0f of %.0f before migration", subscriptionPlan.Usage, subscriptionPlan.UsageIncluded),
			})
		}
	}

	return rows
}

func reviewSourceLabel(app AppPlan, instance InstancePlan) string {
	return fmt.Sprintf(
		"%s (%s)",
		reviewInstanceLabel(app, instance),
		emptyDash(instance.SourceType),
	)
}

func reviewInstanceLabel(app AppPlan, instance InstancePlan) string {
	return firstNonEmpty(app.Title, app.Name) + " - " + firstNonEmpty(instance.Title, instance.Name)
}

func printReviewTable(w io.Writer, indent string, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeReviewTableRow(tw, indent, headers)
	separator := make([]string, len(headers))
	for index, header := range headers {
		separator[index] = strings.Repeat("-", len(header))
	}
	writeReviewTableRow(tw, indent, separator)
	for _, row := range rows {
		writeReviewTableRow(tw, indent, row)
	}
	_ = tw.Flush()
}

func printReviewTableColor(w io.Writer, indent string, headers []string, rows [][]string, color string) {
	if !migrationColorEnabled(w) {
		printReviewTable(w, indent, headers, rows)
		return
	}
	var buffer bytes.Buffer
	printReviewTable(&buffer, indent, headers, rows)
	for _, line := range strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n") {
		fmt.Fprintln(w, migrationColor(w, color, line))
	}
}

func writeReviewTableRow(w io.Writer, indent string, cells []string) {
	sanitized := make([]string, len(cells))
	replacer := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	for index, cell := range cells {
		sanitized[index] = replacer.Replace(cell)
	}
	fmt.Fprintf(w, "%s%s\n", indent, strings.Join(sanitized, "\t"))
}

func verifiedLabel(verified bool) string {
	if verified {
		return "verified"
	}
	return "not verified"
}

func routeFlags(route RoutePlan) string {
	var flags []string
	if route.SSL {
		if route.SSLCustom {
			certificate := "manual custom TLS"
			if route.TargetCertID > 0 {
				certificate = fmt.Sprintf("custom TLS cert=%d hostnames=%s", route.TargetCertID, strings.Join(route.TargetCertDNSNames, ","))
			}
			flags = append(flags, certificate)
		} else {
			flags = append(flags, "TLS")
		}
	}
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
		settings := make([]string, 0, len(route.Settings))
		for _, setting := range route.Settings {
			settings = append(settings, setting.Name+"="+setting.Value)
		}
		flags = append(flags, "settings="+strings.Join(settings, ","))
	}
	if route.ReviewRequired {
		flags = append(flags, "review")
	}
	return strings.Join(flags, " ")
}

func reviewRouteAction(route RoutePlan) string {
	switch route.Action {
	case "create_backend":
		return "serve"
	case "create_redirect":
		return "redirect"
	case "skip_disabled":
		return "skip disabled source domain"
	case "skip_technical":
		return "skip technical domain"
	default:
		return route.Action
	}
}

func reviewRouteTarget(route RoutePlan) string {
	if route.Action == "create_redirect" {
		switch {
		case strings.TrimSpace(route.RedirectTarget) != "":
			return route.RedirectTarget
		case route.RedirectToWWW:
			return "www hostname"
		case route.RedirectNonWWW:
			return "non-www hostname"
		default:
			return "redirect target"
		}
	}
	if strings.TrimSpace(route.Service) == "" {
		return "-"
	}
	if route.PortNumber == nil {
		return route.Service
	}
	return fmt.Sprintf("%s:%d", route.Service, *route.PortNumber)
}

func reviewRouteTargetState(plan Plan, route RoutePlan) string {
	switch route.Action {
	case "create_backend", "create_redirect":
		if plan.Target.OrgCapabilities == nil {
			return "creation state not verified"
		}
		if !plan.Target.OrgCapabilities.CustomDomains {
			return "will be created disabled"
		}
		return "will be created enabled"
	case "skip_disabled", "skip_technical":
		return "will not be migrated"
	default:
		if route.ReviewRequired {
			return "blocked"
		}
		return "not resolved"
	}
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
