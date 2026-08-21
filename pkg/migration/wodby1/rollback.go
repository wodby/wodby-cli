package wodby1

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// RollbackPlan is what a rollback would delete, resolved from migration state
// before anything is removed. It is shown to the operator for confirmation and
// then executed in the order its fields are declared.
type RollbackPlan struct {
	// AppID is set only when this migration created the app. A migration that
	// added instances to an app that already existed leaves the app alone and
	// deletes InstanceIDs instead.
	AppID       int
	AppName     string
	InstanceIDs []RollbackInstance
	StackID     int
	// Integrations the migration created. Integrations it reused are never
	// listed here.
	IntegrationIDs []int
	// SkippedIntegrations are integrations whose provenance this state does not
	// record, because it was written before the migration tracked that. They
	// are reported rather than deleted, since deleting a reused integration
	// would break other apps in the organization.
	SkippedIntegrations []int
	// ReusedIntegrations were resolved to integrations that already existed.
	ReusedIntegrations []int
}

type RollbackInstance struct {
	ID   int
	Name string
}

func (p RollbackPlan) empty() bool {
	return p.AppID == 0 && len(p.InstanceIDs) == 0 && p.StackID == 0 && len(p.IntegrationIDs) == 0
}

// DeletesResources reports whether executing the plan removes anything from
// Wodby 2. A plan that only forgets local resume state does not need a second
// destructive confirmation after the fresh apply plan was approved.
func (p RollbackPlan) DeletesResources() bool {
	return !p.empty()
}

// ErrRollbackAfterCutover reports a migration that has already been verified,
// which means DNS points at Wodby 2 and the target is serving live traffic.
var ErrRollbackAfterCutover = errors.New(
	"this migration was verified, so DNS already points at Wodby 2 and the target is serving live traffic;" +
		" point DNS back at Wodby 1 and confirm the source is serving before rolling back",
)

// PlanRollback resolves what a rollback would delete from migration state
// alone. It never contacts the target, so an operator can see the exact list
// before authorizing anything.
func PlanRollback(state *MigrationState) (RollbackPlan, error) {
	if state == nil {
		return RollbackPlan{}, errors.New("migration state is required")
	}
	if err := state.Validate(); err != nil {
		return RollbackPlan{}, err
	}
	// A completed migration has been through --verify, which only succeeds once
	// DNS resolves to Wodby 2.
	if state.Status == MigrationStatusComplete || state.Phase == MigrationPhaseVerify {
		return RollbackPlan{}, ErrRollbackAfterCutover
	}
	if unresolved := unresolvedRollbackOperations(state); len(unresolved) != 0 {
		return RollbackPlan{}, errors.Errorf(
			"rollback cannot safely identify every target mutation while these operations are unresolved: %s; resume the migration and resolve or retry them before restarting",
			strings.Join(unresolved, ", "),
		)
	}

	plan := RollbackPlan{}
	if state.App.TargetID > 0 && !state.Target.ExistingApp {
		plan.AppID = state.App.TargetID
	}
	if plan.AppID == 0 {
		// The app predates this migration, so only the instances it added may
		// be removed. Deleting the app would take unrelated instances with it.
		for sourceID, instance := range state.Instances {
			if instance == nil || instance.TargetID <= 0 {
				continue
			}
			plan.InstanceIDs = append(plan.InstanceIDs, RollbackInstance{ID: instance.TargetID, Name: sourceID})
		}
		sort.Slice(plan.InstanceIDs, func(i, j int) bool { return plan.InstanceIDs[i].ID < plan.InstanceIDs[j].ID })
	}
	if operation, ok := state.App.Operations[generatedStackOperation]; ok &&
		operation.Status == MigrationOperationSucceeded && operation.TargetID > 0 {
		if operation.Created {
			plan.StackID = operation.TargetID
		}
	}
	for name, operation := range state.App.Operations {
		if !strings.HasPrefix(name, "integration_resolve.") ||
			operation.Status != MigrationOperationSucceeded || operation.TargetID <= 0 {
			continue
		}
		switch {
		case operation.Created:
			plan.IntegrationIDs = append(plan.IntegrationIDs, operation.TargetID)
		case !state.TracksProvenance:
			// Written before provenance was recorded: a missing Created flag
			// does not mean the integration was reused.
			plan.SkippedIntegrations = append(plan.SkippedIntegrations, operation.TargetID)
		default:
			plan.ReusedIntegrations = append(plan.ReusedIntegrations, operation.TargetID)
		}
	}
	sort.Ints(plan.IntegrationIDs)
	sort.Ints(plan.SkippedIntegrations)
	sort.Ints(plan.ReusedIntegrations)
	return plan, nil
}

