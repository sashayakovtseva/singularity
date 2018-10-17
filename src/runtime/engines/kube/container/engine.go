package container

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/runtime/engines/config"
	"github.com/sylabs/singularity/src/runtime/engines/config/starter"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

const (
	// Name of the engine.
	Name = "kube_container"

	// SigCreated is used to notify a caller that container was successfully created.
	SigCreated byte = 1

	// SigCleanup is used to notify a caller that container was cleaned up.
	SigCleanup byte = 2
)

// Config is a config used to create container.
type Config struct {
	Socket                 int
	CreateContainerRequest *k8s.CreateContainerRequest
	ExecSyncRequest        *k8s.ExecSyncRequest
}

// EngineOperations implements the engines.EngineOperations interface for the pod management process.
type EngineOperations struct {
	CommonConfig *config.Common
	config       *Config

	containerID     string
	containerConfig *k8s.ContainerConfig

	podID     string
	podConfig *k8s.PodSandboxConfig

	createError error
	conn        net.Conn
	isExecSync  bool
}

// InitConfig simply saves passed config into engine. Passed cfg already includes parsed ContainerConfig.
func (e *EngineOperations) InitConfig(cfg *config.Common) {
	e.CommonConfig = cfg
	e.config = cfg.EngineConfig.(*Config)
	if e.config.CreateContainerRequest != nil {
		createContainerRequest := e.config.CreateContainerRequest
		e.containerConfig = createContainerRequest.GetConfig()
		e.containerID = kube.ContainerID(createContainerRequest.PodSandboxId, e.containerConfig.GetMetadata())
		e.podConfig = createContainerRequest.GetSandboxConfig()
		e.podID = e.config.CreateContainerRequest.GetPodSandboxId()
	} else if e.config.ExecSyncRequest != nil {
		e.containerID = e.config.ExecSyncRequest.ContainerId
		e.isExecSync = true
	}
}

// Config returns empty CreateContainerRequest that will be filled later with received JSON data.
func (e *EngineOperations) Config() config.EngineConfig {
	return new(Config)
}

// PrepareConfig is called in stage1 to validate and prepare container configuration.
func (e *EngineOperations) PrepareConfig(_ net.Conn, conf *starter.Config) error {
	if e.isExecSync {
		return e.prepareExecSync(conf)
	}

	conf.SetInstance(true)
	conf.SetMountPropagation("shared")

	podInst, err := instance.Get(e.podID)
	if err != nil {
		return fmt.Errorf("could not get pod instance %q: %v", e.podID, err)
	}
	podNsPath := fmt.Sprintf(`/proc/%d/ns`, podInst.Pid)

	var joinNs []specs.LinuxNamespace
	var createNs []specs.LinuxNamespace
	sylog.Debugf("requesting Mount namespace")
	createNs = append(createNs, specs.LinuxNamespace{
		Type: specs.MountNamespace,
	})
	sylog.Debugf("requesting PID namespace")
	createNs = append(createNs, specs.LinuxNamespace{
		Type: specs.PIDNamespace,
	})

	if e.podConfig.GetHostname() != "" {
		sylog.Debugf("joining pod's UTS namespace")
		joinNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.UTSNamespace,
			Path: filepath.Join(podNsPath, "uts"),
		})
	}

	security := e.containerConfig.GetLinux().GetSecurityContext()
	conf.SetNoNewPrivs(security.GetNoNewPrivs())
	switch security.GetNamespaceOptions().GetIpc() {
	case k8s.NamespaceMode_CONTAINER:
		sylog.Debugf("requesting IPC namespace")
		createNs = append(createNs, specs.LinuxNamespace{
			Type: specs.IPCNamespace,
		})
	case k8s.NamespaceMode_POD:
		sylog.Debugf("joining pod's IPC namespace")
		joinNs = append(joinNs, specs.LinuxNamespace{
			Type: specs.IPCNamespace,
			Path: filepath.Join(podNsPath, "ipc"),
		})
	}

	switch security.GetNamespaceOptions().GetNetwork() {
	case k8s.NamespaceMode_CONTAINER:
		sylog.Debugf("requesting NET namespace")
		createNs = append(createNs, specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
		})
	case k8s.NamespaceMode_POD:
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

	return nil
}

func (e *EngineOperations) prepareExecSync(conf *starter.Config) error {
	contInst, err := instance.Get(e.containerID)
	if err != nil {
		return fmt.Errorf("could not get container instance %q: %v", e.containerID, err)
	}

	contConfig := new(Config)
	if err := json.Unmarshal(contInst.Config, contConfig); err != nil {
		return fmt.Errorf("could not unmarshal container config: %v", err)
	}

	contNsPath := fmt.Sprintf(`/proc/%d/ns`, contInst.Pid)
	var joinNs []specs.LinuxNamespace
	sylog.Debugf("joining Mount namespace")
	joinNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.MountNamespace,
		Path: filepath.Join(contNsPath, "mount"),
	})
	sylog.Debugf("joining PID namespace")
	joinNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.PIDNamespace,
		Path: filepath.Join(contNsPath, "pid"),
	})
	sylog.Debugf("joining  UTS namespace")
	joinNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.UTSNamespace,
		Path: filepath.Join(contNsPath, "uts"),
	})
	sylog.Debugf("joining IPC namespace")
	joinNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.IPCNamespace,
		Path: filepath.Join(contNsPath, "ipc"),
	})

	sylog.Debugf("joining pod's NET namespace")
	joinNs = append(joinNs, specs.LinuxNamespace{
		Type: specs.NetworkNamespace,
		Path: filepath.Join(contNsPath, "net"),
	})

	conf.SetNsPathFromSpec(joinNs)
	conf.SetNoNewPrivs(contConfig.CreateContainerRequest.GetConfig().GetLinux().GetSecurityContext().GetNoNewPrivs())
	return nil
}
