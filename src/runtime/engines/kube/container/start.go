package container

import (
	"fmt"
	"io"
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

	if e.config.FifoFD != 0 {
		sylog.Debugf("opening fifo fd %d to read byte", e.config.FifoFD)
		fifo, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", e.config.FifoFD), os.O_RDONLY, 0)
		if err != nil {
			return fmt.Errorf("could not reaopen fifo in a blocking mode: %v", err)
		}
		data := make([]byte, 1)
		sylog.Debugf("reading fifo...")
		_, err = fifo.Read(data)
		if err != nil && err != io.EOF {
			return fmt.Errorf("could not read fifo: %v", err)
		}
		sylog.Debugf("read %q from fifo", data)
		err = fifo.Close()
		if err != nil {
			return fmt.Errorf("could not close fifo: %v", err)
		}
	}
	sylog.Debugf("container %q has started", e.containerName)

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
	cmd.Dir = e.containerConfig.WorkingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if e.containerConfig.GetLogPath() != "" {
		logFileName := filepath.Base(e.containerConfig.GetLogPath())
		path := filepath.Join("/tmp/logs", logFileName)
		sylog.Debugf("redirecting io to %q", path)
		out, err := os.OpenFile(path, syscall.O_RDWR, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not open log file %q: %v", path, err)
		}
		defer out.Close()
		cmd.Stderr = out
		cmd.Stdout = out
	}

	errChan := make(chan error, 1)
	signals := make(chan os.Signal, 1)

	sylog.Debugf("starting container %q", e.containerName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec %v failed: %v", command, err)
	}
	go func() { errChan <- cmd.Wait() }()

	masterConn.Close()

	signal.Notify(signals)
	for {
		select {
		case s := <-signals:
			sylog.Debugf("received signal: %v", s)
			switch s {
			case syscall.SIGCONT:
			case syscall.SIGCHLD:
			case syscall.SIGTERM:
				sylog.Debugf("container %q was asked to terminate", e.containerName)
				cmd.Process.Signal(syscall.SIGTERM)
			default:
				sylog.Debugf("propagating signal to others")
				err := syscall.Kill(-1, s.(syscall.Signal))
				if err != nil {
					return fmt.Errorf("could not kill self: %v", err)
				}
			}

		case err := <-errChan:
			if err == nil {
				sylog.Debugf("container finished")
				os.Exit(0)
			}
			ee, ok := err.(*exec.ExitError)
			if !ok {
				os.Exit(255)
			}
			status, ok := ee.Sys().(syscall.WaitStatus)
			if !ok {
				os.Exit(255)
			}
			if status.Signaled() {
				sylog.Debugf("container was signaled to exit: %v", status.Signal())
			}
			sylog.Debugf("will exit with status: %v", status.ExitStatus())
			os.Exit(status.ExitStatus())
		}
	}
}
