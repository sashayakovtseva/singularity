// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package podsandbox

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/rpc"
	"os"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/singularity/rpc/client"
	"k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// CreateContainer creates a pod. This method is called in the same namespaces as target pod and
// used for proper namespaces initialization.
func (e *EngineOperations) CreateContainer(_ int, rpcConn net.Conn) error {
	sylog.Debugf("setting up pod %q", e.podName)

	rpcOps := &client.RPC{
		Client: rpc.NewClient(rpcConn),
		Name:   e.CommonConfig.EngineName,
	}

	if e.podConfig.GetHostname() != "" {
		sylog.Debugf("setting hostname to %q", e.podConfig.GetHostname())
		if _, err := rpcOps.SetHostname(e.podConfig.GetHostname()); err != nil {
			sylog.Errorf("failed to set hostname to %q: %v", e.podConfig.GetHostname(), err)
		}
	}

	if e.security.GetNamespaceOptions().GetPid() == v1alpha2.NamespaceMode_POD {
		sylog.Debugf("mounting proc fs")
		_, err := rpcOps.Mount("proc", "/proc", "proc", syscall.MS_NOSUID|syscall.MS_NODEV, "")
		if err != nil {
			return fmt.Errorf("could not mount proc fs: %s", err)
		}
	}

	dns := e.podConfig.GetDnsConfig()
	if dns != nil {
		b := bytes.NewBuffer(nil)
		for _, s := range dns.Servers {
			fmt.Fprintf(b, "nameserver %s\n", s)
		}
		for _, s := range dns.Searches {
			fmt.Fprintf(b, "search %s\n", s)
		}
		for _, o := range dns.Options {
			fmt.Fprintf(b, "options %s\n", o)
		}
		temp, err := ioutil.TempFile("", "")
		if err != nil {
			return fmt.Errorf("could not create temp file: %v", err)
		}
		defer func() {
			sylog.Debugf("removing temp resolv.conf: %v", os.Remove(temp.Name()))
		}()
		ioutil.WriteFile(temp.Name(), b.Bytes(), 0644)
		sylog.Debugf("mounting resolv.conf file")
		_, err = rpcOps.Mount(temp.Name(), "/etc/resolv.conf", "", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_BIND, "")
		if err != nil {
			return fmt.Errorf("could not mount resolv.conf: %s", err)
		}
	}

	return nil
}
