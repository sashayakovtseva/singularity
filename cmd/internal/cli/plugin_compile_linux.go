// Copyright (c) 2018-2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sylabs/singularity/docs"
	"github.com/sylabs/singularity/internal/app/singularity"
	"github.com/sylabs/singularity/internal/pkg/sylog"
	"github.com/sylabs/singularity/pkg/cmdline"
)

// -o|--out
var out string
var pluginCompileOutFlag = cmdline.Flag{
	ID:           "pluginCompileOutFlag",
	Value:        &out,
	DefaultValue: "",
	Name:         "out",
	ShortHand:    "o",
}

func init() {
	cmdManager.RegisterFlagForCmd(&pluginCompileOutFlag, PluginCompileCmd)
}

// PluginCompileCmd allows a user to compile a plugin.
//
// singularity plugin compile <path> [<main pkg>] [-o name]
var PluginCompileCmd = &cobra.Command{
	Run: func(cmd *cobra.Command, args []string) {
		sourceDir, err := filepath.Abs(args[0])
		if err != nil {
			sylog.Fatalf("While sanitizing input path: %s", err)
		}

		mainPkg := ""
		if len(args) == 2 {
			mainPkg = args[1]
		}

		destSif := out
		if destSif == "" {
			destSif = sifPath(sourceDir)
		}

		sylog.Debugf("sourceDir: %s; sifPath: %s", sourceDir, destSif)
		if err := singularity.CompilePlugin(sourceDir, mainPkg, destSif, os.TempDir()); err != nil {
			sylog.Fatalf("Plugin compile failed with error: %s", err)
		}
	},
	DisableFlagsInUseLine: true,
	Args:                  cobra.MaximumNArgs(2),

	Use:     docs.PluginCompileUse,
	Short:   docs.PluginCompileShort,
	Long:    docs.PluginCompileLong,
	Example: docs.PluginCompileExample,
}

// sifPath returns the default path where a plugin's resulting SIF file will
// be built to when no custom -o has been set.
//
// The default behavior of this will place the resulting .sif file in the
// same directory as the source code.
func sifPath(sourceDir string) string {
	b := filepath.Base(sourceDir)
	return filepath.Join(sourceDir, b+".sif")
}
