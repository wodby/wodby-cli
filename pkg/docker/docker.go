package docker

import (
	"github.com/pkg/errors"

	"bytes"
	"fmt"
	"github.com/wodby/wodby-cli/pkg/exec"
	"io"
	"os"
	"strings"
)

// Client is docker client representation.
type Client struct{}

type RunConfig struct {
	Image       string
	Volumes     []string
	VolumesFrom []string
	Env         []string
	User        string
	WorkDir     string
	Entrypoint  string
}

// Login authorizes in the registry.
func (c *Client) Login(host string, username string, password string) error {
	out, err := exec.Command("docker", "login", "-u", username, "-p", password, host).CombinedOutput()
	if err != nil {
		return errors.Wrap(err, string(out))
	}

	return nil
}

// Build builds docker image.
func (c *Client) Build(dockerfile string, tags []string, context string, buildArgs map[string]string) error {
	args := []string{"build"}

	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	args = append(args, "-f", "-", context)

	if len(buildArgs) != 0 {
		for name, value := range buildArgs {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%s", name, value))
		}
	}

	fmt.Printf("Building:\n docker %s\n", strings.Join(args, " "))

	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(dockerfile)

	return cmdStartVerbose(cmd)
}

func (c *Client) Push(image string) error {
	fmt.Printf("Pushing:\n docker push %s\n", image)
	cmd := exec.Command("docker", "push", image)

	return cmdStartVerbose(cmd)
}

func (c *Client) Pull(image string) error {
	fmt.Printf("Pulling:\n docker pull %s\n", image)
	cmd := exec.Command("docker", "pull", image)

	return cmdStartVerbose(cmd)
}

func (c *Client) Tag(image string, tag string) error {
	fmt.Printf("Tagging:\n docker tag %s %s\n", image, tag)
	cmd := exec.Command("docker", "tag", image, tag)

	return cmdStartVerbose(cmd)
}

func (c *Client) GetImageDefaultUser(image string) (string, error) {
	defaultUser := ""

	err := c.Pull(image)

	if err != nil {
		return defaultUser, err
	}

	out, err := exec.Command("docker", "image", "inspect", image, "-f", "{{.Config.User}}").CombinedOutput()
	if err != nil {
		return defaultUser, errors.Wrap(err, string(out))
	}

	defaultUser = strings.TrimSuffix(string(out), "\n")

	if defaultUser == "" {
		defaultUser = "root"
	}

	return defaultUser, nil
}

func (c *Client) GetImageWorkingDir(image string) (string, error) {
	workingDir := ""

	err := c.Pull(image)

	if err != nil {
		return workingDir, err
	}

	out, err := exec.Command("docker", "image", "inspect", image, "-f", "{{.Config.WorkingDir}}").CombinedOutput()
	if err != nil {
		return workingDir, errors.Wrap(err, string(out))
	}

	workingDir = strings.TrimSuffix(string(out), "\n")

	if workingDir == "" {
		workingDir = "/"
	}

	return workingDir, nil
}

// Run runs docker container.
func (c *Client) Run(args []string, config RunConfig) error {
	command := []string{"run", "--rm"}
	for _, volumesFrom := range config.VolumesFrom {
		command = append(command, fmt.Sprintf("--volumes-from=%s", volumesFrom))
	}
	for _, volume := range config.Volumes {
		command = append(command, fmt.Sprintf("--volume=%s", volume))
	}
	for _, env := range config.Env {
		command = append(command, fmt.Sprintf("--env=%s", env))
	}
	if config.User != "" {
		command = append(command, fmt.Sprintf("--user=%s", config.User))
	}
	if config.WorkDir != "" {
		command = append(command, fmt.Sprintf("--workdir=%s", config.WorkDir))
	}
	if config.Entrypoint != "" {
		command = append(command, fmt.Sprintf("--entrypoint=%s", config.Entrypoint))
	}
	command = append(append(command, config.Image), args...)

	fmt.Printf("Running:\n docker %s\n", strings.Join(command, " "))

	// Show run command progress.
	cmd := exec.Command("docker", command...)

	return cmdStartVerbose(cmd)
}

// NewClient creates new docker client.
func NewClient() *Client {
	return &Client{}
}

func cmdStartVerbose(cmd *exec.Cmd) error {
	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()

	var errStdout, errStderr error
	stdout := io.MultiWriter(os.Stdout, &stdoutBuf)
	stderr := io.MultiWriter(os.Stderr, &stderrBuf)

	err := cmd.Start()

	if err != nil {
		return err
	}

	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
	}()

	go func() {
		_, errStderr = io.Copy(stderr, stderrIn)
	}()

	err = cmd.Wait()
	if err != nil {
		return err
	}
	if errStdout != nil || errStderr != nil {
		//return errors.New("failed to capture stdout or stderr\n")
	}

	return nil
}
