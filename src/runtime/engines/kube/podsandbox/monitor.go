package podsandbox

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/user"
)

// PostStartProcess is called in smaster after successful execution of container process.
// Since pod is run as instance PostStartProcess creates instance file on host fs.
func (e *EngineOperations) PostStartProcess(pid int) error {
	sylog.Debugf("pod %q is running", e.podName)
	file, err := instance.Add(e.podName, true)
	if err != nil {
		return fmt.Errorf("could not create instance file: %v", err)
	}
	file.Config, err = json.Marshal(e.CommonConfig)
	if err != nil {
		return fmt.Errorf("could not marshal engine config: %v", err)
	}
	sylog.Debugf("instance file is %s", file.Path)

	uid := os.Getuid()
	pw, err := user.GetPwUID(uint32(uid))
	if err != nil {
		return fmt.Errorf("could not get pwuid: %v", err)
	}
	sylog.Debugf("pwuid: %+v", pw)

	file.User = pw.Name
	file.Pid = pid
	file.PPid = os.Getpid()
	return file.Update()
}

// CleanupContainer is called in smaster after the MontiorContainer returns.
// It is responsible for ensuring that the pod is indeed removed. Currently it
// only removes instance file from host fs.
func (e *EngineOperations) CleanupContainer() error {
	sylog.Debugf("removing instance file for pod %q", e.podName)
	pid := os.Getpid()
	file, err := instance.Get(e.podName)
	if err != nil {
		return fmt.Errorf("could not get instance %q: %v", e.podName, err)
	}
	if file.PPid != pid {
		sylog.Debugf("cleanup container is called from fake parent! expected %d, but got %d", file.PPid, pid)
		return nil
	}
	return file.Delete()
}
