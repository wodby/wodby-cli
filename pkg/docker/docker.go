package docker

import (
	"github.com/pkg/errors"

	"github.com/wodby/wodby-cli/pkg/exec"
	"fmt"
	"strings"
	"io"
	"os"
	"bytes"
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
		return errors.New(string(out))
	}

	return nil
}

// Build builds docker image.
func (c *Client) Build(dockerfile string, image string, context string) error {
	cmd := exec.Command("docker", "build", "-t", image, "-f", "-", context)
	cmd.Stdin = strings.NewReader(dockerfile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}

	return nil
}

// Push pushes docker image.
func (c *Client) Push(image string) error {
	out, err := exec.Command("docker", "push", image).CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}

	return nil
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

	// Show run command progress.
	cmd := exec.Command("docker", command...)

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
		return errors.New("failed to capture stdout or stderr\n")
	}

	return nil
}

// NewClient creates new docker client.
func NewClient() *Client {
	return &Client{}
}
