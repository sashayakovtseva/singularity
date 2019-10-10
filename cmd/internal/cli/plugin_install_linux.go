// Copyright (c) 2018-2019, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package cli

import (
	"fmt"
	"plugin"
	"reflect"

	"github.com/spf13/cobra"
	"github.com/sylabs/singularity/docs"
	"github.com/sylabs/singularity/internal/pkg/sylog"
	"github.com/sylabs/singularity/pkg/cmdline"
	pluginapi "github.com/sylabs/singularity/pkg/plugin"
)

// -n|--name
var pluginName string
var pluginInstallNameFlag = cmdline.Flag{
	ID:           "pluginInstallNameFlag",
	Value:        &pluginName,
	DefaultValue: "",
	Name:         "name",
	ShortHand:    "n",
	Usage:        "name to install the plugin as, defaults to the value in the manifest",
}

func init() {
	cmdManager.RegisterFlagForCmd(&pluginInstallNameFlag, PluginInstallCmd)
}

// PluginInstallCmd takes a compiled plugin.sif file and installs it
// in the appropriate location.
//
// singularity plugin install <path> [-n name]
var PluginInstallCmd = &cobra.Command{
	// PreRun: EnsureRootPriv,
	Run: func(cmd *cobra.Command, args []string) {
		_, err := open(args[0])
		if err != nil {
			sylog.Fatalf("Could not open plugin: %v", err)
		}

		// err := singularity.InstallPlugin(args[0], buildcfg.LIBEXECDIR)
		// if err != nil {
		// 	sylog.Fatalf("Failed to install plugin %q: %s.", args[0], err)
		// }
	},
	DisableFlagsInUseLine: true,
	Args:                  cobra.ExactArgs(1),

	Use:     docs.PluginInstallUse,
	Short:   docs.PluginInstallShort,
	Long:    docs.PluginInstallLong,
	Example: docs.PluginInstallExample,
}

func open(path string) (*pluginapi.Plugin, error) {
	pluginPointer, err := plugin.Open(path)
	if err != nil {
		return nil, err
	}

	pluginObject, err := getPluginObject(pluginPointer)
	if err != nil {
		return nil, err
	}

	return pluginObject, nil
}

func getPluginObject(pl *plugin.Plugin) (*pluginapi.Plugin, error) {
	sym, err := pl.Lookup(pluginapi.PluginSymbol)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Plugin type: %s\n", reflect.TypeOf(sym))
	p, ok := sym.(*pluginapi.Plugin)
	if !ok {
		return nil, fmt.Errorf("symbol \"Plugin\" not of type Plugin")
	}

	return p, nil

}
