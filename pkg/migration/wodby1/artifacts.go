package wodby1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// MigrationArtifacts keeps the reviewed plan and the resume states of one
// migration. A location names one stored state; a single app or instance
// migration has one, and a server migration has one per source app. The CLI
// keeps them in files; other programs can keep them in their own storage.
type MigrationArtifacts interface {
	PlanLocation() string
	StateLocation() string
	// ServerAppState is the location of one source app's state in a server
	// migration.
	ServerAppState(sourceAppUUID string) string
	// ServerStates lists the stored per-app states of a server migration.
	ServerStates() ([]string, error)
	Exists(location string) (bool, error)
	Same(first, second string) (bool, error)

	LoadPlan() (Plan, error)
	SavePlan(plan Plan) error

	// InspectState reads a state without an expected identity. LoadState
	// returns an error wrapping fs.ErrNotExist when there is none.
	InspectState(location string) (*MigrationState, error)
	LoadState(location string, expected MigrationStateIdentity) (*MigrationState, error)
	RemoveRestartableState(location string, expected MigrationStateIdentity) error
	RemoveStateAfterTargetDeletion(location string, expected MigrationStateIdentity, targetAppID int) error
	// StateStore is what the executor saves one state through.
	StateStore(location string) MigrationStateStore

	// Lock serializes runs of the migration that owns location.
	Lock(location string) (io.Closer, error)
	// Prepare readies the storage before the first write.
	Prepare() error
	// ValidateServerApps checks that every app of a server export has its own
	// state location.
	ValidateServerApps(export Export) error
}

// MigrationStateStore persists the state of one running migration.
type MigrationStateStore interface {
	LoadOrInitialize(identity MigrationStateIdentity, sourceInstanceIDs []string) (*MigrationState, bool, error)
	// Save stores state when nothing else changed it since it was read, and
	// advances its revision.
	Save(state *MigrationState) error
	RemoveAfterRollback(expected MigrationStateIdentity) error
}

// NewFileArtifacts keeps the plan and states in private files: in the system
// temporary directory, or next to stateFile when it is set.
func NewFileArtifacts(sourceKind, sourceID, stateFile string) (MigrationArtifacts, error) {
	planPath, statePath, err := artifactPaths(sourceKind, sourceID, stateFile)
	if err != nil {
		return nil, err
	}
	return &fileArtifacts{planPath: planPath, statePath: statePath}, nil
}

// FileStateStore saves one state in the private file at path.
func FileStateStore(path string) MigrationStateStore {
	return fileStateStore{path: path}
}

type fileArtifacts struct {
	planPath  string
	statePath string
}

func (a *fileArtifacts) PlanLocation() string  { return a.planPath }
func (a *fileArtifacts) StateLocation() string { return a.statePath }
func (a *fileArtifacts) ServerAppState(sourceAppUUID string) string {
	return serverAppStatePath(a.statePath, sourceAppUUID)
}
func (a *fileArtifacts) ServerStates() ([]string, error) {
	return serverMigrationStatePaths(a.statePath)
}
func (a *fileArtifacts) Exists(location string) (bool, error) {
	return artifactExists(location)
}
func (a *fileArtifacts) Same(first, second string) (bool, error) {
	return sameArtifactPath(first, second)
}
func (a *fileArtifacts) LoadPlan() (Plan, error)  { return LoadReviewedPlan(a.planPath) }
func (a *fileArtifacts) SavePlan(plan Plan) error { return writePlanFile(a.planPath, plan) }
func (a *fileArtifacts) InspectState(location string) (*MigrationState, error) {
	return InspectMigrationState(location)
}
func (a *fileArtifacts) LoadState(location string, expected MigrationStateIdentity) (*MigrationState, error) {
	return LoadMigrationState(location, expected)
}
func (a *fileArtifacts) RemoveRestartableState(location string, expected MigrationStateIdentity) error {
	return RemoveRestartableMigrationState(location, expected)
}
func (a *fileArtifacts) RemoveStateAfterTargetDeletion(location string, expected MigrationStateIdentity, targetAppID int) error {
	return RemoveMigrationStateAfterTargetDeletion(location, expected, targetAppID)
}
func (a *fileArtifacts) StateStore(location string) MigrationStateStore {
	return FileStateStore(location)
}
func (a *fileArtifacts) Lock(location string) (io.Closer, error) {
	return AcquireMigrationStateLock(location)
}
func (a *fileArtifacts) Prepare() error {
	return ensureArtifactDirectories(a.planPath, a.statePath)
}
func (a *fileArtifacts) ValidateServerApps(export Export) error {
	return validateServerArtifactPaths(a.planPath, a.statePath, export)
}

