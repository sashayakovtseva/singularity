// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package podsandbox

import (
	"fmt"
	"net"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/config"
	"github.com/sylabs/singularity/src/runtime/engines/config/starter"
	"k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// Name of the engine.
const Name = "kube_podsandbox"

// EngineOperations implements the engines.EngineOperations interface for the pod management process.
type EngineOperations struct {
	CommonConfig *config.Common
	podConfig    *v1alpha2.PodSandboxConfig
	// name of the pod constructed from metadata. Instance file will be written under this name.
	podName  string
	security *v1alpha2.LinuxSandboxSecurityContext
}

// InitConfig simply saves passed config into engine. Passed cfg already includes parsed PodSandboxConfig.
func (e *EngineOperations) InitConfig(cfg *config.Common) {
	sylog.Debugf("%+v", cfg)
	e.CommonConfig = cfg
	e.podConfig = cfg.EngineConfig.(*v1alpha2.PodSandboxConfig)
	meta := e.podConfig.Metadata // assume metadata is always non-nil
	e.podName = fmt.Sprintf("%s_%s_%s_%d", meta.Name, meta.Namespace, meta.Uid, meta.Attempt)
	if e.podConfig.Linux != nil {
		e.security = e.podConfig.Linux.SecurityContext
	}
}

// Config returns empty PodSandboxConfig that will be filled later with received JSON data.
func (e *EngineOperations) Config() interface{} {
	sylog.Debugf("will return zeroed %T", v1alpha2.PodSandboxConfig{})
	return new(v1alpha2.PodSandboxConfig)
}

// PrepareConfig is called in stage1 to validate and prepare container configuration.
func (e *EngineOperations) PrepareConfig(_ net.Conn, conf *starter.Config) error {
	sylog.Debugf("preparing config for pod %q", e.podName)
	conf.SetInstance(true)

	var namespaces []specs.LinuxNamespace
	sylog.Debugf("requesting Mount namespace")
	namespaces = append(namespaces, specs.LinuxNamespace{
		Type: specs.MountNamespace,
	})

	if e.podConfig.Hostname != "" {
		sylog.Debugf("requesting UTS namespace")
		namespaces = append(namespaces, specs.LinuxNamespace{
			Type: specs.UTSNamespace,
		})
	}

	if e.security != nil {
		conf.SetNoNewPrivs(!e.security.Privileged)
		if e.security.NamespaceOptions != nil {
			if e.security.NamespaceOptions.Network == v1alpha2.NamespaceMode_POD {
				sylog.Debugf("requesting NET namespace")
				namespaces = append(namespaces, specs.LinuxNamespace{
					Type: specs.NetworkNamespace,
				})
			}
			if e.security.NamespaceOptions.Pid == v1alpha2.NamespaceMode_POD {
				sylog.Debugf("requesting PID namespace")
				namespaces = append(namespaces, specs.LinuxNamespace{
					Type: specs.PIDNamespace,
				})
			}
			if e.security.NamespaceOptions.Ipc == v1alpha2.NamespaceMode_POD {
				sylog.Debugf("requesting IPC namespace")
				namespaces = append(namespaces, specs.LinuxNamespace{
					Type: specs.IPCNamespace,
				})
			}
		}
	}
	// todo RunAsUser
	// todo request UserNamespace?
	conf.SetNsFlagsFromSpec(namespaces)
	return nil
}
