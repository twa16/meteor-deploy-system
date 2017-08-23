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
	"github.com/spf13/viper"
	"crypto/tls"
	"bytes"
	"github.com/twa16/meteor-deploy-system/common"
	"encoding/json"
	"github.com/olekukonko/tablewriter"
	"os"
	"net/http"
	"strconv"
)

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: Work your own magic here
		fmt.Println("delete called")
	},
}

func init() {
	deploymentCmd.AddCommand(deleteCmd)

	deleteCmd.Flags().StringVar(&project.projectName, "name", "", "Name of the project")
}

func deleteDeployment() {
	if viper.GetBool("HasSession") != true {
		return
	}
	//Let's build the url
	urlString := viper.GetString("ServerHostname") + "/deployments"
	//Check if the connection should be secure and prepend the proper protocol
	if viper.GetBool("UseHTTPS") {
		urlString = "https://" + urlString
	} else {
		urlString = "http://" + urlString
	}
	//Create the client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: viper.GetBool("IgnoreSSLErrors")},
	}
	client := &http.Client{Transport: tr}
	r, _ := http.NewRequest("DELETE", urlString, nil) // <-- URL-encoded payload
	r.Header.Add("X-Auth-Token", viper.GetString("AuthenticationToken"))

	//Send the data and get the response
	resp, err := client.Do(r)
	if err != nil {
		panic(err)
	}
	//Get the body of the response as a string
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	//Convert the JSON into an AutenticationToken struct
	var deployments []mds.Deployment
	if err = json.Unmarshal(buf.Bytes(), &deployments); err != nil {
		panic(err)
	}

	fmt.Printf("Got %d Deployments\n", len(deployments))
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Name", "URL", "State"})
	for _, deployment := range deployments {
		line := []string{
			strconv.Itoa(int(deployment.ID)),
			deployment.ProjectName,
			deployment.URL,
			deployment.Status,
		}
		table.Append(line)
	}
	table.Render()
}