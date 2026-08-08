package wodby1

import (
	"fmt"
	"strings"
)

// ScopeServerExportApp converts one application from a server export into the
// app-shaped export expected by MigrationExecutor. The authenticated digest is
// recomputed so each app has an independent, secret-bound resume identity.
func ScopeServerExportApp(export Export, sourceAppUUID string, authKey string) (Export, error) {
	if export.Source == nil || export.Source.Kind != "server" {
		return Export{}, fmt.Errorf("source export must identify a Wodby 1 server")
	}
	if err := export.ValidateSource("server", export.Source.UUID); err != nil {
		return Export{}, err
	}
	var selected *AppExport
	for _, appExport := range export.AppExports() {
		if appExport.App.UUID != sourceAppUUID {
			continue
		}
		if selected != nil {
			return Export{}, fmt.Errorf("source server export contains duplicate app %q", sourceAppUUID)
		}
		item := appExport
		selected = &item
	}
	if selected == nil {
		return Export{}, fmt.Errorf("source server export does not contain app %q", sourceAppUUID)
	}

	child := Export{
		Schema:          export.Schema,
		GeneratedAt:     export.GeneratedAt,
		Source:          &ExportSource{Kind: "app", UUID: sourceAppUUID},
		SecretsIncluded: export.SecretsIncluded,
		Apps:            []AppExport{*selected},
		Issues:          append([]ExportIssue(nil), export.Issues...),
	}
	if err := child.ValidateSource("app", sourceAppUUID); err != nil {
		return Export{}, err
	}
	var err error
	child.ConfigMAC, err = child.AuthenticatedConfigDigest(authKey)
	if err != nil {
		return Export{}, fmt.Errorf("compute app-scoped source configuration digest: %w", err)
	}
	child.Digest, err = child.ContentDigest()
	if err != nil {
		return Export{}, fmt.Errorf("compute app-scoped source export digest: %w", err)
	}
	return child, nil
}

// ScopeServerMigrationApp returns the app-shaped export, plan, and target
// mapping used by one independently resumable server-migration child.
func ScopeServerMigrationApp(
	export Export,
	plan Plan,
	prepared PreparedMigration,
	sourceAppUUID string,
	authKey string,
) (Export, Plan, PreparedMigration, error) {
	if plan.Source.Kind != "server" || export.Source == nil || plan.Source.ID != export.Source.UUID {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("server export and migration plan source do not match")
	}
	childExport, err := ScopeServerExportApp(export, sourceAppUUID, authKey)
	if err != nil {
		return Export{}, Plan{}, PreparedMigration{}, err
	}

	var selected *AppPlan
	for _, app := range plan.Apps {
		if app.SourceUUID != sourceAppUUID {
			continue
		}
		if selected != nil {
			return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("server migration plan contains duplicate app %q", sourceAppUUID)
		}
		item := app
		selected = &item
	}
	if selected == nil {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("server migration plan does not contain app %q", sourceAppUUID)
	}

	childPlan := plan
	childPlan.Source.Kind = "app"
	childPlan.Source.ID = sourceAppUUID
	childPlan.Source.Schema = childExport.Schema
	childPlan.Source.GeneratedAt = childExport.GeneratedAt
	childPlan.Source.ResponseDigest = ""
	childPlan.Source.ExportDigest, err = childExport.ContentDigest()
	if err != nil {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("compute app-scoped plan export digest: %w", err)
	}
	childPlan.Source.ConfigDigest, err = childExport.MigrationConfigDigest()
	if err != nil {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("compute app-scoped plan configuration digest: %w", err)
	}
	childPlan.Source.BackupDigest, err = childExport.BackupDigest()
	if err != nil {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("compute app-scoped plan backup digest: %w", err)
	}
	childPlan.Apps = []AppPlan{*selected}
	childPlan.Review = make([]ReviewItem, 0, len(plan.Review))
	for _, item := range plan.Review {
		if item.App == "" || item.App == selected.Name || item.App == sourceAppUUID {
			childPlan.Review = append(childPlan.Review, item)
		}
	}
	sortReview(childPlan.Review)
	childPlan.Summary = PlanSummary{}
	childPlan.computeSummary()
	childPlan.PlanHash, err = childPlan.contentDigest()
	if err != nil {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("compute app-scoped migration plan digest: %w", err)
	}

	childPrepared, found := prepared.ForApp(sourceAppUUID)
	if !found && len(prepared.Apps) != 0 {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("prepared server migration does not contain app %q", sourceAppUUID)
	}
	if found && childPrepared.App.App.UUID != sourceAppUUID {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("prepared app mapping does not match %q", sourceAppUUID)
	}
	if strings.TrimSpace(childPlan.Source.ConfigDigest) == "" {
		return Export{}, Plan{}, PreparedMigration{}, fmt.Errorf("app-scoped configuration digest is required")
	}
	return childExport, childPlan, childPrepared, nil
}
