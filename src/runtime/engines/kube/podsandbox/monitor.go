package podsandbox

import (
	"fmt"
	"os"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
)

// PostStartProcess is called in smaster after successful execution of container process.
// Since pod is run as instance PostStartProcess creates instance file on host fs.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("pod %q is running", e.podID)
	err := kube.AddStartedFile(e.podID)
	if err != nil {
		return fmt.Errorf("could not add started timestamp file: %v", err)
	}
	return nil
}

// MonitorContainer is responsible for waiting for pod process.
func (e *EngineOperations) MonitorContainer(pid int, signals chan os.Signal) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring pod %q", e.podID)
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
			if err := kube.AddFinishedFile(e.podID); err != nil {
				return 0, fmt.Errorf("could not add finished timestamp file: %v", err)
			}
			err = kube.AddExitCodeFile(e.podID, status)
			return status, err
		default:
			return 0, fmt.Errorf("interrupted by signal %s", s.String())
		}
	}
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// It is responsible for ensuring that the pod is indeed removed. Currently it
// only removes instance file from host fs.
func (e *EngineOperations) CleanupContainer() error {
	return nil
}
