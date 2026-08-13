package cicache

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
)

const (
	DirectoryName = ".wodby-ci-cache"
	ContainerRoot = "/tmp/wodby-cache"
)

// HostRoot returns a cache path that is visible anywhere the CI context is
// already bind-mountable. An explicit root remains available for CI-specific
// shared-path configurations.
func HostRoot(context string) (string, error) {
	root := os.Getenv("WODBY_CI_CACHE_DIR")
	if root == "" {
		return filepath.Join(context, DirectoryName), nil
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root), nil
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve WODBY_CI_CACHE_DIR")
	}
	return absolute, nil
}

// ImportDataContainer restores persistent CI caches into the cache volume and
// removes the host-side cache copy from the code volume.
func ImportDataContainer(container, workingDir, context string) error {
	cacheRoot, err := HostRoot(context)
	if err != nil {
		return err
	}

	if err := runDataContainerUtility(container, "rm", "-rf", path.Join(workingDir, DirectoryName)); err != nil {
		return err
	}
	if err := runDataContainerUtility(container, "mkdir", "-p", ContainerRoot); err != nil {
		return err
	}

	info, err := os.Stat(cacheRoot)
	if err == nil {
		if !info.IsDir() {
			return errors.Errorf("Wodby CI cache root %s is not a directory", cacheRoot)
		}
		output, copyErr := exec.Command(
			"docker",
			"cp",
			cacheRoot+string(os.PathSeparator)+".",
			fmt.Sprintf("%s:%s", container, ContainerRoot),
		).CombinedOutput()
		if copyErr != nil {
			return errors.Wrap(copyErr, string(output))
		}
	} else if !os.IsNotExist(err) {
		return errors.WithStack(err)
	}

	return runDataContainerUtility(container, "chmod", "-R", "0777", ContainerRoot)
}

// PrepareDataContainerProfiles ensures a cache-enabled command can write its
// profile directories even when the selected image uses a different user.
func PrepareDataContainerProfiles(container string, profiles []string) error {
	args := []string{"mkdir", "-p"}
	for _, profile := range profiles {
		if profile == "" || filepath.Base(profile) != profile {
			return errors.Errorf("invalid cache profile path %q", profile)
		}
		args = append(args, path.Join(ContainerRoot, profile))
	}
	if len(args) == 2 {
		return nil
	}
	if err := runDataContainerUtility(container, args...); err != nil {
		return err
	}
	return runDataContainerUtility(container, "chmod", "-R", "0777", ContainerRoot)
}

// ExportDataContainerProfiles copies cache contents back to the CI workspace
// so the CI provider can persist them between ephemeral DinD jobs.
func ExportDataContainerProfiles(container, context string, profiles []string) error {
	cacheRoot, err := HostRoot(context)
	if err != nil {
		return err
	}

	for _, profile := range profiles {
		if profile == "" || filepath.Base(profile) != profile {
			return errors.Errorf("invalid cache profile path %q", profile)
		}
		destination := filepath.Join(cacheRoot, profile)
		if err := os.MkdirAll(destination, 0755); err != nil {
			return errors.Wrapf(err, "failed to create %s cache directory", profile)
		}
		output, copyErr := exec.Command(
			"docker",
			"cp",
			fmt.Sprintf("%s:%s/.", container, path.Join(ContainerRoot, profile)),
			destination,
		).CombinedOutput()
		if copyErr != nil {
			return errors.Wrap(copyErr, string(output))
		}
	}

	return nil
}

func runDataContainerUtility(container string, args ...string) error {
	command := []string{
		"run",
		"--rm",
		fmt.Sprintf("--volumes-from=%s", container),
		"--user=root",
		"--entrypoint=",
		"alpine",
	}
	command = append(command, args...)
	output, err := exec.Command("docker", command...).CombinedOutput()
	if err != nil {
		return errors.Wrap(err, string(output))
	}
	return nil
}