type fileStateStore struct {
	path string
}

func (s fileStateStore) LoadOrInitialize(identity MigrationStateIdentity, sourceInstanceIDs []string) (*MigrationState, bool, error) {
	return LoadOrInitializeMigrationState(s.path, identity, sourceInstanceIDs)
}
func (s fileStateStore) Save(state *MigrationState) error { return SaveMigrationState(s.path, state) }
func (s fileStateStore) RemoveAfterRollback(expected MigrationStateIdentity) error {
	return RemoveMigrationStateAfterRollback(s.path, expected)
}

// MigrationRecords is storage for a migration's plan and states as versioned
// records, for programs that do not keep them in files.
type MigrationRecords interface {
	// Get returns a record and its revision, or an error wrapping
	// fs.ErrNotExist.
	Get(key string) (data []byte, revision uint64, err error)
	// Put stores a record when its stored revision is expected (0: it must not
	// exist) and gives it revision. It returns
	// ErrMigrationStateConcurrentUpdate otherwise.
	Put(key string, data []byte, expected uint64, revision uint64) error
	// Delete removes a record whose stored revision is expected.
	Delete(key string, expected uint64) error
	// Keys lists the stored keys that start with prefix.
	Keys(prefix string) ([]string, error)
	// Lock serializes runs of the migration that owns key.
	Lock(key string) (io.Closer, error)
}

const (
	recordPlanKey      = "plan"
	recordStateKey     = "state"
	recordServerPrefix = "state.app-"
)

// NewRecordArtifacts keeps a migration's plan and states in records. The plan
// and state checks are the same as for files.
func NewRecordArtifacts(records MigrationRecords) MigrationArtifacts {
	return &recordArtifacts{records: records}
}

type recordArtifacts struct {
	records MigrationRecords
}

