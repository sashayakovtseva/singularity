// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package podsandbox

import (
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

	hostnameF, err := ioutil.ReadFile("/etc/hostname")
	sylog.Debugf("%s\n%v", hostnameF, err)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals)
	for {
		s := <-signals
		sylog.Debugf("received signal: %v", s)
		switch s {
		case syscall.SIGTERM:
			sylog.Debugf("pod %q was asked to terminate", e.podName)
			os.Exit(0)
		}
	}
}
