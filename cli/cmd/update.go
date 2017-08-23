// Copyright © 2017 Manuel Gauto <github.com/twa16>
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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update a deployment",
	Long: `Update a deployment.`,
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: Work your own magic here
		fmt.Println("update called")
	},
}

type DeploymentUpdateData struct {
	projectName string
	tarballPath string
	settingsPath string
	envVars []string
}
var updatedProject DeploymentUpdateData

func init() {
	deploymentCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVar(&updatedProject.projectName, "name", "", "Name of the project")
	updateCmd.Flags().StringVarP(&updatedProject.tarballPath, "tarball", "p", "", "Path to the project tarball")
	updateCmd.Flags().StringVarP(&updatedProject.settingsPath, "settings", "s", "", "Path to the settings file")
	updateCmd.Flags().StringSliceVarP(&updatedProject.envVars, "env", "e", []string{}, "Environment variables to pass to container. (FOO=BAR,KEY=VALUE)")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// updateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// updateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}
