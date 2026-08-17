package run

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/cicache"
	"github.com/wodby/wodby-cli/pkg/ciuser"
	"github.com/wodby/wodby-cli/pkg/docker"
)

const cacheLabel = "com.wodby.ci.cache"

type cacheProfile struct {
	envName       string
	hostPath      []string
	containerPath string
}

var cacheProfiles = map[string]cacheProfile{
	"npm":      {envName: "NPM_CONFIG_CACHE", hostPath: []string{".npm"}, containerPath: "/tmp/wodby-cache/npm"},
	"composer": {envName: "COMPOSER_CACHE_DIR", hostPath: []string{".composer", "cache"}, containerPath: "/tmp/wodby-cache/composer"},
	"bundler":  {envName: "BUNDLE_USER_CACHE", hostPath: []string{".bundle", "cache"}, containerPath: "/tmp/wodby-cache/bundler"},
	"uv":       {envName: "UV_CACHE_DIR", hostPath: []string{".cache", "uv"}, containerPath: "/tmp/wodby-cache/uv"},
}

// resolveHostCacheStorage keeps native runs on conventional user cache paths.
// Data-container runs need a project-local staging root that the CI provider
// can persist and the CLI can import into the Docker-managed cache volume.
func resolveHostCacheStorage(context string, dataContainer bool) (string, string, error) {
	if dataContainer || os.Getenv("WODBY_CI_CACHE_DIR") != "" {
		root, err := cicache.HostRoot(context)
		return "", root, err
	}

	home, err := os.UserHomeDir()
	return home, "", err
}

