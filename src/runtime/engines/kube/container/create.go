package container

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/rpc"
	"syscall"

	"os"

	"github.com/sylabs/sif/pkg/sif"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/loop"
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

	rootfs := "/mnt/" + e.containerName
	imagePath := e.containerConfig.Image.Image
	{
		sylog.Debugf("mounting tmpfs into /mnt")
		_, err := rpcOps.Mount("tmpfs", "/mnt", "tmpfs", syscall.MS_NOSUID, "")
		if err != nil {
			return fmt.Errorf("could not mount tmpfs into /mnt: %s", err)
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
	_, err := rpcOps.Mount("proc", rootfs+"/proc", "proc", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount proc fs: %s", err)
	}
	sylog.Debugf("mounting dev")
	_, err = rpcOps.Mount("/dev", rootfs+"/dev", "udev", syscall.MS_NOSUID|syscall.MS_BIND, "")
	if err != nil {
		return fmt.Errorf("could not mount udev: %s", err)
	}
	sylog.Debugf("mounting sysfs")
	_, err = rpcOps.Mount("sysfs", rootfs+"/sys", "sysfs", syscall.MS_NOSUID, "")
	if err != nil {
		return fmt.Errorf("could not mount sysfs: %s", err)
	}

	sylog.Debugf("set RPC mount propagation flag to SLAVE")
	_, err = rpcOps.Mount("", "/", "", syscall.MS_SLAVE|syscall.MS_REC, "")
	if err != nil {
		return fmt.Errorf("could not set RPC mount propagation flag to SLAVE: %v", err)
	}
	_, err = rpcOps.Chroot(rootfs)
	if err != nil {
		return fmt.Errorf("could not chroot: %v", err)
	}

	err = e.addInstanceFile(containerPID)
	if err != nil {
		return fmt.Errorf("could not add instance file: %v", err)
	}

	err = rpcConn.Close()
	if err != nil {
		return fmt.Errorf("could not close rpc: %v", err)
	}

	sylog.Debugf("stopping container %q", e.containerName)
	err = syscall.Kill(containerPID, syscall.SIGSTOP)
	if err != nil {
		return fmt.Errorf("could not send stop signal to container: %v", err)
	}
	return nil
}

func ll(dir string) {
	fii, _ := ioutil.ReadDir(dir)
	sylog.Debugf("content of %s", dir)
	for _, fi := range fii {
		sylog.Debugf("\t%s\t%s", fi.Mode().String(), fi.Name())
	}
}
