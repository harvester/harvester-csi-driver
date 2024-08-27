package command

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
)

var ErrCmdTimeout = errors.New("command timeout")

const (
	NSBinary          = "nsenter"
	cmdTimeoutDefault = 180 * time.Second // 3 minutes by default
	cmdTimeoutNone    = 0 * time.Second   // no timeout
)

type Executor struct {
	namespace  string
	cmdTimeout time.Duration
}

func NewExecutor() *Executor {
	return &Executor{
		namespace:  "",
		cmdTimeout: cmdTimeoutDefault,
	}
}

func NewExecutorWithNS(ns string) (*Executor, error) {
	exec := NewExecutor()
	exec.namespace = ns

	// test if nsenter is available
	if _, err := execute(NSBinary, []string{"-V"}, cmdTimeoutNone); err != nil {
		return nil, errors.Wrap(err, "cannot find nsenter for namespace switching")
	}
	return exec, nil
}

func (exec *Executor) SetTimeout(timeout time.Duration) {
	exec.cmdTimeout = timeout
}

func (exec *Executor) Execute(cmd string, args []string) (string, error) {
	command := cmd
	cmdArgs := args
	if exec.namespace != "" {
		cmdArgs = []string{
			"--mount=" + filepath.Join(exec.namespace, "mnt"),
			"--net=" + filepath.Join(exec.namespace, "net"),
			"--ipc=" + filepath.Join(exec.namespace, "ipc"),
			cmd,
		}
		command = NSBinary
		cmdArgs = append(cmdArgs, args...)
	}
	return execute(command, cmdArgs, exec.cmdTimeout)
}

func execute(command string, args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(command, args...)

	var output, stderr bytes.Buffer
	cmdTimeout := false
	cmd.Stdout = &output
	cmd.Stderr = &stderr

	timer := time.NewTimer(cmdTimeoutNone)
	if timeout != cmdTimeoutNone {
		// add timer to kill the process if timeout
		timer = time.AfterFunc(timeout, func() {
			cmdTimeout = true
			cmd.Process.Kill()
		})
	}
	defer timer.Stop()

	if err := cmd.Run(); err != nil {
		if cmdTimeout {
			return "", errors.Wrapf(ErrCmdTimeout, "timeout after %v: %v %v", timeout, command, args)
		}
		return "", errors.Wrapf(err, "failed to execute: %v %v, output %s, stderr %s",
			command, args, output.String(), stderr.String())
	}

	return output.String(), nil
}
