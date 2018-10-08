package container

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/config"
	"github.com/sylabs/singularity/src/runtime/engines/config/starter"
	"k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// Name of the engine.
const Name = "kube_container"

// EngineOperations implements the engines.EngineOperations interface for the pod management process.
type EngineOperations struct {
	CommonConfig           *config.Common
	createContainerRequest *v1alpha2.CreateContainerRequest
	containerName          string
	security               *v1alpha2.LinuxContainerSecurityContext
	containerConfig        *v1alpha2.ContainerConfig
	podConfig              *v1alpha2.PodSandboxConfig
}

// InitConfig simply saves passed config into engine. Passed cfg already includes parsed ContainerConfig.
func (e *EngineOperations) InitConfig(cfg *config.Common) {
	sylog.Debugf("%+v", cfg)
	e.CommonConfig = cfg
	e.createContainerRequest = cfg.EngineConfig.(*v1alpha2.CreateContainerRequest)
	e.containerConfig = e.createContainerRequest.GetConfig()
	meta := e.containerConfig.GetMetadata()
	e.containerName = fmt.Sprintf("%s_%s_%d", e.createContainerRequest.GetPodSandboxId(), meta.GetName(), meta.GetAttempt())
	e.podConfig = e.createContainerRequest.GetSandboxConfig()
	e.security = e.containerConfig.GetLinux().GetSecurityContext()
}

// Config returns empty CreateContainerRequest that will be filled later with received JSON data.
func (e *EngineOperations) Config() interface{} {
	return new(v1alpha2.CreateContainerRequest)
}

// PrepareConfig is called in stage1 to validate and prepare container configuration.
func (e *EngineOperations) PrepareConfig(_ net.Conn, conf *starter.Config) error {
	sylog.Debugf("preparing config for container %q", e.containerName)
	conf.SetInstance(true)
	conf.SetMountPropagation("shared")

	podInst, err := instance.Get(e.createContainerRequest.GetPodSandboxId())
	if err != nil {
		return fmt.Errorf("could not get pod instance %q: %v", e.createContainerRequest.GetPodSandboxId(), err)
	}
	podNsPath := fmt.Sprintf(`/proc/%d/ns`, podInst.Pid)

	var joinNs []specs.LinuxNamespace
	var createNs []specs.LinuxNamespace
	sylog.Debugf("requesting Mount namespace")
	createNs = append(createNs, specs.LinuxNamespace{
		Type: specs.MountNamespace,
	})
	sylog.Debugf("requesting PID namespace")
	createNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.PIDNamespace,
	})

	if e.podConfig.GetHostname() != "" {
		sylog.Debugf("joining pod's UTS namespace")
		joinNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.UTSNamespace,
			Path: filepath.Join(podNsPath, "uts"),
		})
	}

	conf.SetNoNewPrivs(e.security.GetNoNewPrivs())
	switch e.security.GetNamespaceOptions().GetIpc() {
	case v1alpha2.NamespaceMode_CONTAINER:
		sylog.Debugf("requesting IPC namespace")
		createNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.IPCNamespace,
		})
	case v1alpha2.NamespaceMode_POD:
		sylog.Debugf("joining pod's IPC namespace")
		joinNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.IPCNamespace,
			Path: filepath.Join(podNsPath, "ipc"),
		})
	}

	switch e.security.GetNamespaceOptions().GetNetwork() {
	case v1alpha2.NamespaceMode_CONTAINER:
		sylog.Debugf("requesting NET namespace")
		createNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
		})
	case v1alpha2.NamespaceMode_POD:
		sylog.Debugf("joining pod's NET namespace")
		joinNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
			Path: filepath.Join(podNsPath, "net"),
		})
	}

	conf.SetNsPathFromSpec(joinNs)
	conf.SetNsFlagsFromSpec(createNs)

	if e.containerConfig.GetLogPath() != "" {
		logPath := filepath.Join(e.podConfig.LogDirectory, e.containerConfig.GetLogPath())
		err := os.MkdirAll(filepath.Dir(logPath), os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create log directory: %v", err)
		}
		sylog.Debugf("creating log file")
		logs, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("could not create log file: %v", err)
		}
		logs.Close()
	}

	// todo request UserNamespace?
	return nil
}