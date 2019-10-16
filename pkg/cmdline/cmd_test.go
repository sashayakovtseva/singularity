// Copyright (c) 2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package cmdline

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/user"
	"reflect"
	"runtime"
	"strconv"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{Use: "root"}
var parentCmd = &cobra.Command{Use: "parent"}
var childCmd = &cobra.Command{Use: "child"}

func newCommandManager(cmd *cobra.Command) (cm *CommandManager, err error) {
	defer func() {
		if t := recover(); t != nil {
			err = fmt.Errorf("%s", t)
		}
	}()
	return NewCommandManager(cmd), nil
}

func registerCmd(cm *CommandManager, cmd *cobra.Command) (err error) {
	defer func() {
		if t := recover(); t != nil {
			err = fmt.Errorf("%s", t)
		}
	}()
	cm.RegisterCmd(cmd)
	return
}

func registerSubCmd(cm *CommandManager, parent, child *cobra.Command) (err error) {
	defer func() {
		if t := recover(); t != nil {
			err = fmt.Errorf("%s", t)
		}
	}()
	cm.RegisterSubCmd(parent, child)
	return
}

func setCmdGroup(cm *CommandManager, name string, cmds ...*cobra.Command) (err error) {
	defer func() {
		if t := recover(); t != nil {
			err = fmt.Errorf("%s", t)
		}
	}()
	cm.SetCmdGroup(name, cmds...)
	return
}

func TestCommandManager(t *testing.T) {
	DropPrivilege(t)
	defer ResetPrivilege(t)

	// check panic with nil root command
	_, err := newCommandManager(nil)
	if err == nil {
		t.Errorf("unexpected success with root nil command")
	}
	// create command manager
	cm, err := newCommandManager(rootCmd)
	if err != nil {
		t.Errorf("unexpected error while instantiating new command manager: %err", err)
	}

	// get root command
	if cm.GetRootCmd() != rootCmd {
		t.Errorf("unexpected root command returned")
	}
	// root command name must return an empty string
	if cm.GetCmdName(rootCmd) != "" {
		t.Errorf("unexpected root command name returned")
	}

	// check panic while registering a nil command
	if err := registerCmd(cm, nil); err == nil {
		t.Errorf("unexpected success with nil command")
	}

	// register parent command
	if err := registerCmd(cm, parentCmd); err != nil {
		t.Errorf("unexpected error while registering command: %s", err)
	}
	// get name with command
	if cm.GetCmdName(parentCmd) != "parent" {
		t.Errorf("unexpected command name returned")
	}
	// test with non-existent command name
	if cm.GetCmd("noparent") != nil {
		t.Errorf("unexpected command returned")
	}
	// get parent command by name
	if cm.GetCmd("parent") != parentCmd {
		t.Errorf("unexpected child command returned")
	}

	// check panic with nil parent command
	if err := registerSubCmd(cm, nil, childCmd); err == nil {
		t.Errorf("unexpected success with nil parent command")
	}
	// check panic with nil child command
	if err := registerSubCmd(cm, parentCmd, nil); err == nil {
		t.Errorf("unexpected success with nil child command")
	}
	// check panic with unregistered command
	unregisteredCmd := &cobra.Command{}
	if err := registerSubCmd(cm, unregisteredCmd, childCmd); err == nil {
		t.Errorf("unexpected success while registering sub command with unregistered parent command")
	}

	// register child command for parent command
	if err := registerSubCmd(cm, parentCmd, childCmd); err != nil {
		t.Errorf("unexpected error while registering command: %s", err)
	}
	// get child command by name
	if cm.GetCmd("parent_child") != childCmd {
		t.Errorf("unexpected child command returned")
	}

	// check panic by creating a group with nil command only
	emptyGroup := []*cobra.Command{nil, nil}
	if err := setCmdGroup(cm, "test", emptyGroup...); err == nil {
		t.Errorf("unexpected success while creating group with nil command")
	}

	// create group test with a nil command
	testGroup := []*cobra.Command{parentCmd, childCmd}
	if err := setCmdGroup(cm, "test", testGroup...); err != nil {
		t.Errorf("unexpected error while creating group command: %s", err)
	}
	// check panic by adding an already existing group
	if err := setCmdGroup(cm, "test", testGroup...); err == nil {
		t.Errorf("unexpected success while creating an existing group")
	}

	// check returned command group
	cmdGroup := cm.GetCmdGroup("test")
	if !reflect.DeepEqual(testGroup, cmdGroup) {
		t.Errorf("unexpected group command returned")
	}

	// check get command by name
	if cm.GetCmd("test") != nil {
		t.Errorf("unexpected test command returned")
	}
}

