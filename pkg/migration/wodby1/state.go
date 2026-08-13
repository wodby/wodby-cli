package wodby1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	// MigrationStateSchema identifies the on-disk resumable migration state
	// format. State files intentionally contain no migration payloads, secrets,
	// or import source URLs.
	MigrationStateSchema = "wodby1-migration-state/v1"

	maxMigrationStateBytes = 4 << 20
	migrationStateFileMode = 0600
)

var (
	ErrMigrationStateIdentityMismatch = errors.New("migration state identity mismatch")
	ErrMigrationStateInsecure         = errors.New("migration state file permissions are not 0600")
	ErrMigrationStateConcurrentUpdate = errors.New("migration state was updated by another process")
	ErrMigrationStateInvalid          = errors.New("invalid migration state")
	ErrMigrationStateUnsafeRestart    = errors.New("migration state contains target mutation risk")

	stateKindPattern      = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	stateIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)
	stateDigestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	stateOperationPattern = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)
)

type MigrationStatus string

const (
	MigrationStatusInitialized MigrationStatus = "initialized"
	MigrationStatusRunning     MigrationStatus = "running"
	MigrationStatusFailed      MigrationStatus = "failed"
	MigrationStatusComplete    MigrationStatus = "complete"
)

type MigrationPhase string

const (
	MigrationPhasePlan     MigrationPhase = "plan"
	MigrationPhasePrepare  MigrationPhase = "prepare"
	MigrationPhaseSyncData MigrationPhase = "sync_data"
	MigrationPhaseFinalize MigrationPhase = "finalize"
	MigrationPhaseVerify   MigrationPhase = "verify"
)

type MigrationResourceStatus string

const (
	MigrationResourcePending   MigrationResourceStatus = "pending"
	MigrationResourceCreating  MigrationResourceStatus = "creating"
	MigrationResourceReady     MigrationResourceStatus = "ready"
	MigrationResourceFailed    MigrationResourceStatus = "failed"
	MigrationResourceAmbiguous MigrationResourceStatus = "ambiguous"
)

type MigrationOperationStatus string

const (
	MigrationOperationIntent    MigrationOperationStatus = "intent"
	MigrationOperationAccepted  MigrationOperationStatus = "accepted"
	MigrationOperationSucceeded MigrationOperationStatus = "succeeded"
	MigrationOperationFailed    MigrationOperationStatus = "failed"
	MigrationOperationAmbiguous MigrationOperationStatus = "ambiguous"
)

// MigrationStateIdentity binds a state file to one immutable source
// configuration, one migration plan, and one Wodby 2 destination. Backup
// snapshots are deliberately tracked outside this identity.
type MigrationStateIdentity struct {
	Source   MigrationStateSourceIdentity `json:"source"`
	PlanHash string                       `json:"planHash"`
	Target   MigrationStateTarget         `json:"target"`
}

type MigrationStateSourceIdentity struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	ConfigDigest string `json:"configDigest"`
}

type MigrationStateSource struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	ConfigDigest string `json:"configDigest"`
	BackupDigest string `json:"backupDigest,omitempty"`
}

type MigrationStateTarget struct {
	OrgID     int `json:"orgId"`
	ProjectID int `json:"projectId,omitempty"`
	ClusterID int `json:"clusterId"`
}

// MigrationState is the complete, non-sensitive resume record. Instances are
// keyed by their Wodby 1 UUID so encoding/json emits them deterministically.
type MigrationState struct {
	Schema    string                             `json:"schema"`
	Revision  uint64                             `json:"revision"`
	Source    MigrationStateSource               `json:"source"`
	PlanHash  string                             `json:"planHash"`
	Target    MigrationStateTarget               `json:"target"`
	Status    MigrationStatus                    `json:"status"`
	Phase     MigrationPhase                     `json:"phase"`
	App       MigrationResourceState             `json:"app"`
	Instances map[string]*MigrationResourceState `json:"instances"`
}

type MigrationResourceState struct {
	TargetID   int                                `json:"targetId,omitempty"`
	Status     MigrationResourceStatus            `json:"status"`
	Operations map[string]MigrationOperationState `json:"operations"`
}