func (a *recordArtifacts) PlanLocation() string  { return recordPlanKey }
func (a *recordArtifacts) StateLocation() string { return recordStateKey }
func (a *recordArtifacts) ServerAppState(sourceAppUUID string) string {
	return recordServerPrefix + sourceAppUUID
}
func (a *recordArtifacts) ServerStates() ([]string, error) {
	keys, err := a.records.Keys(recordServerPrefix)
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}
func (a *recordArtifacts) Exists(location string) (bool, error) {
	_, _, err := a.records.Get(location)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (a *recordArtifacts) Same(first, second string) (bool, error) { return first == second, nil }

func (a *recordArtifacts) LoadPlan() (Plan, error) {
	data, _, err := a.records.Get(recordPlanKey)
	if err != nil {
		return Plan{}, err
	}
	return decodeReviewedPlan(data)
}

func (a *recordArtifacts) SavePlan(plan Plan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode migration plan: %w", err)
	}
	if len(data) > maxMigrationPlanBytes {
		return invalidPlanError("plan exceeds maximum size")
	}
	_, current, err := a.records.Get(recordPlanKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return a.records.Put(recordPlanKey, data, current, current+1)
}

func (a *recordArtifacts) InspectState(location string) (*MigrationState, error) {
	state, _, err := a.readState(location)
	return state, err
}

func (a *recordArtifacts) readState(location string) (*MigrationState, uint64, error) {
	data, revision, err := a.records.Get(location)
	if err != nil {
		return nil, 0, err
	}
	state, err := decodeMigrationState(data)
	if err != nil {
		return nil, 0, err
	}
	if state.Revision != revision {
		return nil, 0, ErrMigrationStateConcurrentUpdate
	}
	return state, revision, nil
}

func (a *recordArtifacts) LoadState(location string, expected MigrationStateIdentity) (*MigrationState, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	state, err := a.InspectState(location)
	if err != nil {
		return nil, err
	}
	if err := checkMigrationStateIdentity(state, expected); err != nil {
		return nil, err
	}
	return state, nil
}

func (a *recordArtifacts) RemoveRestartableState(location string, expected MigrationStateIdentity) error {
	return a.removeState(location, expected, 0)
}

func (a *recordArtifacts) RemoveStateAfterTargetDeletion(location string, expected MigrationStateIdentity, targetAppID int) error {
	if targetAppID <= 0 {
		return invalidStateError("deleted target app ID must be positive")
	}
	if expected.Target.ExistingApp {
		return invalidStateError("an existing target app must never be treated as a migration-created deletion target")
	}
	return a.removeState(location, expected, targetAppID)
}

func (a *recordArtifacts) removeState(location string, expected MigrationStateIdentity, deletedTargetAppID int) error {
	if err := expected.validate(); err != nil {
		return err
	}
	state, revision, err := a.readState(location)
	if err != nil {
		return err
	}
	if err := checkMigrationStateRemoval(state, expected, deletedTargetAppID); err != nil {
		return err
	}
	return a.records.Delete(location, revision)
}

func (a *recordArtifacts) StateStore(location string) MigrationStateStore {
	return recordStateStore{artifacts: a, key: location}
}

func (a *recordArtifacts) Lock(location string) (io.Closer, error) {
	if strings.TrimSpace(location) == "" {
		return nil, invalidStateError("state location is required")
	}
	return a.records.Lock(location)
}

func (a *recordArtifacts) Prepare() error                         { return nil }
func (a *recordArtifacts) ValidateServerApps(export Export) error { return nil }

type recordStateStore struct {
	artifacts *recordArtifacts
	key       string
}

func (s recordStateStore) LoadOrInitialize(identity MigrationStateIdentity, sourceInstanceIDs []string) (*MigrationState, bool, error) {
	state, err := s.artifacts.LoadState(s.key, identity)
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
	if err := s.Save(state); err != nil {
		return nil, false, err
	}
	return state, true, nil
}

// Save follows SaveMigrationState: the stored state must be the one state was
// read as, and the record's revision is the state's own.
func (s recordStateStore) Save(state *MigrationState) error {
	if state == nil {
		return invalidStateError("state is required")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	current, _, err := s.artifacts.readState(s.key)
	switch {
	case err == nil:
		if current.Identity() != state.Identity() {
			return ErrMigrationStateIdentityMismatch
		}
		if current.Revision != state.Revision {
			return ErrMigrationStateConcurrentUpdate
		}
	case errors.Is(err, fs.ErrNotExist):
		if state.Revision != 0 {
			return ErrMigrationStateConcurrentUpdate
		}
	default:
		return err
	}
	next := *state
	next.Revision++
	data, err := json.Marshal(&next)
	if err != nil {
		return fmt.Errorf("encode migration state: %w", err)
	}
	if len(data) > maxMigrationStateBytes {
		return invalidStateError("state exceeds maximum size")
	}
	if err := s.artifacts.records.Put(s.key, data, state.Revision, next.Revision); err != nil {
		return err
	}
	state.Revision = next.Revision
	return nil
}

func (s recordStateStore) RemoveAfterRollback(expected MigrationStateIdentity) error {
	return s.artifacts.removeState(s.key, expected, rollbackCompleted)
}

// MemoryMigrationRecords keeps records in memory, for tests and for programs
// that keep a migration's records elsewhere between runs.
type MemoryMigrationRecords struct {
	mu      sync.Mutex
	records map[string]memoryMigrationRecord
	locked  map[string]bool
}

type memoryMigrationRecord struct {
	data     []byte
	revision uint64
}

func NewMemoryMigrationRecords() *MemoryMigrationRecords {
	return &MemoryMigrationRecords{records: map[string]memoryMigrationRecord{}, locked: map[string]bool{}}
}

func (m *MemoryMigrationRecords) Get(key string) ([]byte, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[key]
	if !ok {
		return nil, 0, fmt.Errorf("migration record %q: %w", key, fs.ErrNotExist)
	}
	return append([]byte(nil), record.data...), record.revision, nil
}

func (m *MemoryMigrationRecords) Put(key string, data []byte, expected uint64, revision uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[key].revision != expected {
		return ErrMigrationStateConcurrentUpdate
	}
	m.records[key] = memoryMigrationRecord{data: append([]byte(nil), data...), revision: revision}
	return nil
}

func (m *MemoryMigrationRecords) Delete(key string, expected uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[key]
	if !ok || record.revision != expected {
		return ErrMigrationStateConcurrentUpdate
	}
	delete(m.records, key)
	return nil
}

func (m *MemoryMigrationRecords) Keys(prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := []string{}
	for key := range m.records {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *MemoryMigrationRecords) Lock(key string) (io.Closer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked[key] {
		return nil, ErrMigrationStateLocked
	}
	m.locked[key] = true
	return memoryMigrationLock{records: m, key: key}, nil
}

type memoryMigrationLock struct {
	records *MemoryMigrationRecords
	key     string
}

func (l memoryMigrationLock) Close() error {
	l.records.mu.Lock()
	defer l.records.mu.Unlock()
	delete(l.records.locked, l.key)
	return nil
}
