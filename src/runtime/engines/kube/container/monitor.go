package container

import (
	"fmt"
	"os"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
)

// PostStartProcess is called in smaster after successful execution of container process.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("adding %s start timestamp file", e.containerID)
	err := kube.AddStartedFile(e.containerID)
	if err != nil {
		return fmt.Errorf("could not add starter timestamp file: %v", err)
	}
	return nil
}

// MonitorContainer is responsible for waiting for container process.
func (e *EngineOperations) MonitorContainer(pid int, signals chan os.Signal) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring container %q", e.containerID)
	for {
		s := <-signals
		sylog.Debugf("received signal: %v", s)
		switch s {
		case syscall.SIGCHLD:
			var status syscall.WaitStatus
			wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			if err != nil {
				return 0, fmt.Errorf("error while waiting child: %s", err)
			}
			if wpid != pid {
				continue
			}
			if err := kube.AddFinishedFile(e.containerID); err != nil {
				return 0, fmt.Errorf("could not add finished timestamp file: %v", err)
			}
			err = kube.AddExitCodeFile(e.containerID, status)
			return status, err
		default:
			return 0, fmt.Errorf("interrupted by signal %s", s.String())
		}
	}
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// This method will NOT remove instance file since it is assumed to be done by CRI server.
func (e *EngineOperations) CleanupContainer() error {
	if e.conn != nil {
		sylog.Debugf("sending %v to socket", SigCleanup)
		_, err := e.conn.Write([]byte{SigCleanup})
		if err != nil {
			return fmt.Errorf("could not write to socket: %v", err)
		}
		if e.createError != nil {
			_, err := e.conn.Write([]byte(e.createError.Error()))
			if err != nil {
				return fmt.Errorf("could not write reason to pisocketpe: %v", err)
			}
		}
		if err := e.conn.Close(); err != nil {
			sylog.Errorf("could not close socket: %v", err)
		}
	}
	return nil
}