type MigrationOperationState struct {
	Status      MigrationOperationStatus `json:"status"`
	Attempts    uint32                   `json:"attempts"`
	TargetID    int                      `json:"targetId,omitempty"`
	TaskID      int                      `json:"taskId,omitempty"`
	IntentAt    time.Time                `json:"intentAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
	FailureCode string                   `json:"failureCode,omitempty"`
}

// NewMigrationState creates an in-memory state bound to identity. Call
// SaveMigrationState before performing the first target mutation.
func NewMigrationState(identity MigrationStateIdentity, sourceInstanceIDs []string) (*MigrationState, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}

	instances := make(map[string]*MigrationResourceState, len(sourceInstanceIDs))
	for _, sourceID := range sourceInstanceIDs {
		if !stateIDPattern.MatchString(sourceID) {
			return nil, invalidStateError("source instance ID must be an opaque identifier")
		}
		if _, exists := instances[sourceID]; exists {
			return nil, invalidStateError("source instance IDs must be unique")
		}
		instances[sourceID] = newMigrationResourceState()
	}

	state := &MigrationState{
		Schema: MigrationStateSchema,
		Source: MigrationStateSource{
			Kind:         identity.Source.Kind,
			ID:           identity.Source.ID,
			ConfigDigest: identity.Source.ConfigDigest,
		},
		PlanHash:  identity.PlanHash,
		Target:    identity.Target,
		Status:    MigrationStatusInitialized,
		Phase:     MigrationPhasePlan,
		App:       *newMigrationResourceState(),
		Instances: instances,
	}
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return state, nil
}

// LoadOrInitializeMigrationState resumes an existing state file or atomically
// creates a new one. Existing files must match both identity and the exact set
// of source instances.
func LoadOrInitializeMigrationState(
	path string,
	identity MigrationStateIdentity,
	sourceInstanceIDs []string,
) (*MigrationState, bool, error) {
	state, err := LoadMigrationState(path, identity)
	if err == nil {
		if err := validateSourceInstanceSet(state, sourceInstanceIDs); err != nil {
			return nil, false, err
		}
		return state, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false, err
	}

	state, err = NewMigrationState(identity, sourceInstanceIDs)
	if err != nil {
		return nil, false, err
	}
	if err := SaveMigrationState(path, state); err != nil {
		return nil, false, err
	}
	return state, true, nil
}

// LoadMigrationState reads a secure state file and verifies that it belongs to
// expected. Unknown JSON fields are rejected so payload data cannot be smuggled
// into the persisted format.
func LoadMigrationState(path string, expected MigrationStateIdentity) (*MigrationState, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	state, _, err := loadMigrationStateFile(path)
	if err != nil {
		return nil, err
	}
	if state.Identity() != expected {
		return nil, ErrMigrationStateIdentityMismatch
	}
	return state, nil
}

// InspectMigrationState securely reads and validates a state file without
// imposing an expected identity. Callers must validate Identity before using
// it to resume or replace state for a specific migration.
func InspectMigrationState(path string) (*MigrationState, error) {
	state, _, err := loadMigrationStateFile(path)
	return state, err
}

// CanRestartSafely reports whether the state proves that no target mutation
// succeeded, was accepted, or became ambiguous. Definitive API rejections in
// the plan/prepare phases are restartable because the target rejected them
// before creating or changing a resource.
func (s *MigrationState) CanRestartSafely() bool {
	if s == nil || s.Validate() != nil {
		return false
	}
	if s.Status != MigrationStatusInitialized && s.Status != MigrationStatusFailed {
		return false
	}
	if s.Phase != MigrationPhasePlan && s.Phase != MigrationPhasePrepare {
		return false
	}
	if !restartableMigrationResource(s.App) {
		return false
	}
	for _, instance := range s.Instances {
		if instance == nil || !restartableMigrationResource(*instance) {
			return false
		}
	}
	return true
}

// RemoveRestartableMigrationState atomically revalidates the expected state
// identity and restart safety before removing it. Callers must hold the
// migration state lock so another CLI process cannot start a mutation between
// inspection and removal.
func RemoveRestartableMigrationState(path string, expected MigrationStateIdentity) error {
	return removeMigrationState(path, expected, 0)
}

// RemoveMigrationStateAfterTargetDeletion replaces a state that contains
// successful target mutations only after the caller has verified through the
// target API that the recorded app ID no longer exists. The target ID is
// revalidated under the state lock so stale callers cannot remove a different
// migration's state.
func RemoveMigrationStateAfterTargetDeletion(path string, expected MigrationStateIdentity, targetAppID int) error {
	if targetAppID <= 0 {
		return invalidStateError("deleted target app ID must be positive")
	}
	return removeMigrationState(path, expected, targetAppID)
}

func removeMigrationState(path string, expected MigrationStateIdentity, deletedTargetAppID int) error {
	if err := expected.validate(); err != nil {
		return err
	}
	state, info, err := loadMigrationStateFile(path)
	if err != nil {
		return err
	}
	if state.Identity() != expected {
		return ErrMigrationStateIdentityMismatch
	}
	if deletedTargetAppID == 0 && !state.CanRestartSafely() {
		return ErrMigrationStateUnsafeRestart
	}
	if deletedTargetAppID != 0 && state.App.TargetID != deletedTargetAppID {
		return ErrMigrationStateIdentityMismatch
	}
	if err := verifyMigrationStateTargetUnchanged(path, info); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove restartable migration state: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open migration state directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync migration state directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close migration state directory: %w", closeErr)
	}
	return nil
}

// SaveMigrationState atomically persists state with mode 0600. The state
// revision provides optimistic protection against stale in-memory writers.
func SaveMigrationState(path string, state *MigrationState) error {
	if state == nil {
		return invalidStateError("state is required")
	}
	if path == "" {
		return invalidStateError("state path is required")
	}
	if err := state.Validate(); err != nil {
		return err
	}

	var previousInfo fs.FileInfo
	current, info, err := loadMigrationStateFile(path)
	switch {
	case err == nil:
		if current.Identity() != state.Identity() {
			return ErrMigrationStateIdentityMismatch
		}
		if current.Revision != state.Revision {
			return ErrMigrationStateConcurrentUpdate
		}
		previousInfo = info
	case errors.Is(err, fs.ErrNotExist):
		if state.Revision != 0 {
			return ErrMigrationStateConcurrentUpdate
		}
	default:
		return err
	}

	next := *state
	next.Revision++
	data, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode migration state: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxMigrationStateBytes {
		return invalidStateError("state file exceeds maximum size")
	}

	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create migration state temporary file: %w", err)
	}
	tempPath := temp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(migrationStateFileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure migration state temporary file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write migration state temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync migration state temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close migration state temporary file: %w", err)
	}

	if err := verifyMigrationStateTargetUnchanged(path, previousInfo); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace migration state file: %w", err)
	}
	renamed = true
	state.Revision = next.Revision

	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open migration state directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync migration state directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close migration state directory: %w", closeErr)
	}
	return nil
}

func (s *MigrationState) Identity() MigrationStateIdentity {
	if s == nil {
		return MigrationStateIdentity{}
	}
	return MigrationStateIdentity{
		Source: MigrationStateSourceIdentity{
			Kind:         s.Source.Kind,
			ID:           s.Source.ID,
			ConfigDigest: s.Source.ConfigDigest,
		},
		PlanHash: s.PlanHash,
		Target:   s.Target,
	}
}

func restartableMigrationResource(resource MigrationResourceState) bool {
	if resource.TargetID != 0 ||
		(resource.Status != MigrationResourcePending && resource.Status != MigrationResourceFailed) {
		return false
	}
	for _, operation := range resource.Operations {
		if operation.Status != MigrationOperationFailed ||
			operation.FailureCode != "api_rejected" ||
			operation.TargetID != 0 || operation.TaskID != 0 {
			return false
		}
	}
	return true
}

// Validate verifies both the schema and every persisted field. Arbitrary
// payload fields are not represented by the format and are rejected on load.
func (s *MigrationState) Validate() error {
	if s == nil {
		return invalidStateError("state is required")
	}
	if s.Schema != MigrationStateSchema {
		return invalidStateError("unsupported schema")
	}
	if err := s.Identity().validate(); err != nil {
		return err
	}
	if !validMigrationStatus(s.Status) {
		return invalidStateError("unsupported migration status")
	}
	if !validMigrationPhase(s.Phase) {
		return invalidStateError("unsupported migration phase")
	}
	if s.Source.BackupDigest != "" && !stateDigestPattern.MatchString(s.Source.BackupDigest) {
		return invalidStateError("source backup digest must be a lowercase SHA-256 digest")
	}
	if err := validateMigrationResourceState("app", &s.App); err != nil {
		return err
	}
	if s.Instances == nil {
		return invalidStateError("instances map is required")
	}
	for sourceID, instance := range s.Instances {
		if !stateIDPattern.MatchString(sourceID) {
			return invalidStateError("source instance ID must be an opaque identifier")
		}
		if err := validateMigrationResourceState("instance", instance); err != nil {
			return err
		}
	}
	if s.Status == MigrationStatusComplete {
		if s.App.Status != MigrationResourceReady {
			return invalidStateError("completed migration requires a ready app")
		}
		for _, instance := range s.Instances {
			if instance.Status != MigrationResourceReady {
				return invalidStateError("completed migration requires ready instances")
			}
		}
	}
	return nil
}

func (s *MigrationState) SetStatus(status MigrationStatus) error {
	if !validMigrationStatus(status) {
		return invalidStateError("unsupported migration status")
	}
	previous := s.Status
	s.Status = status
	if err := s.Validate(); err != nil {
		s.Status = previous
		return err
	}
	return nil
}

func (s *MigrationState) SetPhase(phase MigrationPhase) error {
	if !validMigrationPhase(phase) {
		return invalidStateError("unsupported migration phase")
	}
	previous := s.Phase
	s.Phase = phase
	if err := s.Validate(); err != nil {
		s.Phase = previous
		return err
	}
	return nil
}

// SetBackupDigest records the selected backup snapshot independently of the
// source configuration identity. Once apply starts, resume must keep it exact.
func (s *MigrationState) SetBackupDigest(digest string) error {
	if digest != "" && !stateDigestPattern.MatchString(digest) {
		return invalidStateError("source backup digest must be a lowercase SHA-256 digest")
	}
	previous := s.Source.BackupDigest
	s.Source.BackupDigest = digest
	if err := s.Validate(); err != nil {
		s.Source.BackupDigest = previous
		return err
	}
	return nil
}

func (s *MigrationState) SetAppTarget(targetID int, status MigrationResourceStatus) error {
	return s.setResourceTarget(&s.App, targetID, status)
}

func (s *MigrationState) SetInstanceTarget(
	sourceInstanceID string,
	targetID int,
	status MigrationResourceStatus,
) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.setResourceTarget(instance, targetID, status)
}

func (s *MigrationState) MarkAppOperationIntent(operation string) error {
	return s.markOperationIntent(&s.App, operation)
}

func (s *MigrationState) MarkAppOperationSuccess(operation string) error {
	return s.MarkAppOperationSuccessWithIDs(operation, 0, 0)
}

func (s *MigrationState) MarkAppOperationSuccessWithIDs(
	operation string,
	targetID int,
	taskID int,
) error {
	return s.markOperationSuccess(&s.App, operation, targetID, taskID)
}

func (s *MigrationState) MarkAppOperationFailure(operation string, failureCode string) error {
	return s.markOperationFailure(&s.App, operation, failureCode)
}

func (s *MigrationState) MarkAppOperationAmbiguous(operation string) error {
	return s.MarkAppOperationAmbiguousWithIDs(operation, 0, 0)
}

func (s *MigrationState) MarkAppOperationAmbiguousWithIDs(
	operation string,
	targetID int,
	taskID int,
) error {
	return s.markOperationAmbiguous(&s.App, operation, targetID, taskID)
}

func (s *MigrationState) MarkInstanceOperationIntent(sourceInstanceID string, operation string) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.markOperationIntent(instance, operation)
}

func (s *MigrationState) MarkInstanceOperationSuccess(sourceInstanceID string, operation string) error {
	return s.MarkInstanceOperationSuccessWithIDs(sourceInstanceID, operation, 0, 0)
}

func (s *MigrationState) MarkInstanceOperationAcceptedWithIDs(
	sourceInstanceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.markOperationAccepted(instance, operation, targetID, taskID)
}

func (s *MigrationState) MarkInstanceOperationSuccessWithIDs(
	sourceInstanceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.markOperationSuccess(instance, operation, targetID, taskID)
}

func (s *MigrationState) MarkInstanceOperationFailure(
	sourceInstanceID string,
	operation string,
	failureCode string,
) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.markOperationFailure(instance, operation, failureCode)
}

func (s *MigrationState) MarkInstanceOperationAmbiguous(
	sourceInstanceID string,
	operation string,
) error {
	return s.MarkInstanceOperationAmbiguousWithIDs(sourceInstanceID, operation, 0, 0)
}

func (s *MigrationState) MarkInstanceOperationAmbiguousWithIDs(
	sourceInstanceID string,
	operation string,
	targetID int,
	taskID int,
) error {
	instance, err := s.instance(sourceInstanceID)
	if err != nil {
		return err
	}
	return s.markOperationAmbiguous(instance, operation, targetID, taskID)
}

func (s *MigrationState) setResourceTarget(
	resource *MigrationResourceState,
	targetID int,
	status MigrationResourceStatus,
) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if targetID < 0 {
		return invalidStateError("target ID cannot be negative")
	}
	if !validMigrationResourceStatus(status) {
		return invalidStateError("unsupported resource status")
	}
	if status == MigrationResourceReady && targetID == 0 {
		return invalidStateError("ready resource requires a target ID")
	}
	previousID, previousStatus := resource.TargetID, resource.Status
	resource.TargetID, resource.Status = targetID, status
	if err := s.Validate(); err != nil {
		resource.TargetID, resource.Status = previousID, previousStatus
		return err
	}
	return nil
}

func (s *MigrationState) markOperationIntent(resource *MigrationResourceState, operation string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if !stateOperationPattern.MatchString(operation) {
		return invalidStateError("operation name must be an opaque status key")
	}
	if s.Status == MigrationStatusComplete {
		return invalidStateError("completed migration cannot start an operation")
	}
	if resource.Operations == nil {
		resource.Operations = map[string]MigrationOperationState{}
	}
	current, exists := resource.Operations[operation]
	if exists && current.Status == MigrationOperationSucceeded {
		return invalidStateError("succeeded operation cannot be restarted")
	}
	if current.Attempts == ^uint32(0) {
		return invalidStateError("operation attempts limit reached")
	}
	now := time.Now().UTC()
	current.Status = MigrationOperationIntent
	current.Attempts++
	current.TargetID = 0
	current.TaskID = 0
	current.IntentAt = now
	current.UpdatedAt = now
	current.FailureCode = ""
	resource.Operations[operation] = current
	s.Status = MigrationStatusRunning
	return s.Validate()
}

func (s *MigrationState) markOperationAccepted(
	resource *MigrationResourceState,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if !stateOperationPattern.MatchString(operation) {
		return invalidStateError("operation name must be an opaque status key")
	}
	if targetID < 0 || taskID < 0 || (targetID == 0 && taskID == 0) {
		return invalidStateError("accepted operation requires a target or task ID")
	}
	current, exists := resource.Operations[operation]
	if !exists ||
		(current.Status != MigrationOperationIntent && current.Status != MigrationOperationAccepted) {
		return invalidStateError("operation acceptance requires a recorded intent")
	}
	if current.TargetID > 0 && targetID > 0 && current.TargetID != targetID {
		return invalidStateError("accepted operation target ID cannot change")
	}
	if current.TaskID > 0 && taskID > 0 && current.TaskID != taskID {
		return invalidStateError("accepted operation task ID cannot change")
	}
	current.Status = MigrationOperationAccepted
	if targetID > 0 {
		current.TargetID = targetID
	}
	if taskID > 0 {
		current.TaskID = taskID
	}
	current.UpdatedAt = nextMigrationOperationTimestamp(current.IntentAt)
	current.FailureCode = ""
	resource.Operations[operation] = current
	s.Status = MigrationStatusRunning
	return s.Validate()
}

func (s *MigrationState) markOperationSuccess(
	resource *MigrationResourceState,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if !stateOperationPattern.MatchString(operation) {
		return invalidStateError("operation name must be an opaque status key")
	}
	if targetID < 0 || taskID < 0 {
		return invalidStateError("operation target and task IDs cannot be negative")
	}
	current, exists := resource.Operations[operation]
	if !exists || current.Status != MigrationOperationIntent {
		return invalidStateError("operation success requires a recorded intent")
	}
	current.Status = MigrationOperationSucceeded
	current.TargetID = targetID
	current.TaskID = taskID
	current.UpdatedAt = nextMigrationOperationTimestamp(current.IntentAt)
	current.FailureCode = ""
	resource.Operations[operation] = current
	return s.Validate()
}

func (s *MigrationState) markOperationFailure(
	resource *MigrationResourceState,
	operation string,
	failureCode string,
) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if !stateOperationPattern.MatchString(operation) {
		return invalidStateError("operation name must be an opaque status key")
	}
	if !stateOperationPattern.MatchString(failureCode) {
		return invalidStateError("failure code must be an opaque status key")
	}
	current, exists := resource.Operations[operation]
	if !exists || current.Status != MigrationOperationIntent {
		return invalidStateError("operation failure requires a recorded intent")
	}
	current.Status = MigrationOperationFailed
	current.UpdatedAt = nextMigrationOperationTimestamp(current.IntentAt)
	current.FailureCode = failureCode
	resource.Operations[operation] = current
	s.Status = MigrationStatusFailed
	return s.Validate()
}

func (s *MigrationState) markOperationAmbiguous(
	resource *MigrationResourceState,
	operation string,
	targetID int,
	taskID int,
) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if !stateOperationPattern.MatchString(operation) {
		return invalidStateError("operation name must be an opaque status key")
	}
	if targetID < 0 || taskID < 0 {
		return invalidStateError("operation target and task IDs cannot be negative")
	}
	current, exists := resource.Operations[operation]
	if !exists || current.Status != MigrationOperationIntent {
		return invalidStateError("ambiguous operation requires a recorded intent")
	}
	current.Status = MigrationOperationAmbiguous
	current.TargetID = targetID
	current.TaskID = taskID
	current.UpdatedAt = nextMigrationOperationTimestamp(current.IntentAt)
	current.FailureCode = ""
	resource.Operations[operation] = current
	s.Status = MigrationStatusFailed
	return s.Validate()
}

func (s *MigrationState) instance(sourceInstanceID string) (*MigrationResourceState, error) {
	if s == nil || s.Instances == nil {
		return nil, invalidStateError("instances map is required")
	}
	instance, exists := s.Instances[sourceInstanceID]
	if !exists {
		return nil, invalidStateError("source instance is not part of this migration")
	}
	return instance, nil
}

func (i MigrationStateIdentity) validate() error {
	if !stateKindPattern.MatchString(i.Source.Kind) {
		return invalidStateError("source kind must be an opaque identifier")
	}
	if !stateIDPattern.MatchString(i.Source.ID) {
		return invalidStateError("source ID must be an opaque identifier")
	}
	if !stateDigestPattern.MatchString(i.Source.ConfigDigest) {
		return invalidStateError("source config digest must be a lowercase SHA-256 digest")
	}
	if !stateDigestPattern.MatchString(i.PlanHash) {
		return invalidStateError("plan hash must be a lowercase SHA-256 digest")
	}
	if i.Target.OrgID <= 0 || i.Target.ProjectID < 0 || i.Target.ClusterID <= 0 {
		return invalidStateError("target org and cluster IDs must be positive and project ID cannot be negative")
	}
	return nil
}

func newMigrationResourceState() *MigrationResourceState {
	return &MigrationResourceState{
		Status:     MigrationResourcePending,
		Operations: map[string]MigrationOperationState{},
	}
}

func validateMigrationResourceState(name string, resource *MigrationResourceState) error {
	if resource == nil {
		return invalidStateError(name + " state is required")
	}
	if resource.TargetID < 0 {
		return invalidStateError(name + " target ID cannot be negative")
	}
	if !validMigrationResourceStatus(resource.Status) {
		return invalidStateError(name + " has unsupported resource status")
	}
	if resource.Status == MigrationResourceReady && resource.TargetID == 0 {
		return invalidStateError(name + " is ready without a target ID")
	}
	if resource.Operations == nil {
		return invalidStateError(name + " operations map is required")
	}
	for operation, state := range resource.Operations {
		if !stateOperationPattern.MatchString(operation) {
			return invalidStateError(name + " operation name must be an opaque status key")
		}
		if state.Attempts == 0 {
			return invalidStateError(name + " operation must record at least one attempt")
		}
		if state.TargetID < 0 || state.TaskID < 0 {
			return invalidStateError(name + " operation target and task IDs cannot be negative")
		}
		if state.IntentAt.IsZero() || state.UpdatedAt.IsZero() {
			return invalidStateError(name + " operation timestamps are required")
		}
		if state.UpdatedAt.Before(state.IntentAt) {
			return invalidStateError(name + " operation update cannot precede its intent")
		}
		switch state.Status {
		case MigrationOperationIntent, MigrationOperationSucceeded:
			if state.FailureCode != "" {
				return invalidStateError(name + " non-failed operation cannot have a failure code")
			}
		case MigrationOperationAccepted:
			if state.FailureCode != "" {
				return invalidStateError(name + " accepted operation cannot have a failure code")
			}
			if state.TargetID == 0 && state.TaskID == 0 {
				return invalidStateError(name + " accepted operation requires a target or task ID")
			}
		case MigrationOperationAmbiguous:
			if state.FailureCode != "" {
				return invalidStateError(name + " ambiguous operation cannot have a failure code")
			}
		case MigrationOperationFailed:
			if !stateOperationPattern.MatchString(state.FailureCode) {
				return invalidStateError(name + " failed operation requires an opaque failure code")
			}
		default:
			return invalidStateError(name + " operation has unsupported status")
		}
	}
	return nil
}

func validateSourceInstanceSet(state *MigrationState, sourceInstanceIDs []string) error {
	expected := append([]string(nil), sourceInstanceIDs...)
	sort.Strings(expected)
	actual := make([]string, 0, len(state.Instances))
	for sourceID := range state.Instances {
		actual = append(actual, sourceID)
	}
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return ErrMigrationStateIdentityMismatch
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return ErrMigrationStateIdentityMismatch
		}
	}
	return nil
}

func validMigrationStatus(status MigrationStatus) bool {
	switch status {
	case MigrationStatusInitialized, MigrationStatusRunning, MigrationStatusFailed, MigrationStatusComplete:
		return true
	default:
		return false
	}
}

func validMigrationPhase(phase MigrationPhase) bool {
	switch phase {
	case MigrationPhasePlan,
		MigrationPhasePrepare,
		MigrationPhaseSyncData,
		MigrationPhaseFinalize,
		MigrationPhaseVerify:
		return true
	default:
		return false
	}
}

func validMigrationResourceStatus(status MigrationResourceStatus) bool {
	switch status {
	case MigrationResourcePending,
		MigrationResourceCreating,
		MigrationResourceReady,
		MigrationResourceFailed,
		MigrationResourceAmbiguous:
		return true
	default:
		return false
	}
}

func nextMigrationOperationTimestamp(intentAt time.Time) time.Time {
	now := time.Now().UTC()
	if now.Before(intentAt) {
		return intentAt
	}
	return now
}

func loadMigrationStateFile(path string) (*MigrationState, fs.FileInfo, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: state path must be a regular file", ErrMigrationStateInvalid)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect migration state file: %w", err)
	}
	if !os.SameFile(linkInfo, info) {
		return nil, nil, ErrMigrationStateConcurrentUpdate
	}
	if info.Mode().Perm() != migrationStateFileMode {
		return nil, nil, ErrMigrationStateInsecure
	}
	if info.Size() > maxMigrationStateBytes {
		return nil, nil, invalidStateError("state file exceeds maximum size")
	}

	data, err := io.ReadAll(io.LimitReader(file, maxMigrationStateBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read migration state file: %w", err)
	}
	if len(data) > maxMigrationStateBytes {
		return nil, nil, invalidStateError("state file exceeds maximum size")
	}

	var state MigrationState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, nil, fmt.Errorf("%w: decode state: %v", ErrMigrationStateInvalid, err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, invalidStateError("state file contains trailing JSON")
		}
		return nil, nil, fmt.Errorf("%w: decode trailing state data: %v", ErrMigrationStateInvalid, err)
	}
	if state.Revision == 0 {
		return nil, nil, invalidStateError("persisted state revision must be positive")
	}
	if err := state.Validate(); err != nil {
		return nil, nil, err
	}
	return &state, info, nil
}

func verifyMigrationStateTargetUnchanged(path string, previous fs.FileInfo) error {
	current, err := os.Lstat(path)
	if previous == nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return ErrMigrationStateConcurrentUpdate
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrMigrationStateConcurrentUpdate
		}
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() ||
		current.Mode().Perm() != migrationStateFileMode ||
		!os.SameFile(previous, current) ||
		current.Size() != previous.Size() ||
		!current.ModTime().Equal(previous.ModTime()) {
		return ErrMigrationStateConcurrentUpdate
	}
	return nil
}

func invalidStateError(message string) error {
	return fmt.Errorf("%w: %s", ErrMigrationStateInvalid, message)
}
