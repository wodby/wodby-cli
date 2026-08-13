package cidata

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/exec"
)

const utilityImage = "alpine"

// FixOwnership changes code ownership in a Docker-managed data volume without
// relying on utilities being present in the application image.
func FixOwnership(container, workingDir, ownership string) error {
	return RunUtility(container, "chown", "-R", ownership, workingDir)
}

// RunUtility executes an Alpine utility against all volumes from a data
// container. The utility image is pulled by ci init before the data container
// is created.
func RunUtility(container string, args ...string) error {
	cmd := utilityCommand(container, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrap(err, string(output))
	}
	return nil
}

func utilityCommand(container string, args ...string) *exec.Cmd {
	command := []string{
		"run",
		"--rm",
		fmt.Sprintf("--volumes-from=%s", container),
		"--user=root",
		"--entrypoint=",
		utilityImage,
	}
	command = append(command, args...)
	return exec.Command("docker", command...)
}