func unresolvedRollbackOperations(state *MigrationState) []string {
	if state == nil {
		return nil
	}
	result := []string{}
	collect := func(scope string, resource MigrationResourceState) {
		for name, operation := range resource.Operations {
			switch operation.Status {
			case MigrationOperationIntent, MigrationOperationAccepted, MigrationOperationAmbiguous:
				result = append(result, fmt.Sprintf("%s:%s (%s)", scope, name, operation.Status))
			}
		}
	}
	collect("app", state.App)
	for sourceID, instance := range state.Instances {
		if instance != nil {
			collect("instance:"+sourceID, *instance)
		}
	}
	sort.Strings(result)
	return result
}

// Describe renders the plan for the confirmation prompt.
func (p RollbackPlan) Describe(appName string) string {
	return p.describe(appName, "Rollback", false)
}

// DescribeRestart renders the resources from a saved migration that must be
// removed before a fresh plan can be applied. It deliberately avoids calling
// this a rollback: restart cleanup is one part of the new apply operation, not
// a separate action the operator has already approved.
func (p RollbackPlan) DescribeRestart(appName string) string {
	return p.describe(appName, "Restart", true)
}

func (p RollbackPlan) describe(appName string, action string, continuing bool) string {
	var b strings.Builder
	if !p.DeletesResources() {
		b.WriteString("No migration-created Wodby 2 resources need to be deleted.\n")
		if continuing {
			b.WriteString("The saved local migration state will be replaced before the fresh migration starts.\n")
		} else {
			b.WriteString("The saved local migration state will be replaced; Wodby 1 is not touched.\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "%s will delete the following from Wodby 2:\n\n", action)
	if p.AppID > 0 {
		fmt.Fprintf(&b, "  app %q (ID %d) and every app instance, service, route, and imported\n", appName, p.AppID)
		b.WriteString("    database and files under it\n")
	}
	for _, instance := range p.InstanceIDs {
		fmt.Fprintf(&b, "  app instance ID %d and its imported data (the app itself is kept)\n", instance.ID)
	}
	if p.StackID > 0 {
		fmt.Fprintf(&b, "  the stack this migration generated (ID %d)\n", p.StackID)
	}
	for _, id := range p.IntegrationIDs {
		fmt.Fprintf(&b, "  integration ID %d, created by this migration\n", id)
	}
	if len(p.ReusedIntegrations) != 0 {
		fmt.Fprintf(&b, "\nKept: %d integration(s) that already existed and were reused.\n", len(p.ReusedIntegrations))
	}
	if len(p.SkippedIntegrations) != 0 {
		fmt.Fprintf(
			&b,
			"\nKept: %d integration(s) whose origin this migration state does not record.\n"+
				"Review and remove them by hand if this migration created them: %v\n",
			len(p.SkippedIntegrations), p.SkippedIntegrations,
		)
	}
	b.WriteString("\nThis deletion cannot be undone. Wodby 1 is not touched.\n")
	if continuing {
		b.WriteString("After cleanup, the fresh migration plan will be saved and applied.\n")
	}
	return b.String()
}

// Rollback deletes the target resources this migration created, in dependency
// order, and removes the resume state once nothing is left behind.
//
// The order is forced by Wodby 2: an app instance holds the running services
// and imported data, and a stack cannot be deleted while an app instance still
// references its revision. Integrations go last because app services link to
// them. Deleting the app cascades to its instances in one task, so instances
// are deleted individually only when the app itself must be kept.
func (e *MigrationExecutor) Rollback(
	ctx context.Context,
	state *MigrationState,
	plan RollbackPlan,
	appName string,
) error {
	if state == nil {
		return errors.New("migration state is required")
	}
	if plan.empty() {
		e.reportProgress("Nothing recorded in migration state was created in Wodby 2; nothing to roll back.")
		return RemoveMigrationStateAfterRollback(e.statePath, state.Identity())
	}

	if plan.AppID > 0 {
		e.reportProgress("Step: delete target app %q (ID %d) and everything under it.", appName, plan.AppID)
		if err := e.deleteAndWait(ctx, "app", plan.AppID, func(ctx context.Context) (TargetOperationResult, error) {
			return e.target.DeleteApp(ctx, plan.AppID)
		}); err != nil {
			return err
		}
	}
	for _, instance := range plan.InstanceIDs {
		e.reportProgress("Step: delete target app instance ID %d.", instance.ID)
		if err := e.deleteAndWait(ctx, "app instance", instance.ID, func(ctx context.Context) (TargetOperationResult, error) {
			return e.target.DeleteAppInstance(ctx, instance.ID)
		}); err != nil {
			return err
		}
	}

	// Deleting the app only schedules instance teardown; the stack stays
	// referenced until that finishes, so confirm the instances are gone rather
	// than trusting the task alone.
	if plan.StackID > 0 {
		if err := e.awaitInstancesGone(ctx, plan); err != nil {
			return err
		}
		e.reportProgress("Step: delete the stack this migration generated (ID %d).", plan.StackID)
		if err := e.deleteAndWait(ctx, "stack", plan.StackID, func(ctx context.Context) (TargetOperationResult, error) {
			return e.target.DeleteStack(ctx, plan.StackID)
		}); err != nil {
			return err
		}
	}
	for _, id := range plan.IntegrationIDs {
		e.reportProgress("Step: delete integration ID %d created by this migration.", id)
		if err := e.deleteAndWait(ctx, "integration", id, func(ctx context.Context) (TargetOperationResult, error) {
			return e.target.DeleteIntegration(ctx, id)
		}); err != nil {
			return err
		}
	}

	e.reportProgress("Rollback complete. Wodby 1 was not touched.")
	return RemoveMigrationStateAfterRollback(e.statePath, state.Identity())
}

func (e *MigrationExecutor) deleteAndWait(
	ctx context.Context,
	resource string,
	id int,
	remove func(context.Context) (TargetOperationResult, error),
) error {
	result, err := remove(ctx)
	if err != nil {
		// Already gone is the outcome rollback wants, so a partially completed
		// rollback can be rerun without tripping over its own progress.
		if isTargetNotFound(err) {
			e.reportProgress("  Target %s ID %d is already deleted.", resource, id)
			return nil
		}
		return errors.Wrapf(err, "delete target %s ID %d", resource, id)
	}
	if result.TaskID != nil && *result.TaskID > 0 {
		e.reportProgress("  Waiting for %s deletion task ID %d...", resource, *result.TaskID)
		if err := e.waitTask(ctx, *result.TaskID); err != nil {
			return errors.Wrapf(err, "wait for target %s ID %d deletion", resource, id)
		}
	}
	e.reportProgress("  Target %s ID %d deleted.", resource, id)
	return nil
}

// awaitInstancesGone blocks until no app instance from this migration still
// resolves, so the generated stack is no longer referenced.
func (e *MigrationExecutor) awaitInstancesGone(ctx context.Context, plan RollbackPlan) error {
	ids := make([]int, 0, len(plan.InstanceIDs))
	for _, instance := range plan.InstanceIDs {
		ids = append(ids, instance.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return e.poll(ctx, "app instance teardown", func(ctx context.Context) (bool, error) {
		for _, id := range ids {
			_, err := e.target.GetAppInstance(ctx, id)
			if err == nil {
				return false, nil
			}
			if !isTargetNotFound(err) {
				return false, err
			}
		}
		return true, nil
	})
}
