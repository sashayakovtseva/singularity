package kube

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/user"
)

var (
	// ErrNotFound is returned when requested resource is not found.
	ErrNotFound = fmt.Errorf("not found")
)

// AddInstanceFile adds instance file with given name.
func AddInstanceFile(name, image string, pid int, config interface{}) error {
	file, err := instance.Add(name, true)
	if err != nil {
		return fmt.Errorf("could not create instance file: %v", err)
	}
	file.Config, err = json.Marshal(config)
	if err != nil {
		return fmt.Errorf("could not marshal config: %v", err)
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
	file.Image = image
	file.PPid = os.Getpid()
	return file.Update()
}

// GetInstance is a wrapper for getting instance file. When no instance file
// is found ErrNotFound is returned.
func GetInstance(name string) (*instance.File, error) {
	inst, err := instance.Get(name)
	if err != nil {
		if strings.Contains(err.Error(), "no instance found") {
			return nil, ErrNotFound
		}
	}
	return inst, nil
}
