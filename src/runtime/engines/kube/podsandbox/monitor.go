package podsandbox

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
)

// PostStartProcess is called in smaster after successful execution of container process.
// Since pod is run as instance PostStartProcess creates instance file on host fs.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("pod %q is running", e.podName)
	err := kube.AddInstanceFile(e.podName, "", pid, e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not add instance file: %v", err)
	}
	err = kube.AddStartedFile(e.podName)
	if err != nil {
		return fmt.Errorf("could not add started timestamp file: %v", err)
	}
	return nil
}

// MonitorContainer is responsible for waiting for pod process.
func (e *EngineOperations) MonitorContainer(pid int) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring pod %q", e.podName)
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
				return status, fmt.Errorf("error while waiting child: %s", err)
			}
			if wpid != pid {
				continue
			}
			return status, nil
		default:
			return 0, fmt.Errorf("interrupted by signal %s", s.String())
		}
	}
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// It is responsible for ensuring that the pod is indeed removed. Currently it
// only removes instance file from host fs.
func (e *EngineOperations) CleanupContainer() error {
	sylog.Debugf("removing instance file for pod %q", e.podName)
	return kube.CleanupInstance(e.podName)
}
