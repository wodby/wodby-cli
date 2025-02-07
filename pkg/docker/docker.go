package docker

import (
	"github.com/pkg/errors"

	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wodby/wodby-cli/pkg/exec"
)

// Client is docker client representation.
type Client struct{}

type RunConfig struct {
	Image       string
	Volumes     []string
	VolumesFrom []string
	Env         []string
	EnvFile     string
	User        string
	WorkDir     string
	Entrypoint  string
}

// Login authorizes in the registry.
func (c *Client) Login(host string, username string, password string) error {
	command := fmt.Sprintf("echo %s | docker login -u %s --password-stdin %s", password, username, host)
	out, err := exec.Command("bash", "-c", command).CombinedOutput()
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

	args = append(args, "-f", dockerfile, context)

	if len(buildArgs) != 0 {
		for name, value := range buildArgs {
			if value == "" {
				args = append(args, "--build-arg", name)
			} else {
				args = append(args, "--build-arg", fmt.Sprintf("%s=%s", name, value))
			}
		}
	}

	fmt.Printf("Building:\n docker %s\n", strings.Join(args, " "))
	cmd := exec.Command("docker", args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "DOCKER_BUILDKIT=1")

	return cmdStartVerbose(cmd)
}

func (c *Client) Push(image string) error {
	fmt.Printf("Pushing:\n docker push %s\n", image)
	cmd := exec.Command("docker", "push", image)

	return cmdStartVerbose(cmd)
}

func (c *Client) Pull(image string) error {
	fmt.Printf("Pulling:\n docker pull %s\n", image)
	cmd := exec.Command("docker", "pull", "-q", image)

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
	if config.EnvFile != "" {
		command = append(command, fmt.Sprintf("--env-file=%s", config.EnvFile))
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

type CustomError struct {
	Msg      string
	StdOut   string
	StdErr   string
	Original error
}

func (e *CustomError) Error() string {
	return fmt.Sprintf("Docker push failed: %s\nStdout: %s\nStderr: %s\nOriginal error: %v", e.Msg, e.StdOut, e.StdErr, e.Original)
}

func cmdStartVerbose(cmd *exec.Cmd) error {
	var stdoutBuf, stderrBuf bytes.Buffer

	stdoutIn, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithStack(err)
	}

	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return errors.WithStack(err)
	}

	stdout := io.MultiWriter(os.Stdout, &stdoutBuf)
	stderr := io.MultiWriter(os.Stderr, &stderrBuf)

	err = cmd.Start()
	if err != nil {
		return &CustomError{
			Msg:      "Failed to start docker push command",
			Original: err,
		}
	}

	var errStdout, errStderr error
	var done = make(chan struct{})

	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
		done <- struct{}{}
	}()

	go func() {
		_, errStderr = io.Copy(stderr, stderrIn)
		done <- struct{}{}
	}()

	// Wait for both goroutines to complete
	for i := 0; i < 2; i++ {
		<-done
	}

	err = cmd.Wait()
	if err != nil {
		return &CustomError{
			Msg:      "Docker push command failed",
			StdOut:   stdoutBuf.String(),
			StdErr:   stderrBuf.String(),
			Original: err,
		}
	}

	if errStdout != nil || errStderr != nil {
		return &CustomError{
			Msg:      "Failed to capture stdout or stderr",
			StdOut:   stdoutBuf.String(),
			StdErr:   stderrBuf.String(),
			Original: errors.New("IO copy error"),
		}
	}

	return nil
}
