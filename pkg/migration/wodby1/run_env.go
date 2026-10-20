package wodby1

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	// DefaultSourceBaseURL is the Wodby 1 API a migration exports from.
	DefaultSourceBaseURL = "https://api.wodby.com"
	// SourceTokenEnv names the environment variable the CLI reads the Wodby 1
	// token from.
	SourceTokenEnv = "WODBY1_SOURCE_TOKEN"
)

// RunOptions are the options of `wodby migrate wodby1 instance|app|server`.
// Mapping values use the command's flag syntax, for example SOURCE=TARGET.
type RunOptions struct {
	SourceBaseURL string
	SourceToken   string
	Apply         bool
	Verify        bool
	Restart       bool
	Rollback      bool
	Yes           bool

	TargetProject    string
	TargetCluster    string
	TargetApp        string
	TargetEnvMap     []string
	TargetStackID    int
	TargetStackMap   []string
	TargetServiceMap []string
	TargetVersionMap []string
	TargetImportMap  []string

	TargetCIIntegrationID  int
	TargetWodbyCI          bool
	TargetLetsEncrypt      bool
	TargetGitIntegrationID int
	TargetRepositoryName   string
	TargetRepositoryMap    []string
	TargetCodeService      string
	TargetGitRef           string
	TargetGitRefType       string

	SkipCode                bool
	SkipData                bool
	SourceBackups           []string
	AllowUnsupportedDrupal  bool
	AddMissingServices      bool
	ExcludeApps             []string
	ExcludeInstances        []string
	Workspace               bool
	WorkspaceInstances      []string
	WorkspaceStorageClass   string
	WorkspaceStorageService string

	StateFile      string
	PollInterval   time.Duration
	WaitTimeout    time.Duration
	RetryAmbiguous string
	Output         string
}

// DefaultRunOptions returns the command's defaults.
func DefaultRunOptions() *RunOptions {
	return &RunOptions{
		SourceBaseURL:      DefaultSourceBaseURL,
		PollInterval:       2 * time.Second,
		WaitTimeout:        30 * time.Minute,
		Output:             "text",
		AddMissingServices: true,
	}
}

// RunEnv is where a migration run reads input, writes output and keeps its
// plan and state. The CLI passes its terminal and files; another program, such
// as the Wodby backend, passes its own writers and artifact store.
type RunEnv struct {
	Ctx context.Context
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// JSON prints machine-readable results instead of text.
	JSON    bool
	Verbose bool
	// Target is the Wodby 2 API client the migration acts with.
	Target *TargetClient
	// Artifacts opens the plan and state store of a migration. Nil keeps them
	// in files, as the CLI does.
	Artifacts func(sourceKind, sourceID, stateFile string) (MigrationArtifacts, error)
	// RestartSuggestion tells the operator how to restart a saved migration
	// whose plan no longer applies.
	RestartSuggestion func(sourceKind, sourceID string) string
	// ApprovedPlanHash, when set, is the hash of the plan the operator approved
	// elsewhere. A new or restarted apply then proceeds without a prompt only
	// when the plan it builds has that hash.
	ApprovedPlanHash string
	// OnPlan receives the plan a run built, with the inspected target, or nil
	// when a custom stack needs a target stack first. A preview reports it
	// this way to a program; its hash is what an apply must be approved for.
	OnPlan func(plan Plan, prepared *PreparedMigration)
	// SilenceUsage and SilenceErrors are called once the run has explained a
	// failure itself, so the caller does not add usage or repeat the error.
	SilenceUsage  func()
	SilenceErrors func()
}

// RunSingle previews, applies, verifies, restarts or rolls back the migration
// of one Wodby 1 app or instance (sourceKind "app" or "instance").
func RunSingle(env *RunEnv, sourceKind, sourceID string, opts *RunOptions) error {
	if env == nil || opts == nil {
		return errors.New("migration run environment and options are required")
	}
	return runWodby1Single(env, sourceKind, sourceID, opts)
}

// RunServer does the same for every app of one Wodby 1 server.
func RunServer(env *RunEnv, sourceID string, opts *RunOptions) error {
	if env == nil || opts == nil {
		return errors.New("migration run environment and options are required")
	}
	return runWodby1Server(env, sourceID, opts)
}

// ValidateRunOptions checks options before a run. The caller checks its own
// Wodby 2 credentials.
func ValidateRunOptions(opts *RunOptions) error {
	return validateOptions(opts)
}

func (e *RunEnv) Context() context.Context {
	if e.Ctx == nil {
		return context.Background()
	}
	return e.Ctx
}

func (e *RunEnv) OutOrStdout() io.Writer {
	if e.Out == nil {
		return io.Discard
	}
	return e.Out
}

func (e *RunEnv) ErrOrStderr() io.Writer {
	if e.Err == nil {
		return e.OutOrStdout()
	}
	return e.Err
}

func (e *RunEnv) InOrStdin() io.Reader {
	if e.In == nil {
		return strings.NewReader("")
	}
	return e.In
}

func (e *RunEnv) onPlan(plan Plan, prepared *PreparedMigration) {
	if e.OnPlan != nil {
		e.OnPlan(plan, prepared)
	}
}

func (e *RunEnv) silenceUsage() {
	if e.SilenceUsage != nil {
		e.SilenceUsage()
	}
}

func (e *RunEnv) silenceErrors() {
	e.silenceUsage()
	if e.SilenceErrors != nil {
		e.SilenceErrors()
	}
}

func (e *RunEnv) targetClient() (*TargetClient, error) {
	if e.Target == nil {
		return nil, errors.New("a Wodby 2 API client is required")
	}
	return e.Target, nil
}

func (e *RunEnv) restartSuggestion(sourceKind, sourceID string) string {
	if e.RestartSuggestion != nil {
		return e.RestartSuggestion(sourceKind, sourceID)
	}
	return "wodby migrate wodby1 " + sourceKind + " " + sourceID + " [same options] --apply --restart"
}

func (e *RunEnv) artifacts(sourceKind, sourceID, stateFile string) (MigrationArtifacts, error) {
	if e.Artifacts != nil {
		return e.Artifacts(sourceKind, sourceID, stateFile)
	}
	return NewFileArtifacts(sourceKind, sourceID, stateFile)
}

// checkApprovedPlan refuses a plan other than the one approved elsewhere.
func (e *RunEnv) checkApprovedPlan(plan Plan) error {
	if e.ApprovedPlanHash == "" || plan.PlanHash == e.ApprovedPlanHash {
		return nil
	}
	return errors.New("the migration plan changed after it was approved; preview it again and approve the new plan")
}
