// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package main

import (
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/sylabs/singularity/src/pkg/test"
)

func imageVerify(t *testing.T, imagePath string, labels bool) {
	type testSpec struct {
		name          string
		execArgs      []string
		expectSuccess bool
	}
	tests := []testSpec{
		{"False", []string{"false"}, false},
		{"RunScript", []string{"test", "-f", "/.singularity.d/runscript"}, true},
		{"OneBase", []string{"test", "-f", "/.singularity.d/env/01-base.sh"}, true},
		{"ActionsShell", []string{"test", "-f", "/.singularity.d/actions/shell"}, true},
		{"ActionsExec", []string{"test", "-f", "/.singularity.d/actions/exec"}, true},
		{"ActionsRun", []string{"test", "-f", "/.singularity.d/actions/run"}, true},
		{"Environment", []string{"test", "-L", "/environment"}, true},
		{"Singularity", []string{"test", "-L", "/singularity"}, true},
	}
	if labels && *runDisabled { // TODO
		tests = append(tests, testSpec{"Labels", []string{"test", "-f", "/.singularity.d/labels.json"}, true})
	}

	for _, tt := range tests {
		t.Run(tt.name, test.WithoutPrivilege(func(t *testing.T) {
			_, stderr, exitCode, err := imageExec(t, "exec", opts{}, imagePath, tt.execArgs)
			if tt.expectSuccess && (exitCode != 0) {
				t.Log(stderr)
				t.Fatalf("unexpected failure running '%v': %v", strings.Join(tt.execArgs, " "), err)
			} else if !tt.expectSuccess && (exitCode != 1) {
				t.Log(stderr)
				t.Fatalf("unexpected success running '%v'", strings.Join(tt.execArgs, " "))
			}
		}))
	}
}

type buildOpts struct {
	force    bool
	sandbox  bool
	writable bool
	env      []string
}

func imageBuild(opts buildOpts, imagePath, buildSpec string) ([]byte, error) {
	var argv []string
	argv = append(argv, "build")
	if opts.force {
		argv = append(argv, "--force")
	}
	if opts.sandbox {
		argv = append(argv, "--sandbox")
	}
	if opts.writable {
		argv = append(argv, "--writable")
	}
	argv = append(argv, imagePath, buildSpec)

	cmd := exec.Command(cmdPath, argv...)
	cmd.Env = opts.env

	return cmd.CombinedOutput()
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
		buildSpec  string
		sandbox    bool
		writable   bool
	}{
		{"BusyBox", "", "../../../examples/busybox/Singularity", false, false},
		{"BusyBoxSandbox", "", "../../../examples/busybox/Singularity", true, false},
		{"BusyBoxWritable", "", "../../../examples/busybox/Singularity", true, true},
		{"Debootstrap", "debootstrap", "../../../examples/debian/Singularity", true, false},
		{"DockerURI", "", "docker://busybox", true, false},
		{"DockerDefFile", "", "../../../examples/docker/Singularity", true, false},
		{"SHubURI", "", "shub://GodloveD/busybox", true, false},
		{"SHubDefFile", "", "../../../examples/shub/Singularity", true, false},
		{"LibraryDefFile", "", "../../../examples/library/Singularity", true, false},
		{"Yum", "yum", "../../../examples/centos/Singularity", true, false},
		{"Zypper", "zypper", "../../../examples/opensuse/Singularity", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, test.WithPrivilege(func(t *testing.T) {
			if tt.dependency != "" {
				if _, err := exec.LookPath(tt.dependency); err != nil {
					t.Skipf("%v not found in path", tt.dependency)
				}
			}

			opts := buildOpts{
				sandbox:  tt.sandbox,
				writable: tt.writable,
			}

			imagePath := path.Join(testDir, "container")
			defer os.RemoveAll(imagePath)

			if b, err := imageBuild(opts, imagePath, tt.buildSpec); err != nil {
				t.Log(string(b))
				t.Fatalf("unexpected failure: %v", err)
			}
			imageVerify(t, imagePath, false)
		}))
	}
}

func TestBuildMultiStage(t *testing.T) {
	imagePath1 := path.Join(testDir, "container1")
	imagePath2 := path.Join(testDir, "container2")
	imagePath3 := path.Join(testDir, "container3")

	liDefFile := prepareDefFile(DefFileDetail{
		Bootstrap: "localimage",
		From:      imagePath1,
	})
	defer os.Remove(liDefFile)

	labels := make(map[string]string)
	labels["FOO"] = "bar"
	liLabelDefFile := prepareDefFile(DefFileDetail{
		Bootstrap: "localimage",
		From:      imagePath2,
		Labels:    labels,
	})
	defer os.Remove(liLabelDefFile)

	type testSpec struct {
		name      string
		imagePath string
		buildSpec string
		force     bool
		sandbox   bool
		writable  bool
		labels    bool
	}

	tests := []struct {
		name  string
		steps []testSpec
	}{
		{"SIFToSIF", []testSpec{
			{"BusyBox", imagePath1, "../../../examples/busybox/Singularity", false, false, false, false},
			{"SIF", imagePath2, imagePath1, false, false, false, false},
		}},
		{"SandboxToSIF", []testSpec{
			{"BusyBoxSandbox", imagePath1, "../../../examples/busybox/Singularity", false, true, false, false},
			{"SIF", imagePath2, imagePath1, false, false, false, false},
		}},
		{"WritableToSIF", []testSpec{
			{"BusyBoxWritable", imagePath1, "../../../examples/busybox/Singularity", false, false, true, false},
			{"SIF", imagePath2, imagePath1, false, false, false, false},
		}},
		{"LocalImage", []testSpec{
			{"BusyBox", imagePath1, "../../../examples/busybox/Singularity", false, false, false, false},
			{"LocalImage", imagePath2, liDefFile, false, false, false, false},
			{"LocalImageLabel", imagePath3, liLabelDefFile, false, false, false, true},
		}},
		{"LocalImageSandbox", []testSpec{
			{"BusyBoxSandbox", imagePath2, "../../../examples/busybox/Singularity", true, true, false, false},
			{"LocalImageLabel", imagePath3, liLabelDefFile, false, false, false, true},
		}},
		{"LocalImageWritable", []testSpec{
			{"BusyBoxWritable", imagePath2, "../../../examples/busybox/Singularity", false, false, true, false},
			{"LocalImageLabel", imagePath3, liLabelDefFile, false, false, false, true},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, test.WithPrivilege(func(t *testing.T) {
			for _, ts := range tt.steps {
				defer os.RemoveAll(ts.imagePath)
			}

			for _, ts := range tt.steps {
				t.Run(ts.name, test.WithPrivilege(func(t *testing.T) {
					opts := buildOpts{
						force:    ts.force,
						sandbox:  ts.sandbox,
						writable: ts.writable,
					}

					if b, err := imageBuild(opts, ts.imagePath, ts.buildSpec); err != nil {
						t.Log(string(b))
						t.Fatalf("unexpected failure: %v", err)
					}
					imageVerify(t, ts.imagePath, ts.labels)
				}))
			}
		}))
	}
}

func TestBadPath(t *testing.T) {
	test.EnsurePrivilege(t)

	imagePath := path.Join(testDir, "container")
	defer os.RemoveAll(imagePath)

	if b, err := imageBuild(buildOpts{}, imagePath, "/some/dumb/path"); err == nil {
		t.Log(string(b))
		t.Fatal("unexpected success")
	}
}
