// Copyright 2019 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"

	"github.com/maistra/ior/pkg/bootstrap"
	"github.com/maistra/ior/pkg/galley"
	"github.com/maistra/ior/pkg/version"
	"github.com/spf13/cobra"
	"istio.io/pkg/log"
)

var (
	loggingOptions = log.DefaultOptions()
	cliArgs        = bootstrap.DefaultArgs()
)

func newRootCmd() *cobra.Command {

	rootCmd := &cobra.Command{
		Use:   "ior",
		Short: "Connects to Galley and manages OpenShift Routes based on Istio Gateways",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}

			log.Infof("Starting IOR %s", version.Info)

			g, err := galley.New(cliArgs)
			if err != nil {
				return err
			}

			return g.Run()
		},
	}

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.PersistentFlags().StringVarP(&cliArgs.McpAddr, "mcp-address", "", cliArgs.McpAddr,
		"Galley's MCP server address")
	rootCmd.PersistentFlags().StringVarP(&cliArgs.Namespace, "namespace", "n", cliArgs.Namespace,
		"Namespace where the control plane resides. If not set, uses ${POD_NAMESPACE} environment variable")

	loggingOptions.AttachCobraFlags(rootCmd)

	rootCmd.AddCommand(version.GetVersionCmd())
	cliArgs.CredentialOptions.AttachCobraFlags(rootCmd)
	return rootCmd
}
