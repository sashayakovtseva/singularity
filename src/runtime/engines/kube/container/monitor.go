package container

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/mainthread"
	"github.com/sylabs/singularity/src/pkg/util/user"
)

// PostStartProcess is called in smaster after successful execution of container process.
// Since container is run as instance PostStartProcess creates instance file on host fs.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("container %q is running", e.containerName)

	uid := os.Getuid()
	gid := os.Getgid()
	// todo set to false when in UserNamespace?
	privileged := true

	file, err := instance.Add(e.containerName, privileged)
	if err != nil {
		return fmt.Errorf("could not create instance file: %v", err)
	}
	file.Config, err = json.Marshal(e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not marshal engine config: %v", err)
	}
	sylog.Debugf("instance file is %s", file.Path)

	pw, err := user.GetPwUID(uint32(uid))
	if err != nil {
		return fmt.Errorf("could not get pwuid: %v", err)
	}
	sylog.Debugf("pwuid: %+v", pw)

	file.User = pw.Name
	file.Pid = pid
	file.PPid = os.Getpid()
	if privileged {
		var err error
		mainthread.Execute(func() {
			if err = syscall.Setresuid(0, 0, uid); err != nil {
				err = fmt.Errorf("failed to escalate uid privileges")
				return
			}
			if err = syscall.Setresgid(0, 0, gid); err != nil {
				err = fmt.Errorf("failed to escalate gid privileges")
				return
			}
			if err = file.Update(); err != nil {
				return
			}
			if err = syscall.Setresgid(gid, gid, 0); err != nil {
				err = fmt.Errorf("failed to escalate gid privileges")
				return
			}
			if err := syscall.Setresuid(uid, uid, 0); err != nil {
				err = fmt.Errorf("failed to escalate uid privileges")
				return
			}
		})
		return err
	}
	return file.Update()
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// It is responsible for ensuring that the pod is indeed removed. Currently it
// only removes instance file from host fs.
func (e *EngineOperations) CleanupContainer() error {
	sylog.Debugf("removing instance file for container %q", e.containerName)
	uid := os.Getuid()
	pid := os.Getpid()
	file, err := instance.Get(e.containerName)
	if err != nil {
		return fmt.Errorf("could not get instance %q: %v", e.containerName, err)
	}

	if file.PPid != pid {
		sylog.Debugf("cleanup container is called from fake parent! expected %d, but got %d", file.PPid, pid)
		return nil
	}

	if file.Privileged {
		var err error
		mainthread.Execute(func() {
			if err = syscall.Setresuid(0, 0, uid); err != nil {
				err = fmt.Errorf("failed to escalate privileges: %v", err)
				return
			}
			defer syscall.Setresuid(uid, uid, 0)
			err = file.Delete()
		})
		return err
	}
	return file.Delete()
}
