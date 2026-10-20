package wodby1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pkg/errors"
)

func artifactPaths(sourceKind, sourceID, statePath string) (string, string, error) {
	sourceKind = strings.ToLower(strings.TrimSpace(sourceKind))
	if sourceKind != "instance" && sourceKind != "app" && sourceKind != "server" {
		return "", "", errors.Errorf("unsupported migration source kind %q", sourceKind)
	}
	if strings.TrimSpace(sourceID) == "" {
		return "", "", errors.New("source UUID is required")
	}
	for _, char := range sourceID {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return "", "", errors.New("source UUID contains unsupported characters")
	}
	dir := defaultMigrationArtifactDir()
	base := "wodby1-" + sourceKind + "-" + sourceID
	planPath := filepath.Join(dir, base+".migration-plan.json")
	if strings.TrimSpace(statePath) == "" {
		statePath = filepath.Join(dir, base+".migration-state.json")
	} else {
		// Keep the plan with an explicit state file. Resume, verify and
		// rollback need both, and the temporary directory can be cleared,
		// for example on reboot. A plan written by an earlier version in the
		// temporary directory is still used until it is replaced.
		explicitPlan := explicitStatePlanPath(statePath)
		legacyExists, err := artifactExists(planPath)
		if err != nil {
			return "", "", err
		}
		explicitExists, err := artifactExists(explicitPlan)
		if err != nil {
			return "", "", err
		}
		if explicitExists || !legacyExists {
			planPath = explicitPlan
		}
	}
	planPath = filepath.Clean(planPath)
	statePath = filepath.Clean(statePath)
	same, err := sameArtifactPath(planPath, statePath)
	if err != nil {
		return "", "", err
	}
	if same {
		return "", "", errors.New("--state-file must not resolve to the migration plan path")
	}
	sameLock, err := sameArtifactPath(planPath, statePath+".lock")
	if err != nil {
		return "", "", err
	}
	if sameLock {
		return "", "", errors.New("migration plan must not resolve to the migration state lock path")
	}
	return planPath, statePath, nil
}

func defaultMigrationArtifactDir() string {
	return filepath.Join(os.TempDir(), "wodby-migrations")
}

func ensureArtifactDirectories(planPath, statePath string) error {
	defaultDir := defaultMigrationArtifactDir()
	info, err := os.Lstat(defaultDir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(defaultDir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return errors.Wrap(err, "create temporary Wodby migration directory")
		}
		info, err = os.Lstat(defaultDir)
	}
	if err != nil {
		return errors.WithStack(err)
	}
	// Windows reports 0777 for every directory; access there follows the ACL.
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return errors.Errorf("temporary Wodby migration directory %s must be a private directory with no group or other permissions", defaultDir)
	}
	for _, dir := range []string{filepath.Dir(planPath), filepath.Dir(statePath)} {
		if dir == defaultDir {
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.Wrap(err, "create migration artifact directory")
		}
	}
	return nil
}

// explicitStatePlanPath names the plan stored next to an explicit state file:
// "migration.json" keeps its plan in "migration.plan.json".
func explicitStatePlanPath(statePath string) string {
	ext := filepath.Ext(statePath)
	if ext == "" {
		return statePath + ".plan.json"
	}
	return strings.TrimSuffix(statePath, ext) + ".plan" + ext
}

func artifactExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, errors.WithStack(err)
}

func serverAppStatePath(basePath string, sourceAppUUID string) string {
	digest := sha256.Sum256([]byte(sourceAppUUID))
	suffix := fmt.Sprintf(".app-%x", digest[:8])
	ext := filepath.Ext(basePath)
	if ext == "" {
		return basePath + suffix
	}
	return strings.TrimSuffix(basePath, ext) + suffix + ext
}

// serverMigrationStatePaths returns every per-app state reserved by a server
// migration. The aggregate state path itself is intentionally never written.
func serverMigrationStatePaths(basePath string) ([]string, error) {
	dir := filepath.Dir(basePath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WithStack(err)
	}

	filename := filepath.Base(basePath)
	ext := filepath.Ext(filename)
	prefix := strings.TrimSuffix(filename, ext) + ".app-"
	paths := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		digest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		if len(digest) != 16 {
			continue
		}
		if _, err := hex.DecodeString(digest); err == nil {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths, nil
}

func validateServerArtifactPaths(planPath string, statePath string, export Export) error {
	seen := map[string]string{}
	for _, app := range export.AppExports() {
		childPath := serverAppStatePath(statePath, app.App.UUID)
		same, err := sameArtifactPath(planPath, childPath)
		if err != nil {
			return err
		}
		if same {
			return errors.Errorf("temporary plan file must not resolve to the per-app migration state path for source app %q", app.App.UUID)
		}
		canonical, err := canonicalArtifactPath(childPath)
		if err != nil {
			return err
		}
		if existing, duplicate := seen[canonical]; duplicate {
			return errors.Errorf("source apps %q and %q resolve to the same migration state path", existing, app.App.UUID)
		}
		seen[canonical] = app.App.UUID
	}
	return nil
}

func sameArtifactPath(first string, second string) (bool, error) {
	firstCanonical, err := canonicalArtifactPath(first)
	if err != nil {
		return false, err
	}
	secondCanonical, err := canonicalArtifactPath(second)
	if err != nil {
		return false, err
	}
	if firstCanonical == secondCanonical {
		return true, nil
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	if firstErr == nil && secondErr == nil {
		return os.SameFile(firstInfo, secondInfo), nil
	}
	if firstErr != nil && !errors.Is(firstErr, fs.ErrNotExist) {
		return false, firstErr
	}
	if secondErr != nil && !errors.Is(secondErr, fs.ErrNotExist) {
		return false, secondErr
	}
	return false, nil
}

func canonicalArtifactPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.WithStack(err)
	}
	current := absolute
	missing := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errors.WithStack(err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolute), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func writePlanFile(path string, plan Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return errors.WithStack(err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if err := file.Close(); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.Rename(tempPath, path))
}
