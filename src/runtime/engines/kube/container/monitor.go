package container

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
)

// PostStartProcess is called in smaster after successful execution of container process.
// Since container is run as instance PostStartProcess creates instance file on host fs.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("container %q is running", e.containerName)
	return nil
}

// MonitorContainer is responsible for waiting for container process.
func (e *EngineOperations) MonitorContainer(pid int) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring container %q", e.containerName)
	defer func() {
		sylog.Debugf("container %q has exited", e.containerName)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals)

	for {
		s := <-signals
		sylog.Debugf("received signal: %v", s)
		switch s {
		case syscall.SIGCHLD:
			var status syscall.WaitStatus
			wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			sylog.Debugf("wait4 returned: %d %v", wpid, err)
			if err != nil {
				return status, fmt.Errorf("error while waiting child: %s", err)
			}
			if wpid == pid {
				return status, nil
			}
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
