package container

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"syscall"

	"github.com/sylabs/sif/pkg/sif"
	"github.com/sylabs/singularity/src/pkg/buildcfg"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/loop"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
	"github.com/sylabs/singularity/src/runtime/engines/singularity/rpc/client"
	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// CreateContainer creates a container. This method is called in the same
// namespaces as target container and used for proper namespaces initialization.
func (e *EngineOperations) CreateContainer(containerPID int, rpcConn net.Conn) error {
	sylog.Debugf("setting up container %q", e.containerName)
	rpcOps := &client.RPC{
		Client: rpc.NewClient(rpcConn),
		Name:   e.CommonConfig.EngineName,
	}

	sylog.Debugf("setting mount propagation to SLAVE")
	_, err := rpcOps.Mount("", "/", "", syscall.MS_SLAVE|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not set RPC mount propagation flag to SLAVE: %v", err)
	}
	var (
		imagePath     = e.containerConfig.GetImage().GetImage()
		containerPath = buildcfg.SESSIONDIR
		lowerPath     = filepath.Join(containerPath, "lower")
		upperPath     = filepath.Join(containerPath, "upper")
		workPath      = filepath.Join(containerPath, "work")
		chrootPath    = filepath.Join(containerPath, "root")
	)
	sylog.Debugf("creating %s", containerPath)
	_, err = rpcOps.Mkdir(containerPath, 0755)
	if err != nil {
		return fmt.Errorf("could not create directory for container: %v", err)
	}
	sylog.Debugf("mounting tmpfs into %s", containerPath)
	_, err = rpcOps.Mount("tmpfs", containerPath, "tmpfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount tmpfs into container directory %q: %v", containerPath, err)
	}

	err = mountImage(rpcOps, imagePath, lowerPath)
	if err != nil {
		return fmt.Errorf("could not mount image: %v", err)
	}
	err = createBindDirs(rpcOps, upperPath, e.containerConfig.GetMounts())
	if err != nil {
		return fmt.Errorf("could not mount binds: %v", err)
	}

	sylog.Debugf("creating %s", upperPath)
	_, err = rpcOps.Mkdir(upperPath, 0755)
	if err != nil {
		return fmt.Errorf("could not create upper directory for overlay: %v", err)
	}
	sylog.Debugf("creating %s", workPath)
	_, err = rpcOps.Mkdir(workPath, 0755)
	if err != nil {
		return fmt.Errorf("could not create working directory for overlay: %v", err)
	}
	sylog.Debugf("creating %s", chrootPath)
	_, err = rpcOps.Mkdir(chrootPath, 0755)
	if err != nil {
		return fmt.Errorf("could not create root directory for overlay: %v", err)
	}

	overlayOpts := fmt.Sprintf("lowerdir=%s,workdir=%s,upperdir=%s", lowerPath, workPath, upperPath)
	sylog.Debugf("mounting overlay with options: %v", overlayOpts)
	_, err = rpcOps.Mount("overlay", chrootPath, "overlay", syscall.MS_NOSUID|syscall.MS_REC, overlayOpts)
	if err != nil {
		return fmt.Errorf("could not mount overlay: %v", err)
	}

	err = mountBinds(rpcOps, chrootPath, e.containerConfig.GetMounts())
	if err != nil {
		return fmt.Errorf("could not mount system fs: %v", err)
	}
	err = mountSysFs(rpcOps, chrootPath)
	if err != nil {
		return fmt.Errorf("could not mount system fs: %v", err)
	}

	if e.containerConfig.GetLogPath() != "" {
		hostLogDir := filepath.Dir(filepath.Join(e.podConfig.GetLogDirectory(), e.containerConfig.GetLogPath()))
		contLogDir := filepath.Join(chrootPath, "/tmp/logs")
		_, err = rpcOps.Mkdir(contLogDir, 0755)
		if err != nil {
			return fmt.Errorf("could not create log dir: %v", err)
		}
		sylog.Debugf("mounting log directory %q to %q", hostLogDir, contLogDir)
		_, err = rpcOps.Mount(hostLogDir, contLogDir, "tempfs", syscall.MS_NOSUID|syscall.MS_BIND, "")
		if err != nil {
			return fmt.Errorf("could not mount log directory: %v", err)
		}
	}

	_, err = rpcOps.Chroot(chrootPath)
	if err != nil {
		return fmt.Errorf("could not chroot: %v", err)
	}

	err = kube.AddInstanceFile(e.containerName, imagePath, containerPID, e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not add instance file: %v", err)
	}
	err = kube.AddCreatedFile(e.containerName)
	if err != nil {
		return fmt.Errorf("could not add created timestamp file: %v", err)
	}

	err = rpcConn.Close()
	if err != nil {
		return fmt.Errorf("could not close rpc: %v", err)
	}
	return nil
}

