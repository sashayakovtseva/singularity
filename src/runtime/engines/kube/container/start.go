package container

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
)

// StartProcess starts container.
func (e *EngineOperations) StartProcess(masterConn net.Conn) error {
	const (
		runScript  = "/.singularity.d/runscript"
		execScript = "/.singularity.d/actions/exec"
	)

	if e.containerConfig.WorkingDir != "" {
		sylog.Debugf("changing working directory to %q", e.containerConfig.WorkingDir)
		err := os.Chdir(e.containerConfig.WorkingDir)
		if err != nil {
			return fmt.Errorf("could not set working directory: %v", err)
		}
	}

	for _, kv := range e.containerConfig.GetEnvs() {
		err := os.Setenv(kv.Key, kv.Value)
		if err != nil {
			return fmt.Errorf("could not set environment: %v", err)
		}
	}

	command := append(e.containerConfig.GetCommand(), e.containerConfig.GetArgs()...)
	if len(command) == 0 {
		command = []string{runScript}
	}

	cmd := exec.Command(execScript, command...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if e.containerConfig.GetLogPath() != "" {
		logFileName := filepath.Base(e.containerConfig.GetLogPath())
		path := filepath.Join("/tmp/logs", logFileName)
		sylog.Debugf("redirecting io to %q", path)
		io, err := os.OpenFile(path, syscall.O_RDWR, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not open log file %q: %v", path, err)
		}
		defer io.Close()
		cmd.Stderr = io
		cmd.Stdout = io
	}

	errChan := make(chan error, 1)
	signals := make(chan os.Signal, 1)

	sylog.Debugf("starting container %q", e.containerName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec %v failed: %v", command, err)
	}

	go func() {
		errChan <- cmd.Wait()
	}()

	masterConn.Close()

	signal.Notify(signals)
	for {
		select {
		case s := <-signals:
			sylog.Debugf("received signal: %v", s)
			switch s {
			case syscall.SIGCONT:
			case syscall.SIGCHLD:
				var status syscall.WaitStatus
				for {
					wpid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
					if wpid <= 0 || err != nil {
						break
					}
				}
			default:
				err := syscall.Kill(-1, s.(syscall.Signal))
				if err != nil {
					return fmt.Errorf("could not kill self: %v", err)
				}
			}
		case err := <-errChan:
			if e, ok := err.(*exec.ExitError); ok {
				if status, ok := e.Sys().(syscall.WaitStatus); ok {
					if status.Signaled() {
						syscall.Kill(syscall.Gettid(), syscall.SIGKILL)
					}
					os.Exit(status.ExitStatus())
				}
				return fmt.Errorf("command exit with error: %s", err)
			}
			os.Exit(0)
		}
	}
}