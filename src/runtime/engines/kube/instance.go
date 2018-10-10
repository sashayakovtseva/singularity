package kube

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sylabs/singularity/src/pkg/instance"
	"github.com/sylabs/singularity/src/pkg/sylog"
	"github.com/sylabs/singularity/src/pkg/util/user"
)

var (
	// ErrNotFound is returned when requested resource is not found.
	ErrNotFound = fmt.Errorf("not found")
)

const (
	infoStoragePath = "/var/run/singularity/info"

	infoFile     = "info"
	exitCodeFile = "exit"
	createdFile  = "created"
	startedFile  = "started"
	finishedFile = "finished"
)

// Info holds info about container's status.
type Info struct {
	CreatedAt  int64
	StartedAt  int64
	FinishedAt int64
	ExitCode   int64
}

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
	symlinkPath := filepath.Join(infoPath, infoFile)
	err = os.Symlink(file.Path, symlinkPath)
	if err != nil {
		return fmt.Errorf("coudl not symlink instance file: %v", err)
	}
	sylog.Debugf("instance file is %s", symlinkPath)
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

// GetInfo reads all info files from dedicated directory and returns
// its contents in a convenient form of a struct.
func GetInfo(name string) (*Info, error) {
	path, err := pathToInfoDir(name)
	if err != nil {
		return nil, err
	}

	var i Info
	created, err := ioutil.ReadFile(filepath.Join(path, createdFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	i.CreatedAt, err = strconv.ParseInt(string(created), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %v", err)
	}

	started, err := ioutil.ReadFile(filepath.Join(path, startedFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	i.StartedAt, err = strconv.ParseInt(string(started), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %v", err)
	}

	finished, err := ioutil.ReadFile(filepath.Join(path, finishedFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	i.FinishedAt, err = strconv.ParseInt(string(finished), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %v", err)
	}

	exitCode, err := ioutil.ReadFile(filepath.Join(path, exitCodeFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	i.ExitCode, err = strconv.ParseInt(string(exitCode), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid exit code: %v", err)
	}

	return &i, nil
}

// CleanupInstance removes all files related to instance with passed name.
func CleanupInstance(name string) error {
	pid := os.Getpid()
	file, err := GetInstance(name)
	if err != nil && err != ErrNotFound {
		return fmt.Errorf("could not get instance %q: %v", name, err)
	}
	if err == nil {
		if file.PPid != pid {
			sylog.Debugf("unauthorized cleanup: expected ppid %d, but got %d", file.PPid, pid)
			return nil
		}
		if err := file.Delete(); err != nil {
			return fmt.Errorf("could not remove instance file: %v", err)
		}
	}

	infoPath, err := pathToInfoDir(name)
	if err != nil {
		return err
	}
	err = os.RemoveAll(infoPath)
	if err != nil {
		return fmt.Errorf("could not remove info directory: %v", err)
	}
	return nil
}

// AddExitCodeFile checks that instance file still exists and writes file with
// exit status in corresponding info directory.
func AddExitCodeFile(name string, status syscall.WaitStatus) error {
	payload := fmt.Sprintf("%d", status.ExitStatus())
	return addInfoFile(name, exitCodeFile, []byte(payload))
}

// AddCreatedFile checks that instance file still exists and writes file with
// creation timestamp in nanoseconds in corresponding info directory.
func AddCreatedFile(name string) error {
	payload := fmt.Sprintf("%d", time.Now().UnixNano())
	return addInfoFile(name, createdFile, []byte(payload))
}

// AddStartedFile checks that instance file still exists and writes file with
// start timestamp in nanoseconds in corresponding info directory.
func AddStartedFile(name string) error {
	payload := fmt.Sprintf("%d", time.Now().UnixNano())
	return addInfoFile(name, startedFile, []byte(payload))
}

// AddFinishedFile checks that instance file still exists and writes file with
// exit timestamp in nanoseconds in corresponding info directory.
func AddFinishedFile(name string) error {
	payload := fmt.Sprintf("%d", time.Now().UnixNano())
	return addInfoFile(name, finishedFile, []byte(payload))
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
	path := filepath.Join(infoStoragePath, name)

	oldumask := syscall.Umask(0)
	defer syscall.Umask(oldumask)

	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("could not create info storage path: %v", err)
	}
	return path, nil
}