func explicitEnvironmentNames(env []string, envFile string) (map[string]struct{}, error) {
	names := make(map[string]struct{}, len(env))
	for _, value := range env {
		name := strings.TrimSpace(strings.SplitN(value, "=", 2)[0])
		if name != "" {
			names[name] = struct{}{}
		}
	}

	if envFile == "" {
		return names, nil
	}

	file, err := os.Open(envFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if name != "" {
			names[name] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return names, nil
}

func withMappedUserHome(env []string, explicitEnv map[string]struct{}) []string {
	if _, ok := explicitEnv["HOME"]; ok {
		return env
	}

	return append(env, "HOME=/tmp")
}

func resolveRunCacheProfileNames(explicit []string, disabled bool, explicitUser, image string, labels map[string]string) ([]string, error) {
	return resolveCacheProfileNames(explicit, disabled, explicitUser == "", image, labels)
}

func cacheConfigurationIsExplicit(values []string) bool {
	names := splitCacheProfileNames(values)
	return len(names) > 1 || len(names) == 1 && names[0] != "auto" && names[0] != "none"
}

func warnAutomaticCache(stage string, err error) {
	fmt.Fprintf(os.Stderr, "WARNING: skipping automatic cache %s: %v\n", stage, err)
}

func handleCacheFailure(stage string, strict bool, err error) error {
	if err == nil {
		return nil
	}
	if strict {
		return err
	}
	warnAutomaticCache(stage, err)
	return nil
}

func resolveCacheProfileNames(explicit []string, disabled, autoAllowed bool, image string, labels map[string]string) ([]string, error) {
	names := splitCacheProfileNames(explicit)
	if disabled {
		if len(names) > 0 {
			return nil, errors.New("--cache and --no-cache cannot be used together")
		}
		return nil, nil
	}

	if len(names) > 0 {
		if len(names) == 1 && names[0] == "auto" {
			names = nil
		} else if len(names) == 1 && names[0] == "none" {
			return nil, nil
		} else {
			for _, name := range names {
				if name == "auto" || name == "none" {
					return nil, errors.Errorf("cache profile %q cannot be combined with other profiles", name)
				}
			}
			return validateCacheProfileNames(names)
		}
	}

	if !autoAllowed {
		return nil, nil
	}

	if value, ok := labels[cacheLabel]; ok {
		return validateCacheProfileNames(splitCacheProfileNames([]string{value}))
	}

	return knownImageCacheProfiles(image), nil
}

func splitCacheProfileNames(values []string) []string {
	seen := make(map[string]struct{})
	names := make([]string, 0, len(values))
	for _, value := range values {
		for _, name := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	return names
}

func validateCacheProfileNames(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	for _, name := range names {
		if _, ok := cacheProfiles[name]; !ok {
			return nil, errors.Errorf("unknown cache profile %q (supported: npm, composer, bundler, uv)", name)
		}
	}

	return names, nil
}

func knownImageCacheProfiles(image string) []string {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nil
	}

	repository := fmt.Sprintf("%s/%s", reference.Domain(named), reference.Path(named))
	switch repository {
	case "docker.io/library/node", "docker.io/wodby/node", "docker.io/wodby/drupal-node":
		return []string{"npm"}
	case "docker.io/library/composer",
		"docker.io/wodby/php",
		"docker.io/wodby/drupal-php",
		"docker.io/wodby/wordpress-php",
		"docker.io/wodby/php-apache",
		"docker.io/wodby/php-nginx",
		"docker.io/wodby/wordpress-composer":
		return []string{"composer"}
	case "docker.io/library/ruby", "docker.io/wodby/ruby":
		return []string{"bundler"}
	case "docker.io/wodby/python":
		return []string{"uv"}
	default:
		return nil
	}
}

func addCacheProfiles(config *docker.RunConfig, names []string, explicitEnv map[string]struct{}, hostHome, cacheRoot string, dataContainer bool, user string) ([]string, error) {
	active := make([]string, 0, len(names))
	for _, name := range names {
		profile := cacheProfiles[name]
		if _, ok := explicitEnv[profile.envName]; ok {
			continue
		}

		if volumeTargetsPath(config.Volumes, profile.containerPath) {
			config.Env = append(config.Env, fmt.Sprintf("%s=%s", profile.envName, profile.containerPath))
			continue
		}

		if !dataContainer {
			hostPath := filepath.Join(append([]string{hostHome}, profile.hostPath...)...)
			if cacheRoot != "" {
				hostPath = filepath.Join(cacheRoot, name)
			}
			if err := os.MkdirAll(hostPath, 0755); err != nil {
				return nil, errors.Wrapf(err, "failed to create %s cache directory", name)
			}
			if uid, gid, ok := numericIdentity(user); ok && ciuser.CanChownDirectories() {
				if err := os.Chown(hostPath, uid, gid); err != nil {
					return nil, errors.Wrapf(err, "failed to set %s cache directory ownership", name)
				}
			}
			config.Volumes = append(config.Volumes, fmt.Sprintf("%s:%s", hostPath, profile.containerPath))
		}
		config.Env = append(config.Env, fmt.Sprintf("%s=%s", profile.envName, profile.containerPath))
		active = append(active, name)
	}

	return active, nil
}

// prepareNativeCacheOwnership makes host cache directories writable by the
// image user without changing the checkout owner. This is needed on native CI
// runners where the Docker daemon can change bind-mount ownership but the
// unprivileged CLI process cannot.
func prepareNativeCacheOwnership(client *docker.Client, image string, names []string, hostHome, cacheRoot, ownership string) error {
	uid, _, numeric := numericIdentity(ownership)
	if numeric && uid == 0 {
		return nil
	}

	runConfig, args := nativeCacheOwnershipRun(image, names, hostHome, cacheRoot, ownership)
	if len(runConfig.Volumes) == 0 {
		return nil
	}

	return client.Run(args, runConfig)
}

func nativeCacheOwnershipRun(image string, names []string, hostHome, cacheRoot, ownership string) (docker.RunConfig, []string) {
	runConfig := docker.RunConfig{
		Image:           image,
		User:            "root",
		ClearEntrypoint: true,
	}
	args := []string{"chown", "-R", ownership}
	for _, name := range names {
		profile := cacheProfiles[name]
		hostPath := filepath.Join(append([]string{hostHome}, profile.hostPath...)...)
		if cacheRoot != "" {
			hostPath = filepath.Join(cacheRoot, name)
		}
		runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", hostPath, profile.containerPath))
		args = append(args, profile.containerPath)
	}

	return runConfig, args
}

func numericIdentity(user string) (int, int, bool) {
	parts := strings.SplitN(user, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	gid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return uid, gid, true
}

func volumeTargetsPath(volumes []string, target string) bool {
	for _, volume := range volumes {
		for _, field := range strings.Split(volume, ":") {
			if field == target {
				return true
			}
		}
	}

	return false
}