var origUID, origGID, unprivUID, unprivGID int
var origHome, unprivHome string

// DropPrivilege drops privilege. Use this at the start of a test that does
// not require elevated privileges. A matching call to ResetPrivilege must
// occur before the test completes (a defer statement is recommended.)
func DropPrivilege(t *testing.T) {

	// setresuid/setresgid modifies the current thread only. To ensure our new
	// uid/gid sticks, we need to lock ourselves to the current OS thread.
	runtime.LockOSThread()

	if os.Getgid() == 0 {
		if err := syscall.Setresgid(unprivGID, unprivGID, origGID); err != nil {
			t.Fatalf("failed to set group identity: %v", err)
		}
	}
	if os.Getuid() == 0 {
		if err := syscall.Setresuid(unprivUID, unprivUID, origUID); err != nil {
			t.Fatalf("failed to set user identity: %v", err)
		}

		if err := os.Setenv("HOME", unprivHome); err != nil {
			t.Fatalf("failed to set HOME environment variable: %v", err)
		}
	}
}

// ResetPrivilege returns effective privilege to the original user.
func ResetPrivilege(t *testing.T) {
	if err := syscall.Setresuid(origUID, origUID, unprivUID); err != nil {
		t.Fatalf("failed to reset user identity: %v", err)
	}
	if err := syscall.Setresgid(origGID, origGID, unprivGID); err != nil {
		t.Fatalf("failed to reset group identity: %v", err)
	}
	if err := os.Setenv("HOME", origHome); err != nil {
		t.Fatalf("failed to reset HOME environment variable: %v", err)
	}

	runtime.UnlockOSThread()
}

// getUnprivIDs searches recursively up the process parent chain to find a
// process with a non-root UID, then returns the UID and GID of that process.
// Calls os.Exit on error, or if no non-root process is found.
func getUnprivIDs(pid int) (uid int, gid int) {
	if 1 == pid {
		log.Fatal("no unprivileged process found")
	}

	ppid, uid, gid := getProcInfo(pid)
	if uid != 0 {
		return uid, gid
	}
	return getUnprivIDs(ppid)
}

// getProcInfo returns the parent PID, UID, and GID associated with the
// supplied PID. Calls os.Exit on error.
func getProcInfo(pid int) (ppid int, uid int, gid int) {
	f, err := os.Open(fmt.Sprintf("/proc/%v/status", pid))
	if err != nil {
		log.Fatalf("failed to open /proc/%v/status", pid)
	}

	for s := bufio.NewScanner(f); s.Scan(); {
		var temp int
		if n, _ := fmt.Sscanf(s.Text(), "PPid:\t%d", &temp); n == 1 {
			ppid = temp
		}
		if n, _ := fmt.Sscanf(s.Text(), "Uid:\t%d", &temp); n == 1 {
			uid = temp
		}
		if n, _ := fmt.Sscanf(s.Text(), "Gid:\t%d", &temp); n == 1 {
			gid = temp
		}
	}
	return ppid, uid, gid
}

func init() {
	origUID = os.Getuid()
	origGID = os.Getgid()
	origUser, err := user.LookupId(strconv.Itoa(origUID))

	if err != nil {
		log.Fatalf("err: %s", err)
	}

	origHome = origUser.HomeDir

	unprivUID, unprivGID = getUnprivIDs(os.Getpid())
	unprivUser, err := user.LookupId(strconv.Itoa(unprivUID))

	if err != nil {
		log.Fatalf("err: %s", err)
	}

	unprivHome = unprivUser.HomeDir
}
