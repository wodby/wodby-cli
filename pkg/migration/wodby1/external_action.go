package wodby1

import (
	stderrors "errors"
	"fmt"
	"strings"
)

// wodbyCIRepositoryURL is the public home of the Wodby CLI 2.x pipeline
// examples. Customers migrating a third-party CI app must rewrite their
// pipeline against these before the target can produce its first build.
const wodbyCIRepositoryURL = "https://github.com/wodby/wodby-ci/tree/2.0"

// ExternalActionRequiredError reports that the migration cannot continue until
// somebody performs an action outside Wodby. It is deliberately distinct from a
// migration failure: every recorded operation succeeded, the target is intact,
// and rerunning the same --apply command resumes from this point once the
// external action is done.
type ExternalActionRequiredError struct {
	// Instance is the source instance name the migration stopped on.
	Instance string
	// TargetInstanceID is the created Wodby 2 app instance.
	TargetInstanceID int
	// ServiceName and TargetServiceID identify the code service whose build is
	// missing. TargetServiceID is what the pipeline must publish to.
	ServiceName     string
	TargetServiceID int
	// ProviderKey and ProviderLabel name the CI provider Wodby 1 recorded on
	// the instance's last successful build. Empty when Wodby 1 reported none we
	// recognize.
	ProviderKey   string
	ProviderLabel string
	// ProviderSupported reports whether Wodby 2 has a CI provider for it.
	// Wodby 1 recognizes Bitbucket Pipelines, Travis CI, and Jenkins, none of
	// which Wodby 2 supports; those migrate as Custom CI.
	ProviderSupported bool
	// ExampleURL points at the closest wodby/wodby-ci example for this app.
	ExampleURL string
	// GitRef is set only when the reviewed build source pins a ref, which
	// narrows which pipeline run the migration can adopt.
	GitRef string
}

func (e *ExternalActionRequiredError) Error() string {
	return fmt.Sprintf(
		"instance %q is waiting for its first Custom CI build of service %q (app service ID %d)",
		e.Instance, e.ServiceName, e.TargetServiceID,
	)
}

// Summary is the single-line headline shown before the numbered steps.
func (e *ExternalActionRequiredError) Summary() string {
	return fmt.Sprintf(
		"Instance %q needs one build from your own CI pipeline before Wodby 2 can deploy it.",
		e.Instance,
	)
}

// NextSteps renders the operator-facing instructions. Callers print this
// instead of a failure message, so it must be complete on its own.
func (e *ExternalActionRequiredError) NextSteps() string {
	example := strings.TrimSpace(e.ExampleURL)
	if example == "" {
		example = wodbyCIRepositoryURL
	}
	provider := strings.TrimSpace(e.ProviderLabel)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", e.Summary())
	b.WriteString("This app deploys through third-party CI, so Wodby 2 cannot start the build\n")
	b.WriteString("itself; only your pipeline can publish the image. Everything else for this\n")
	b.WriteString("instance is already created and configured.\n\n")

	b.WriteString("Next steps\n")
	switch {
	case provider != "" && e.ProviderSupported:
		fmt.Fprintf(&b, "  1. Update your %s configuration to Wodby CLI 2.x, starting\n", provider)
		fmt.Fprintf(&b, "     from the Wodby CI example closest to this app:\n       %s\n", example)
	case provider != "":
		// Wodby 1 recognizes providers Wodby 2 has no counterpart for. Saying so
		// beats linking a page that holds nothing for this pipeline.
		fmt.Fprintf(&b, "  1. Update your %s configuration to Wodby CLI 2.x.\n", provider)
		fmt.Fprintf(&b, "     Wodby 2 does not support %s; adapt one of\n", provider)
		fmt.Fprintf(&b, "     %s:\n       %s\n", wodby2CIProviderLabels, example)
	default:
		b.WriteString("  1. Update your CI pipeline to Wodby CLI 2.x, starting from the\n")
		fmt.Fprintf(&b, "     Wodby CI example closest to this app:\n       %s\n", example)
	}
	fmt.Fprintf(&b, "     All examples: %s\n", wodbyCIRepositoryURL)
	b.WriteString("  2. Give the pipeline these values:\n")
	b.WriteString("       WODBY_API_KEY         your Wodby 2 API key (store it as a secret)\n")
	fmt.Fprintf(&b, "       WODBY_APP_SERVICE_ID  %d\n", e.TargetServiceID)
	if provider != "" && !e.ProviderSupported {
		// Wodby CLI 2.x autodetects only the providers Wodby 2 supports;
		// everything else records the build as "unknown" without this.
		fmt.Fprintf(&b, "     Wodby CLI 2.x does not autodetect %s, so run\n", provider)
		fmt.Fprintf(&b, "     `wodby ci init --provider %s`.\n", e.ProviderKey)
	}
	b.WriteString("  3. Run the pipeline once, through `wodby ci deploy`. The build only reaches\n")
	b.WriteString("     COMPLETED after its deployment finishes; `wodby ci build` and\n")
	b.WriteString("     `wodby ci release` alone leave it IN_PROGRESS and this step repeats.\n")
	if ref := strings.TrimSpace(e.GitRef); ref != "" {
		fmt.Fprintf(&b, "     Build the reviewed Git ref %q; other refs are not adopted.\n", ref)
	}
	b.WriteString("  4. Rerun the same --apply command. The migration adopts that build and\n")
	b.WriteString("     continues with deployment and data import.\n")

	fmt.Fprintf(
		&b,
		"\nTarget app instance ID %d, code service %q app service ID %d.\n",
		e.TargetInstanceID, e.ServiceName, e.TargetServiceID,
	)
	b.WriteString("Nothing failed and nothing needs to be cleaned up; the migration is paused.\n")
	return b.String()
}

