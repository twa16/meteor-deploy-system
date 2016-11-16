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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/viper"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/twa16/meteor-deploy-system/common"
	"golang.org/x/crypto/ssh/terminal"
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect the mds cli to a server",
	Long: `Use this command to specify how to connect to an MDS server
	connect [hostname]
	`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Println("Please provide a host to connect to")
			return
		}
		if viper.GetString("AuthToken") != "" {
			fmt.Println("Session exists.")
			return
		}
		fmt.Println("Attempting to connect to: " + args[0])
		data := url.Values{}

		username, password := credentials()
		data.Set("username", username)
		data.Add("password", password)

		login(args[0], data, false)
	},
}

func credentials() (string, string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Username: ")
	username, _ := reader.ReadString('\n')

	fmt.Print("Enter Password: ")
	bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		panic("Failed to get password")
	}
	password := string(bytePassword)

	return strings.TrimSpace(username), strings.TrimSpace(password)
}

func login(hostname string, data url.Values, secure bool) {
	//Let's build the url
	urlString := hostname + "/login"
	//Check if the connection should be secure and prepend the proper protocol
	if secure {
		urlString = "https://" + urlString
	} else {
		urlString = "http://" + urlString
	}
	//Create the client
	client := &http.Client{}
	r, _ := http.NewRequest("POST", urlString, bytes.NewBufferString(data.Encode())) // <-- URL-encoded payload
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))

	//Send the data and get the response
	resp, err := client.Do(r)
	if err != nil {
		panic(err)
	}
	//Get the body of the response as a string
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	//Convert the JSON into an AutenticationToken struct
	var authenticationToken mds.AuthenticationToken
	if err := json.Unmarshal(buf.Bytes(), &authenticationToken); err != nil {
		panic(err)
	}
	viper.Set("AuthenticationToken", authenticationToken.AuthenticationToken)
	err = ioutil.WriteFile(viper.GetString("HomeDirectory")+"/.mds-session", []byte(authenticationToken.AuthenticationToken), 0644)
	if err != nil {
		panic(err)
	}
	fmt.Println("Saved Session.")
}

func init() {
	RootCmd.AddCommand(connectCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// connectCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// connectCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}
