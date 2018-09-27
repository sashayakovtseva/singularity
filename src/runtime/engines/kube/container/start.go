package container

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sylabs/singularity/src/pkg/sylog"
)

// StartProcess starts container.
func (e *EngineOperations) StartProcess(masterConn net.Conn) error {
	masterConn.Close()
	pid := os.Getpid()
	sylog.Debugf("starting container %q", e.containerName)

	hostname, err := os.Hostname()
	sylog.Debugf("hostname: %s %v", hostname, err)

	wd, err := os.Getwd()
	sylog.Debugf("pwd: %s %v", wd, err)

	sylog.Debugf("uid=%d gid=%d euid=%d egid=%d", os.Getuid(), os.Getgid(), os.Geteuid(), os.Getegid())
	sylog.Debugf("pid=%d ppid=%d", pid, os.Getppid())

	sylog.Debugf("envs=%s", os.Environ())

	resolv, err := ioutil.ReadFile("/etc/resolv.conf")
	sylog.Debugf("%s\n%v", resolv, err)

	ll("/")
	ll("/proc")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals)
	for {
		select {
		case s := <-signals:
			sylog.Debugf("received signal: %v", s)
			switch s {
			case syscall.SIGCONT:
			case syscall.SIGTERM:
				sylog.Debugf("container %q was asked to terminate", e.containerName)
				os.Exit(0)
			default:
				err := syscall.Kill(0, s.(syscall.Signal))
				if err != nil {
					return fmt.Errorf("could not kill self: %v", err)
				}
			}
		default:
			sylog.Debugf("this is container %q running", e.containerName)
			time.Sleep(time.Second * 5)
		}
	}
}
