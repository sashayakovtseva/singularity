package container

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"encoding/json"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// StartProcess starts container.
func (e *EngineOperations) StartProcess(masterConn net.Conn) error {
	const (
		runScript  = "/.singularity.d/runscript"
		execScript = "/.singularity.d/actions/exec"
	)

	if e.isExecSync {
		return e.execSync(masterConn)
	}

	if e.config.Socket != 0 {
		comm := os.NewFile(uintptr(e.config.Socket), "")
		socket, err := net.FileConn(comm)
		if err != nil {
			return fmt.Errorf("could not create socket connection: %v", err)
		}
		if err := comm.Close(); err != nil {
			sylog.Errorf("could not close socket file: %v", err)
		}

		data := make([]byte, 1)
		_, err = socket.Read(data)
		if err != nil {
			return fmt.Errorf("could not read socket: %v", err)
		}
		if err = socket.Close(); err != nil {
			return fmt.Errorf("could not close socket connection: %v", err)
		}
	}
	sylog.Debugf("container %q has started", e.containerID)

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
	workDir := e.containerConfig.WorkingDir
	if workDir == "" {
		workDir = "/"
	}

	cmd := exec.Command(execScript, command...)
	cmd.Dir = workDir
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

		cmd.Stderr = &rfc3339NanoWriter{Writer: out, stream: k8s.Stderr}
		cmd.Stdout = &rfc3339NanoWriter{Writer: out, stream: k8s.Stdout}
	}

	errChan := make(chan error, 1)
	signals := make(chan os.Signal, 1)

	sylog.Debugf("starting container %q", e.containerID)
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
				sylog.Debugf("container %q was asked to terminate", e.containerID)
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

func (e *EngineOperations) execSync(masterConn net.Conn) error {
	const (
		execScript = "/.singularity.d/actions/exec"
	)

	contInst, err := instance.Get(e.containerID)
	if err != nil {
		return fmt.Errorf("could not get container instance %q: %v", e.containerID, err)
	}

	contConfig := new(Config)
	if err := json.Unmarshal(contInst.Config, contConfig); err != nil {
		return fmt.Errorf("could not unmarshal container config: %v", err)
	}

	contProcPath := fmt.Sprintf(`/proc/%d`, contInst.Pid)

	for _, kv := range contConfig.CreateContainerRequest.GetConfig().GetEnvs() {
		err := os.Setenv(kv.Key, kv.Value)
		if err != nil {
			return fmt.Errorf("could not set environment: %v", err)
		}
	}

	command := e.config.ExecSyncRequest.Cmd
	if len(command) == 0 {
		return fmt.Errorf("no command to execute")
	}

	cmd := exec.Command(execScript, command...)
	cmd.Dir = filepath.Join(contProcPath, "cwd")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot: filepath.Join(contProcPath, "root"),
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec sync %v failed: %v", command, err)
	}
	masterConn.Close()
	return nil
}