func mountImage(rpcOps *client.RPC, imagePath, targetPath string) error {
	file, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("could not open image: %v", err)
	}
	fimg, err := sif.LoadContainerFp(file, true)
	if err != nil {
		return err
	}
	part, _, err := fimg.GetPartPrimSys()
	if err != nil {
		return err
	}
	fstype, err := part.GetFsType()
	if err != nil {
		return err
	}
	if fstype != sif.FsSquash {
		return fmt.Errorf("unsuported image fs type: %v", fstype)
	}
	loopFlags := uint32(loop.FlagsAutoClear)
	info := loop.Info64{
		Offset:    uint64(part.Fileoff),
		SizeLimit: uint64(part.Filelen),
		Flags:     loopFlags,
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("could not close file: %v", err)
	}

	sylog.Debugf("mounting %s into loop device", imagePath)
	dev, err := rpcOps.LoopDevice(imagePath, os.O_RDWR, info)
	if err != nil {
		return fmt.Errorf("could not attach loop dev: %v", err)
	}

	sylog.Debugf("creating %s", targetPath)
	_, err = rpcOps.Mkdir(targetPath, 0755)
	if err != nil {
		return fmt.Errorf("could not make lowerdir for overlay: %v", err)
	}

	sylog.Debugf("mounting loop device #%d into %s", dev, targetPath)
	_, err = rpcOps.Mount(fmt.Sprintf("/dev/loop%d", dev), targetPath, "squashfs", syscall.MS_NOSUID|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not mount loop device: %v", err)
	}
	return nil
}

func createBindDirs(rpcOps *client.RPC, targetRoot string, mounts []*k8s.Mount) error {
	for _, mount := range mounts {
		target := filepath.Join(targetRoot, mount.GetContainerPath())
		sylog.Debugf("creating %s", target)
		_, err := rpcOps.Mkdir(target, 0755)
		if err != nil {
			return fmt.Errorf("could not create directory in for bind mount: %v", err)
		}
	}
	return nil
}

func mountBinds(rpcOps *client.RPC, targetRoot string, mounts []*k8s.Mount) error {
	for _, mount := range mounts {
		source := mount.GetHostPath()
		mi, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("invalid bind mount source: %v", err)
		}
		if mi.Mode()&os.ModeSymlink == os.ModeSymlink {
			source, err = os.Readlink(mount.GetHostPath())
			if err != nil {
				return fmt.Errorf("could follow mount source symlink: %v", err)
			}
		}

		target := filepath.Join(targetRoot, mount.GetContainerPath())

		var flags uintptr = syscall.MS_BIND | syscall.MS_REC
		if mount.GetReadonly() {
			flags |= syscall.MS_RDONLY
		}
		sylog.Debugf("mounting %s to %s", source, target)
		_, err = rpcOps.Mount(source, target, "", flags, "")
		if err != nil {
			return fmt.Errorf("could not bind mount: %v", err)
		}

		if mount.GetReadonly() {
			sylog.Debugf("remounting due to readonly flag for %s", target)
			_, err = rpcOps.Mount("", target, "", flags|syscall.MS_REMOUNT, "")
			if err != nil {
				return fmt.Errorf("could not remount: %v", err)
			}
		}

		propagation := syscall.MS_PRIVATE
		switch mount.GetPropagation() {
		case k8s.MountPropagation_PROPAGATION_HOST_TO_CONTAINER:
			propagation = syscall.MS_SLAVE
		case k8s.MountPropagation_PROPAGATION_BIDIRECTIONAL:
			propagation = syscall.MS_SHARED
		}
		sylog.Debugf("setting %s propagation to %s", target, mount.GetPropagation())
		_, err = rpcOps.Mount("", target, "", uintptr(propagation), "")
		if err != nil {
			return fmt.Errorf("could not set mount propagation: %v", err)
		}
	}
	return nil
}

func mountSysFs(rpcOps *client.RPC, root string) error {
	procPath := filepath.Join(root, "/proc")
	sylog.Debugf("mounting proc into %s", procPath)
	_, err := rpcOps.Mount("proc", procPath, "proc", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount proc fs: %v", err)
	}

	devPath := filepath.Join(root, "/dev")
	sylog.Debugf("mounting /dev into %s", devPath)
	_, err = rpcOps.Mount("/dev", devPath, "udev", syscall.MS_NOSUID|syscall.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("could not mount udev: %v", err)
	}

	sysPath := filepath.Join(root, "/sys")
	sylog.Debugf("mounting sysfs into %s", sysPath)
	_, err = rpcOps.Mount("sysfs", filepath.Join(root, "/sys"), "sysfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount sysfs: %v", err)
	}

	tmpPath := filepath.Join(root, "/tmp")
	sylog.Debugf("mounting tmpfs into %s", tmpPath)
	_, err = rpcOps.Mount("tmpfs", tmpPath, "tmpfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount /tmp: %v", err)
	}
	return nil
}
