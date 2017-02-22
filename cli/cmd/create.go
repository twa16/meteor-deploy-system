// Copyright © 2016 Manuel Gauto (mgauto@mgenterprises.org)
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"net/http"
	"bytes"
	"github.com/spf13/viper"
	"mime/multipart"
	"os"
	"io"
	"net/url"
	"bufio"
	"strings"
	"github.com/k0kubun/pp"
	"io/ioutil"
	"crypto/tls"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create [path to tarball] [path to settings.json]",
	Short: "Create a deployment",
	Long: `Creates a deployment on the MDS server`,
	Run: func(cmd *cobra.Command, args []string) {

		type ProjectData struct {
			projectName string
			tarballPath string
			settingsPath string
			envVars []string
		}
		var project ProjectData
		//Check if the files have been passed as parameters
		if len(args) > 0 {
			if len(args) > 0 {
				project.tarballPath = args[0]
				if _, err := os.Stat(project.tarballPath); os.IsNotExist(err) {
					fmt.Println("The specified project tarball does not exist")
				}
			}
			if len(args) > 1 {
				project.settingsPath = args[1]
				if _, err := os.Stat(project.settingsPath); os.IsNotExist(err) {
					fmt.Println("The specified settings file does not exist")
				}
			}
		}

		//Get input for other parameters
		reader := bufio.NewReader(os.Stdin)

		//Get file paths if needed
		if project.tarballPath == "" {
			fmt.Print("Path to Tarball: ")
			project.tarballPath, _ = reader.ReadString('\n')
			project.tarballPath = strings.TrimSpace(project.tarballPath)
			if _, err := os.Stat(project.tarballPath); os.IsNotExist(err) {
				fmt.Println("The specified project tarball does not exist")
			}
		}
		if project.settingsPath == "" {
			fmt.Print("Path to Settings Json(optional): ")
			project.settingsPath, _ = reader.ReadString('\n')
			project.settingsPath = strings.TrimSpace(project.settingsPath)
			if project.settingsPath != "" {
				if _, err := os.Stat(project.settingsPath); os.IsNotExist(err) {
					fmt.Println("The specified project tarball does not exist")
				}
			}
		}

		//Get Project Name
		fmt.Print("Enter Project Name: ")
		project.projectName, _ = reader.ReadString('\n')
		project.projectName = strings.TrimSpace(project.projectName)

		//Get Env Variables
		fmt.Println("Please enter environmental variables as KEY=VALUE. If you are finished, enter 'done' as the value.")
		//Loop until the user is done
		for true {
			fmt.Print("Enter EnvVar(KEY=VALUE): ")
			envVar, _ := reader.ReadString('\n')
			envVar = strings.TrimSpace(envVar)
			//Check if the input is our escape word
			if strings.ToLower(envVar) == "done" {
				break
			}
			//Ignore blanks
			if envVar != "" {
				//Otherwise, add it to our array
				project.envVars = append(project.envVars, envVar)
				//Print what has been entered
				fmt.Println("Current Env Vars:")
				for _, val := range(project.envVars) {
					fmt.Println("  "+val)
				}
				fmt.Print("\nEnter 'done' when finished.\n\n")
			}
		}

		pp.Println(project)
		fmt.Print("Do you wish to create this project. Please type in 'yes' or 'no': ")
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(input) == "yes" {
			fmt.Print("Processing Settings File ")
			settingBytes, err := ioutil.ReadFile(project.settingsPath)
			if err != nil {
				fmt.Println("     FAIL!")
				fmt.Println("Error: "+err.Error())
				return
			}
			fmt.Println("     OK.")

			createDeployment(project.tarballPath, project.projectName, string(settingBytes), project.envVars)
		}
	},
}

func createDeployment(pathToTarball string, projectName string, settings string, envVars []string) {
	//Let's build the url
	hostname := viper.GetString("ServerHostname")
	isSecure := viper.GetBool("UseHTTPS")
	authToken := viper.GetString("AuthenticationToken")

	urlString := hostname + "/deployment"
	//Check if the connection should be secure and prepend the proper protocol
	if isSecure {
		urlString = "https://" + urlString
	} else {
		urlString = "http://" + urlString
	}

	//Parse URL
	reqURL, err := url.Parse(urlString)
	if err != nil {
		fmt.Println("Invalid URL Specified!")
		return
	}

	//Store projectname in URL
	q := reqURL.Query()
	q.Add("projectname", projectName)
	//Store the file and settings as byte buffer for body
	data, fw := createForm(settings, pathToTarball, envVars)

	//Create the client
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: viper.GetBool("IgnoreSSLErrors")},
	}
	client := &http.Client{Transport: tr}
	r, _ := http.NewRequest("POST", urlString, data)
	r.URL.RawQuery = q.Encode()
	r.Header.Add("Content-Type", fw.FormDataContentType())
	r.Header.Add("X-Auth-Token", authToken)
	fmt.Println("Creating Deployment...")
	resp, err := client.Do(r)
	if err != nil {
		fmt.Println("Failed to create deployment")
		fmt.Errorf("Error: %s\n", err.Error())
		os.Exit(1)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	fmt.Println("output: "+buf.String())
}

func createForm(settings string, file string, envVars []string) (*bytes.Buffer, *multipart.Writer) {
	// Prepare a form that you will submit to that URL.
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	// Add your image file
	f, err := os.Open(file)
	if err != nil {
		return nil, w
	}
	defer f.Close()
	fw, err := w.CreateFormFile("uploadfile", file)
	if err != nil {
		return nil, w
	}
	if _, err = io.Copy(fw, f); err != nil {
		return nil, w
	}
	// Add the settings
	if fw, err = w.CreateFormField("settings"); err != nil {
		return nil, w
	}
	if _, err = fw.Write([]byte(settings)); err != nil {
		return nil, w
	}

	//Add any env vars
	for _, envVar := range(envVars) {
		if fw, err = w.CreateFormField("Env-Var"); err != nil {
			return nil, w
		}
		if _, err = fw.Write([]byte(envVar)); err != nil {
			return nil, w
		}
	}

	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	w.Close()
	return &b, w
}

func init() {
	deploymentCmd.AddCommand(createCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// createCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// createCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}
