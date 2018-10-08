package kube

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/user"
)

var (
	// ErrNotFound is returned when requested resource is not found.
	ErrNotFound = fmt.Errorf("not found")
)

const (
	InfoStoragePath = "/var/run/singularity/info"

	StatusFile = "status"
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

	file.User = pw.Name
	file.Pid = pid
	file.Image = image
	file.PPid = os.Getpid()
	err = file.Update()
	if err != nil {
		return fmt.Errorf("could not write instance file: %v", err)
	}

	infoPath, err := pathToInfoDir(name)
	if err != nil {
		return err
	}
	fileName := filepath.Base(file.Path)
	err = os.Symlink(file.Path, filepath.Join(infoPath, fileName))
	if err != nil {
		return fmt.Errorf("coudl not symlink instance file: %v", err)
	}
	return nil
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

// AddExitStatusFile checks that instance file still exists and writes file with
// exit status in info directory.
func AddExitStatusFile(name string, status syscall.WaitStatus) error {
	payload := fmt.Sprintf("%d", status.ExitStatus())
	return addInfoFile(name, StatusFile, []byte(payload))
}

func addInfoFile(name, kind string, info []byte) error {
	if info[len(info)-1] != '\n' {
		info = append(info, '\n')
	}
	_, err := GetInstance(name)
	if err != nil {
		return fmt.Errorf("could not fetch instance: %v", err)
	}
	path, err := pathToInfoDir(name)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(path, kind), info, 0644)
	if err != nil {
		return fmt.Errorf("could not write %s info file: %v", kind, err)
	}
	return nil
}

func pathToInfoDir(name string) (string, error) {
	path := filepath.Join(InfoStoragePath, name)

	oldumask := syscall.Umask(0)
	defer syscall.Umask(oldumask)

	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("could not create info storage path: %v", err)
	}
	return path, nil
}
