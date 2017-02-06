// Copyright © 2016 NAME HERE <EMAIL ADDRESS>
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
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/twa16/meteor-deploy-system/common"
	"fmt"
	"github.com/fatih/color"
	"time"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: Work your own magic here
		getDeployments()
	},
}

func init() {
	deploymentCmd.AddCommand(listCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// listCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// listCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}

func getDeployments() {
	//Let's build the url
	urlString := viper.GetString("ServerHostname") + "/deployments"
	//Check if the connection should be secure and prepend the proper protocol
	if viper.GetBool("UseHTTPS") {
		urlString = "https://" + urlString
	} else {
		urlString = "http://" + urlString
	}
	//Create the client
	client := &http.Client{}
	r, _ := http.NewRequest("GET", urlString, nil) // <-- URL-encoded payload
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

	for _, deployment := range deployments {
		fmt.Printf("====== Name: %s =====\n", deployment.ProjectName)
		fmt.Printf("Created: %s\n", deployment.Model.CreatedAt.Format(time.RFC822))
		fmt.Printf("URL: %s\n", deployment.URL)
		fmt.Printf("Status: ")
		if deployment.Status != "running" {
			color.Red("%s\n\n", deployment.Status)
		} else {
			color.Green("%s\n\n", deployment.Status)
		}
	}

}
