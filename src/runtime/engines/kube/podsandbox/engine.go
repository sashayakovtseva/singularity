// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package podsandbox

import (
	"fmt"
	"net"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/config"
	"github.com/sylabs/singularity/src/runtime/engines/config/starter"
	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// Name of the engine.
const Name = "kube_podsandbox"

// EngineOperations implements the engines.EngineOperations interface for the pod management process.
type EngineOperations struct {
	CommonConfig *config.Common
	podConfig    *k8s.PodSandboxConfig
	// id of the pod constructed from metadata. Instance file will be written under this name.
	podID    string
	security *k8s.LinuxSandboxSecurityContext
}

// InitConfig simply saves passed config into engine. Passed cfg already includes parsed PodSandboxConfig.
func (e *EngineOperations) InitConfig(cfg *config.Common) {
	e.CommonConfig = cfg
	e.podConfig = cfg.EngineConfig.(*k8s.PodSandboxConfig)
	meta := e.podConfig.GetMetadata()
	e.podID = fmt.Sprintf("%s_%s_%s_%d", meta.GetName(), meta.GetNamespace(), meta.GetUid(), meta.GetAttempt())
	e.security = e.podConfig.GetLinux().GetSecurityContext()

}

// Config returns empty PodSandboxConfig that will be filled later with received JSON data.
func (e *EngineOperations) Config() interface{} {
	return new(k8s.PodSandboxConfig)
}

// PrepareConfig is called in stage1 to validate and prepare container configuration.
// This method figures out which namespaces are necessary for pod and requests them
// from C part of starter setting conf fields appropriately.
func (e *EngineOperations) PrepareConfig(_ net.Conn, conf *starter.Config) error {
	sylog.Debugf("preparing config for pod %q", e.podID)
	conf.SetInstance(true)

	var namespaces []specs.LinuxNamespace
	sylog.Debugf("requesting Mount namespace")
	namespaces = append(namespaces, specs.LinuxNamespace{
		Type: specs.MountNamespace,
	})

	if e.podConfig.GetHostname() != "" {
		sylog.Debugf("requesting UTS namespace")
		namespaces = append(namespaces, specs.LinuxNamespace{
			Type: specs.UTSNamespace,
		})
	}

	if e.security.GetNamespaceOptions().GetNetwork() == k8s.NamespaceMode_POD {
		sylog.Debugf("requesting NET namespace")
		namespaces = append(namespaces, specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
		})
	}
	if e.security.GetNamespaceOptions().GetPid() == k8s.NamespaceMode_POD {
		sylog.Debugf("requesting PID namespace")
		namespaces = append(namespaces, specs.LinuxNamespace{
			Type: specs.PIDNamespace,
		})
	}
	if e.security.GetNamespaceOptions().GetIpc() == k8s.NamespaceMode_POD {
		sylog.Debugf("requesting IPC namespace")
		namespaces = append(namespaces, specs.LinuxNamespace{
			Type: specs.IPCNamespace,
		})
	}
	conf.SetNoNewPrivs(!e.security.GetPrivileged())
	conf.SetNsFlagsFromSpec(namespaces)

	if e.podConfig.GetLogDirectory() != "" {
		sylog.Debugf("creating log directory")
		err := os.MkdirAll(e.podConfig.GetLogDirectory(), os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create log directory for pod %q", e.podID)
		}
	}

	return nil
}
