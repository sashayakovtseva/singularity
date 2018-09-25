package container

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
)

// StartProcess starts container.
func (e *EngineOperations) StartProcess(masterConn net.Conn) error {
	sylog.Debugf("starting container %q", e.containerName)
	masterConn.Close()

	hostname, err := os.Hostname()
	sylog.Debugf("hostname: %s %v", hostname, err)

	wd, err := os.Getwd()
	sylog.Debugf("pwd: %s %v", wd, err)

	sylog.Debugf("uid=%d gid=%d euid=%d egid=%d", os.Getuid(), os.Getgid(), os.Geteuid(), os.Getegid())
	sylog.Debugf("pid=%d ppid=%d", os.Getpid(), os.Getppid())

	sylog.Debugf("envs=%s", os.Environ())

	resolv, err := ioutil.ReadFile("/etc/resolv.conf")
	sylog.Debugf("%s\n%v", resolv, err)

	ll("/")
	ll("/dev")
	ll("/tmp")
	ll("/proc")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals)
	for {
		s := <-signals
		sylog.Debugf("Received signal %s", s.String())
		switch s {
		case syscall.SIGCHLD:
			var status syscall.WaitStatus
			for {
				wpid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
				if wpid <= 0 || err != nil {
					break
				}
			}
		case syscall.SIGCONT:
		default:
			err := syscall.Kill(0, s.(syscall.Signal))
			if err != nil {
				return fmt.Errorf("could not kill self: %v", err)
			}
		}
	}
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
		switch s {
		case syscall.SIGCHLD:
			var status syscall.WaitStatus
			if wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); err != nil {
				return status, fmt.Errorf("error while waiting child: %s", err)
			} else if wpid != pid {
				continue
			}
			return status, nil
		default:
			return 0, fmt.Errorf("interrupted by signal %s", s.String())
		}
	}
}
