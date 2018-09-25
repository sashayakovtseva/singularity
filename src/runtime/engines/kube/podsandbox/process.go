// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package podsandbox

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
)

// StartProcess starts pod.
func (e *EngineOperations) StartProcess(masterConn net.Conn) error {
	sylog.Debugf("starting pod %q", e.podName)
	masterConn.Close()

	hostname, err := os.Hostname()
	sylog.Debugf("hostname: %s %v", hostname, err)

	wd, err := os.Getwd()
	sylog.Debugf("pwd: %s %v", wd, err)

	sylog.Debugf("uid=%d gid=%d euid=%d egid=%d", os.Getuid(), os.Getgid(), os.Geteuid(), os.Getegid())
	sylog.Debugf("pid=%d ppid=%d", os.Getpid(), os.Getppid())

	resolv, err := ioutil.ReadFile("/etc/resolv.conf")
	sylog.Debugf("%s\n%v", resolv, err)

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

// MonitorContainer is responsible for waiting for pod process.
func (e *EngineOperations) MonitorContainer(pid int) (syscall.WaitStatus, error) {
	sylog.Debugf("monitoring pod %q", e.podName)
	defer func() {
		sylog.Debugf("pod %q has exited", e.podName)
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
