package container

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"syscall"

	"github.com/sylabs/sif/pkg/sif"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/loop"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
	"github.com/sylabs/singularity/src/runtime/engines/singularity/rpc/client"
	"k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// CreateContainer creates a container. This method is called in the same
// namespaces as target container and used for proper namespaces initialization.
func (e *EngineOperations) CreateContainer(containerPID int, rpcConn net.Conn) error {
	sylog.Debugf("setting up container %q", e.containerName)
	rpcOps := &client.RPC{
		Client: rpc.NewClient(rpcConn),
		Name:   e.CommonConfig.EngineName,
	}

	ll("/proc/self/ns")
	rpcOps.Ll("/")
	rpcOps.Ll("/proc/self/ns")
	rpcOps.Ll("/proc/self/fd")
	rpcOps.Ll("/tmp")
	rpcOps.Ll("/tmp/log/pods/testpoduid/testcontainer")

	imagePath := e.containerConfig.GetImage().GetImage()
	containerPath := filepath.Join("/mnt", e.containerName)
	lowerPath := filepath.Join(containerPath, "lower")
	upperPath := filepath.Join(containerPath, "upper")
	workPath := filepath.Join(containerPath, "work")
	chrootPath := filepath.Join(containerPath, "root")

	_, err := rpcOps.Mount("tmpfs", "/mnt", "tmpfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount tmpfs into /mnt: %v", err)
	}

	err = mountImage(rpcOps, imagePath, lowerPath)
	if err != nil {
		return fmt.Errorf("could not mount image: %v", err)
	}
	err = mountBinds(rpcOps, upperPath, e.containerConfig.Mounts)
	if err != nil {
		return fmt.Errorf("could not mount binds: %v", err)
	}

	_, err = rpcOps.Mkdir(workPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not create working directory for overlay: %v", err)
	}
	_, err = rpcOps.Mkdir(chrootPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not create root directory for overlay: %v", err)
	}
	overlayOpts := fmt.Sprintf("lowerdir=%s,workdir=%s", lowerPath, workPath)
	if len(e.containerConfig.Mounts) != 0 {
		overlayOpts += fmt.Sprintf(",upperdir=%s", upperPath)
	}

	sylog.Debugf("mounting overlay with options: %v", overlayOpts)
	_, err = rpcOps.Mount("overlay", chrootPath, "overlay", syscall.MS_NOSUID|syscall.MS_REC, overlayOpts)
	if err != nil {
		return fmt.Errorf("could not mount overlay: %v", err)
	}

	err = mountSysFs(rpcOps, chrootPath)
	if err != nil {
		return fmt.Errorf("could not mount system fs: %v", err)
	}

	logFileName := filepath.Base(e.containerConfig.LogPath)
	if logFileName != "" {
		hostLogDir := filepath.Dir(filepath.Join(e.podConfig.LogDirectory, e.containerConfig.LogPath))
		contLogDir := filepath.Join(chrootPath, "/tmp", "/logs")
		_, err = rpcOps.Mkdir(contLogDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create log dir: %v", err)
		}
		_, err = rpcOps.Mount(hostLogDir, contLogDir, "tempfs", syscall.MS_NOSUID|syscall.MS_BIND, "")
		if err != nil {
			return fmt.Errorf("could not mount log file: %v", err)
		}
	}

	rpcOps.Ll(upperPath)
	rpcOps.Ll(upperPath + "/mounted1")
	rpcOps.Ll(upperPath + "/mounted2")

	_, err = rpcOps.Mount("", "/", "", syscall.MS_SLAVE|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not set RPC mount propagation flag to SLAVE: %v", err)
	}
	_, err = rpcOps.Chroot(chrootPath)
	if err != nil {
		return fmt.Errorf("could not chroot: %v", err)
	}
	rpcOps.Ll("/proc/self/fd")
	rpcOps.Ll("/tmp")
	rpcOps.Ll("/tmp/logs")
	rpcOps.Ll("/")
	rpcOps.Ll("/mounted1")
	rpcOps.Ll("/mounted2")

	err = kube.AddInstanceFile(e.containerName, imagePath, containerPID, e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not add instance file: %v", err)
	}

	sylog.Debugf("stopping container %q", e.containerName)
	err = syscall.Kill(containerPID, syscall.SIGSTOP)
	if err != nil {
		return fmt.Errorf("could not send stop signal to container: %v", err)
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
	var mountType string
	if fstype == sif.FsSquash {
		mountType = "squashfs"
	} else {
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
	dev, err := rpcOps.LoopDevice(imagePath, os.O_RDWR, info)
	if err != nil {
		return fmt.Errorf("could not attach loop dev: %v", err)
	}

	_, err = rpcOps.Mkdir(targetPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("could not make lowerdir for overlay: %v", err)
	}
	_, err = rpcOps.Mount(fmt.Sprintf("/dev/loop%d", dev), targetPath, mountType, syscall.MS_NOSUID|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not mount loop device: %v", err)
	}
	return nil
}

func mountBinds(rpcOps *client.RPC, targetRoot string, mounts []*v1alpha2.Mount) error {
	for _, mount := range mounts {
		source := mount.HostPath
		mi, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("invalid bind mount source: %v", err)
		}
		if mi.Mode()&os.ModeSymlink == os.ModeSymlink {
			source, err = os.Readlink(mount.HostPath)
			if err != nil {
				return fmt.Errorf("could follow mount source symlink: %v", err)
			}
		}

		target := filepath.Join(targetRoot, mount.ContainerPath)
		_, err = rpcOps.Mkdir(target, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create directory in for bind mount: %v", err)
		}

		flags := syscall.MS_NOSUID | syscall.MS_BIND | syscall.MS_REC | syscall.MS_NODEV
		if mount.Readonly {
			flags |= syscall.MS_RDONLY
		}
		switch mount.GetPropagation() {
		case v1alpha2.MountPropagation_PROPAGATION_PRIVATE:
			flags |= syscall.MS_PRIVATE
		case v1alpha2.MountPropagation_PROPAGATION_HOST_TO_CONTAINER:
			flags |= syscall.MS_SLAVE
		case v1alpha2.MountPropagation_PROPAGATION_BIDIRECTIONAL:
			flags |= syscall.MS_SHARED
		}

		_, err = rpcOps.Mount(source, target, "", uintptr(flags), "")
		if err != nil {
			return fmt.Errorf("could not bind mount: %v", err)
		}
	}
	return nil
}

func mountSysFs(rpcOps *client.RPC, root string) error {
	_, err := rpcOps.Mount("proc", filepath.Join(root, "/proc"), "proc", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount proc fs: %v", err)
	}
	_, err = rpcOps.Mount("/dev", filepath.Join(root, "/dev"), "udev", syscall.MS_NOSUID|syscall.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("could not mount udev: %v", err)
	}
	_, err = rpcOps.Mount("sysfs", filepath.Join(root, "/sys"), "sysfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount sysfs: %v", err)
	}
	_, err = rpcOps.Mount("tmpfs", filepath.Join(root, "/tmp"), "tmpfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount /tmp: %v", err)
	}
	return nil
}

func ll(dir string) {
	fii, err := ioutil.ReadDir(dir)
	if err != nil {
		sylog.Debugf("read %q error: %v", dir, err)
		return
	}
	sylog.Debugf("content of %s", dir)
	for _, fi := range fii {
		link, _ := os.Readlink(filepath.Join(dir, fi.Name()))
		sylog.Debugf("\t%s\t%s -> %s", fi.Mode().String(), fi.Name(), link)
	}
}