// MigrationPausedError aggregates every instance waiting on an external action.
// An app or server migration can pause on several instances in one run, and
// each has its own app service ID for the pipeline to publish to.
type MigrationPausedError struct {
	Actions []*ExternalActionRequiredError
}

func (p *MigrationPausedError) Error() string {
	if p == nil || len(p.Actions) == 0 {
		return "migration is paused"
	}
	if len(p.Actions) == 1 {
		return p.Actions[0].Error()
	}
	return fmt.Sprintf(
		"%d instances are waiting for their first Custom CI build; first: %v",
		len(p.Actions), p.Actions[0],
	)
}

// Unwrap exposes the first pending action so errors.As finds the detail type
// through the aggregate.
func (p *MigrationPausedError) Unwrap() error {
	if p == nil || len(p.Actions) == 0 {
		return nil
	}
	return p.Actions[0]
}

// NextSteps renders instructions for every paused instance.
func (p *MigrationPausedError) NextSteps() string {
	if p == nil || len(p.Actions) == 0 {
		return ""
	}
	blocks := make([]string, 0, len(p.Actions))
	for _, action := range p.Actions {
		blocks = append(blocks, action.NextSteps())
	}
	return strings.Join(blocks, "\n")
}

func (p *MigrationPausedError) empty() bool {
	return p == nil || len(p.Actions) == 0
}

func (p *MigrationPausedError) add(action *ExternalActionRequiredError) {
	if action != nil {
		p.Actions = append(p.Actions, action)
	}
}

// AsExternalActionRequired reports whether err is, or wraps, a paused-migration
// signal rather than a failure.
func AsExternalActionRequired(err error) (*ExternalActionRequiredError, bool) {
	var blocked *ExternalActionRequiredError
	if stderrors.As(err, &blocked) {
		return blocked, true
	}
	return nil, false
}

// AsMigrationPaused returns the full set of pending external actions when err
// reports a paused migration. It also normalizes a bare single action so
// callers have one rendering path.
func AsMigrationPaused(err error) (*MigrationPausedError, bool) {
	var paused *MigrationPausedError
	if stderrors.As(err, &paused) {
		return paused, true
	}
	if blocked, ok := AsExternalActionRequired(err); ok {
		return &MigrationPausedError{Actions: []*ExternalActionRequiredError{blocked}}, true
	}
	return nil, false
}

// wodbyCIExampleURL resolves the most specific wodby/wodby-ci example available
// for a stack and CI provider, falling back to broader pages.
func wodbyCIExampleURL(stack, providerExamplePath string) string {
	stack = strings.TrimSpace(stack)
	providerExamplePath = strings.TrimSpace(providerExamplePath)
	if stack == "" {
		return wodbyCIRepositoryURL
	}
	if providerExamplePath != "" {
		return "https://github.com/wodby/wodby-ci/blob/2.0/" + stack + "/" + providerExamplePath
	}
	return wodbyCIRepositoryURL + "/" + stack
}
