package run

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/docker"
)

const cacheLabel = "com.wodby.ci.cache"

type cacheProfile struct {
	envName       string
	hostPath      []string
	containerPath string
}

var cacheProfiles = map[string]cacheProfile{
	"npm": {
		envName:       "NPM_CONFIG_CACHE",
		hostPath:      []string{".npm"},
		containerPath: "/tmp/wodby-cache/npm",
	},
	"composer": {
		envName:       "COMPOSER_CACHE_DIR",
		hostPath:      []string{".composer", "cache"},
		containerPath: "/tmp/wodby-cache/composer",
	},
	"uv": {
		envName:       "UV_CACHE_DIR",
		hostPath:      []string{".cache", "uv"},
		containerPath: "/tmp/wodby-cache/uv",
	},
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
			return nil, errors.Errorf("unknown cache profile %q (supported: npm, composer, uv)", name)
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
	case "docker.io/library/node", "docker.io/wodby/node":
		return []string{"npm"}
	case "docker.io/library/composer", "docker.io/wodby/php":
		return []string{"composer"}
	case "docker.io/wodby/python":
		return []string{"uv"}
	default:
		return nil
	}
}

func addCacheProfiles(config *docker.RunConfig, names []string, explicitEnv map[string]struct{}, hostHome, cacheRoot string) error {
	if cacheRoot != "" && !filepath.IsAbs(cacheRoot) {
		absoluteRoot, err := filepath.Abs(cacheRoot)
		if err != nil {
			return errors.Wrap(err, "failed to resolve WODBY_CI_CACHE_DIR")
		}
		cacheRoot = absoluteRoot
	}

	for _, name := range names {
		profile := cacheProfiles[name]
		if _, ok := explicitEnv[profile.envName]; ok {
			continue
		}

		hostPath := filepath.Join(append([]string{hostHome}, profile.hostPath...)...)
		if cacheRoot != "" {
			hostPath = filepath.Join(cacheRoot, name)
		}
		if !volumeTargetsPath(config.Volumes, profile.containerPath) {
			if err := os.MkdirAll(hostPath, 0755); err != nil {
				return errors.Wrapf(err, "failed to create %s cache directory", name)
			}
			config.Volumes = append(config.Volumes, fmt.Sprintf("%s:%s", hostPath, profile.containerPath))
		}
		config.Env = append(config.Env, fmt.Sprintf("%s=%s", profile.envName, profile.containerPath))
	}

	return nil
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
