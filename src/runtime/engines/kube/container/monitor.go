package container

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
)

// PostStartProcess is called in smaster after successful execution of container process.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("adding %s start timestamp file", e.containerName)
	err := kube.AddStartedFile(e.containerName)
	if err != nil {
		return fmt.Errorf("could not add starter timestamp file")
	}
	return nil
}

// MonitorContainer is responsible for waiting for container process.
func (e *EngineOperations) MonitorContainer(pid int) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring container %q", e.containerName)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals)
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
			if err := kube.AddFinishedFile(e.containerName); err != nil {
				return 0, fmt.Errorf("could not add finished timestamp file: %v", err)
			}
			err = kube.AddExitStatusFile(e.containerName, status)
			return status, err
		default:
			return 0, fmt.Errorf("interrupted by signal %s", s.String())
		}
	}
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// This method will NOT remove instance file since it is assumed to be done by CRI server.
func (e *EngineOperations) CleanupContainer() error {
	return nil
}
