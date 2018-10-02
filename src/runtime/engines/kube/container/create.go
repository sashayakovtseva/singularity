package container

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/rpc"
	"syscall"

	"os"

	"path/filepath"

	"github.com/sylabs/sif/pkg/sif"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/loop"
	"github.com/sylabs/singularity/src/runtime/engines/kube"
	"github.com/sylabs/singularity/src/runtime/engines/singularity/rpc/client"
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

	rootfs := "/mnt/" + e.containerName
	imagePath := e.containerConfig.GetImage().GetImage()
	{
		sylog.Debugf("mounting tmpfs into /mnt")
		_, err := rpcOps.Mount("tmpfs", "/mnt", "tmpfs", syscall.MS_NOSUID, "")
		if err != nil {
			return fmt.Errorf("could not mount tmpfs into /mnt: %v", err)
		}
		file, err := os.Open(imagePath)
		if err != nil {
			return fmt.Errorf("could not open image: %v", err)
		}
		// Load the SIF file
		fimg, err := sif.LoadContainerFp(file, true)
		if err != nil {
			return err
		}
		// Get the default system partition image
		part, _, err := fimg.GetPartPrimSys()
		if err != nil {
			return err
		}
		// record the fs type
		fstype, err := part.GetFsType()
		if err != nil {
			return err
		}
		var mountType string
		if fstype == sif.FsSquash {
			mountType = "squashfs"
		} else if fstype == sif.FsExt3 {
			mountType = "ext3"
		} else {
			return fmt.Errorf("unknown file system type: %v", fstype)
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
		sylog.Debugf("loop device is %d", dev)
		_, err = rpcOps.Mkdir(rootfs, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not make rootfs temp dir: %v", err)
		}
		_, err = rpcOps.Mount(fmt.Sprintf("/dev/loop%d", dev), rootfs, mountType, syscall.MS_NOSUID|syscall.MS_REC, "")
		if err != nil {
			return fmt.Errorf("could not mount loop device: %v", err)
		}
	}

	sylog.Debugf("mounting procfs")
	_, err := rpcOps.Mount("proc", filepath.Join(rootfs, "/proc"), "proc", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount proc fs: %v", err)
	}
	sylog.Debugf("mounting dev")
	_, err = rpcOps.Mount("/dev", filepath.Join(rootfs, "/dev"), "udev", syscall.MS_NOSUID|syscall.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("could not mount udev: %v", err)
	}
	sylog.Debugf("mounting sysfs")
	_, err = rpcOps.Mount("sysfs", filepath.Join(rootfs, "/sys"), "sysfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount sysfs: %v", err)
	}
	sylog.Debugf("mounting /tmp")
	_, err = rpcOps.Mount("tmpfs", filepath.Join(rootfs, "/tmp"), "tmpfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount /tmp: %v", err)
	}

	logFileName := filepath.Base(e.containerConfig.LogPath)
	if logFileName != "" {
		hostLogDir := filepath.Dir(filepath.Join(e.podConfig.LogDirectory, e.containerConfig.LogPath))
		contLogDir := filepath.Join(rootfs, "/tmp", "/logs")

		sylog.Debugf("creating dir for logs")
		_, err = rpcOps.Mkdir(contLogDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create log dir: %v", err)
		}
		sylog.Debugf("mounting log dir")
		_, err = rpcOps.Mount(hostLogDir, contLogDir, "tempfs", syscall.MS_NOSUID|syscall.MS_BIND, "")
		if err != nil {
			return fmt.Errorf("could not mount log file: %v", err)
		}
	}

	sylog.Debugf("setting mount propagation flag to SLAVE")
	_, err = rpcOps.Mount("", "/", "", syscall.MS_SLAVE|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not set RPC mount propagation flag to SLAVE: %v", err)
	}
	_, err = rpcOps.Chroot(rootfs)
	if err != nil {
		return fmt.Errorf("could not chroot: %v", err)
	}
	rpcOps.Ll("/proc/self/fd")
	rpcOps.Ll("/tmp")
	rpcOps.Ll("/tmp/logs")
	rpcOps.Ll("/tmp/logs/testcontainer")

	err = kube.AddInstanceFile(e.containerName, e.containerConfig.GetImage().GetImage(), containerPID, e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not add instance file: %v", err)
	}

	if logFileName != "" {
		sylog.Debugf("redirecting io to log file")
		err = rpcOps.RedirectIO(filepath.Join("/tmp", "/logs", logFileName))
		if err != nil {
			return fmt.Errorf("could not redirect io: %v", err)
		}
	}

	if err := rpcOps.Ll("/proc/self/fd"); err != nil {
		return fmt.Errorf("could not ll: %v", err)
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

func ll(dir string) {
	fii, _ := ioutil.ReadDir(dir)
	sylog.Debugf("content of %s", dir)
	for _, fi := range fii {
		link, _ := os.Readlink(filepath.Join(dir, fi.Name()))
		sylog.Debugf("\t%s\t%s -> %s", fi.Mode().String(), fi.Name(), link)
	}
}
