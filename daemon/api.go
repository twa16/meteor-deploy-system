/*
 * Copyright 2017 Manuel Gauto (github.com/twa16)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
*/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"goji.io"
	"goji.io/pat"
	"golang.org/x/crypto/bcrypt"
	"github.com/twa16/meteor-deploy-system/common"
	"strconv"
	"github.com/twa16/go-auth"
)

var dClient *docker.Client
var database *gorm.DB

const (
	CreateDeploymentPermission = "deployment.create"
	ListDeploymentPermission = "deployment.list"
	DeleteDeploymentPermission = "deployment.delete"
)

func ping(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "PONG")
}

//Called when /loginAPIHandler is called
func loginAPIHandler(w http.ResponseWriter, r *http.Request) {
	//Process the query parameters
	r.ParseForm()
	//Ensure the proper parameters were sent
	if len(r.Form["username"]) == 0 || len(r.Form["password"]) == 0 {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "Record Not Found")
		return
	}
	if len(r.Form["persistent"]) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Persistence setting not set")
		return
	}
	var isPersistent = r.Form["persistent"][0] == "true"
	token, err := handleLoginAttempt(r.Form["username"][0], r.Form["password"][0], isPersistent)
	if err != nil {
		fmt.Fprint(w, err.Error())
		return
	}
	jsonBytes, _ := json.Marshal(token)
	fmt.Fprintf(w, string(jsonBytes))
}

func handleLoginAttempt(username string, password string, persistentToken bool) (simpleauth.Session, error) {
	user, err := getUser(username)
	//Make sure the user exists
	if err != nil {
		log.Warningf("Error retrieving user during loginAPIHandler: %s \n", err)
		//These errors to the user are intentionally vague
		return simpleauth.Session{}, errors.New("Username or password incorrect")
	}
	//Check the password against the user object
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) != nil {
		//The passwords did not match
		//These errors to the user are intentionally vague
		return simpleauth.Session{}, errors.New("Username or password incorrect")
	}
	token, err := authProvider.GenerateSessionKey(user.ID, persistentToken)

	log.Infof("User Authentication Request Granted for %s\n", username)
	//Save it and return it
	database.Create(&token)
	return token, nil
}

//CreateDeployment Called when POST /deployment is called
func createDeploymentEndpoint(w http.ResponseWriter, r *http.Request) {
	//Check authentication
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], CreateDeploymentPermission)
	if authCode == 0 {
		//Process the query parameters
		r.ParseForm()

		//Check if they sent a projectName
		if len(r.Form["projectname"]) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Please provide a project name")
			return
		}
		//Get the project name they want
		projectName := r.Form["projectname"][0]

		//Handle file upload t get the archive of the application
		r.ParseMultipartForm(32 << 20)
		file, _, err := r.FormFile("uploadfile")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer file.Close()
		//Get destination directory
		destination, err := GetNewApplicationDirectory()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Internal Server Error")
			log.Criticalf("Error creating new application directory: %s", err.Error())
			return
		}
		//Copy tarball to volume
		//Create destination
		desFile, err := os.Create(destination + "/application.tar.gz")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Internal Server Error")
			log.Criticalf("Failed to create destination file(%s): %s", destination, err.Error())
			return
		}
		defer desFile.Close()
		//Copy content
		_, err = io.Copy(desFile, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Internal Server Error")
			log.Criticalf("Failed to copy tarball to volume: %s", err.Error())
			return
		}
		//Sync
		err = desFile.Sync()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Internal Server Error")
			log.Criticalf("Failed to copy tarball to volume: %s", err.Error())
			return
		}

		//Get custom environmental Variables
		data := r.Form["Env-Var"]
		customEnvironmentalVariables := make([]string, 1)
		for _, entry := range data {
			log.Debugf("Got custom environmental variable: %s", entry)
			customEnvironmentalVariables = append(customEnvironmentalVariables, entry)
		}
		//Start creating deployment
		deployment, err := createDeployment(dClient, database, projectName, destination, r.Form["settings"][0], customEnvironmentalVariables)
		if err != nil {
			fmt.Fprintf(w,"Error: %s\n", err.Error())
		}
		fmt.Fprint(w, "Created: %s(ID: %d)\n", deployment.ProjectName, deployment.ID)
	} else if authCode == 2 {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, "Token Expired")
	} else {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, "Unauthorized")
	}
}

//GetNewApplicationDirectory Returns a new path for the application files.
func GetNewApplicationDirectory() (string, error) {
	var destination string
	for i := 0; i < 100; i++ {
		attempt := viper.GetString("ApplicationDirectory") + uuid.NewV4().String()
		exists, err := pathExists(attempt)
		if err != nil {
			log.Warning(err)
		}
		if !exists {
			destination = attempt
		}
	}
	//Just a safegaurd. Die if you cannot create a new directory.
	if destination == "" {
		log.Fatal("Failed to create directory for application.")
		panic("Could not create directory")
	}

	//Make sure the path is a full path
	volumePath, err := filepath.Abs(destination)
	if err != nil {
		log.Criticalf("Failed to expand path: %s", destination)
		return "", err
	}
	//Create directory
	err = os.Mkdir(volumePath, 0774)
	if err != nil {
		log.Criticalf("Failed to create directory: %s", destination)
		return "", err
	}
	return volumePath, nil
}

//Checks if a path exists
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

//Called when /deployments is called
func getDeploymentsAPIHandler(w http.ResponseWriter, r *http.Request) {
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], ListDeploymentPermission)
	if authCode == 0 {
		var deployments []mds.Deployment
		database.Find(&deployments)
		for i, deployment := range deployments {
			inspectResult, err := InspectDeployment(dClient, database, deployment.ID)
			if err != nil {
				log.Warning(err)
			}
			deployments[i] = *inspectResult
		}
		jsonBytes, _ := json.Marshal(deployments)
		fmt.Fprintf(w, string(jsonBytes))
	} else if authCode == 2 {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, "Token Expired")
	} else {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Unauthorized")
	}
}

func updateDeploymentAPIHandler(w http.ResponseWriter, r *http.Request) {
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], ListDeploymentPermission)
	if authCode == 0 {
		//Process the query parameters
		r.ParseForm()

		//Check if they sent a projectName
		if len(r.Form["projectid"]) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Please provide a project name")
			return
		}
		//Get the project name they want
		projectidstring := r.Form["projectid"][0]
		projectId, err := strconv.Atoi(projectidstring)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Invalid Deployment ID")
			return
		}

		//Handle file upload t get the archive of the application
		r.ParseMultipartForm(32 << 20)
		file, _, err := r.FormFile("uploadfile")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer file.Close()
		//Get destination directory
		destination, err := GetNewApplicationDirectory()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal Server Error")
			log.Criticalf("Error creating new application directory: %s", err.Error())
			return
		}
		//Copy tarball to volume
		//Create destination
		desFile, err := os.Create(destination + "/application.tar.gz")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal Server Error")
			log.Criticalf("Failed to create destination file(%s): %s", destination, err.Error())
			return
		}
		defer desFile.Close()
		//Copy content
		_, err = io.Copy(desFile, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal Server Error")
			log.Criticalf("Failed to copy tarball to volume: %s", err.Error())
			return
		}
		//Sync
		err = desFile.Sync()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal Server Error")
			log.Criticalf("Failed to copy tarball to volume: %s", err.Error())
			return
		}

		//Get custom environmental Variables
		data := r.Form["Env-Var"]
		customEnvironmentalVariables := make([]string, 1)
		for _, entry := range data {
			log.Debugf("Got custom environmental variable: %s", entry)
			customEnvironmentalVariables = append(customEnvironmentalVariables, entry)
		}
		//Start creating deployment
		updateDeployment(dClient, database, projectId, destination, r.Form["settings"][0], customEnvironmentalVariables)
		fmt.Fprint(w, "")
	} else if authCode == 2 {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Token Expired")
	} else {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Unauthorized")
	}
}

//Called when DELETE /deployment is called an id should be passed as a query parameter
func deleteDeploymentAPIHandler(w http.ResponseWriter, r *http.Request) {
	//TODO: Tear this apart and redo it
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], DeleteDeploymentPermission)
	if authCode == 0 {
		//Process the query parameters
		r.ParseForm()
		//Make sure an id was submitted
		if len(r.Form["id"]) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "Invalid Request")
			return
		}
		//Get the id of the item to remove and retrieve the item
		var id = r.Form["id"][0]
		var deployment mds.Deployment
		recordExists := database.First(&deployment, id).RecordNotFound()
		//Check to see if the item exists
		if recordExists {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "Record Not Found")
			return
		}

		//=====Delete Logic=====
		//Stop the container
		err := dClient.StopContainer(deployment.ContainerID, 10)
		if err != nil {
			log.Warning(err)
			//w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, err.Error()+"\n")
			//return
		}
		//Remove the container
		err = removeContainer(dClient, deployment.ContainerID)
		if err != nil {
			log.Warning(err)
			//w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, err.Error()+"\n")
			//return
		}
		//Delete Record
		database.Delete(&deployment)
		fmt.Fprint(w, "Deleted")
	} else if authCode == 2 {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Token Expired")
	} else {
		//Unauthorized 401
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Unauthorized")
	}
}

// Checks authentication
// Returns 0 if ok, 1 if unauthorized, 2 if expired session
func checkAuthentication(db *gorm.DB, key string, permissionNeeded string) int {
	//Get the auth token
	var authenticationKey simpleauth.Session
	db.Where("authentication_token=?", key).Find(&authenticationKey).First(&authenticationKey)

	//If the token hasn't been used in a week. Force a relogin.
	if authenticationKey.Persistent == false && (time.Now().Unix() - authenticationKey.LastSeen) > (60 * 60 * 24 * 7) {
		return 2
	}

	//Get user of the token
	var user simpleauth.User
	db.First(&user, authenticationKey.AuthUserID)

	hasPermission, err := authProvider.CheckPermission(user.ID, permissionNeeded)
	if err != nil {
		log.Warning("Failed to check user permission for %d: %s\n", user.ID, err.Error())
		return 1
	}
	if hasPermission {
		return 0
	}

	//Otherwise, they are unauthorized
	return 1
}

func startAPI(dockerParam *docker.Client, db *gorm.DB) {
	dClient = dockerParam
	database = db
	mux := goji.NewMux()
	mux.HandleFunc(pat.Get("/ping"), ping)
	mux.HandleFunc(pat.Get("/deployments"), getDeploymentsAPIHandler)
	mux.HandleFunc(pat.Delete("/deployments"), deleteDeploymentAPIHandler)
	mux.HandleFunc(pat.Post("/deployments"), createDeploymentEndpoint)
	mux.HandleFunc(pat.Put("/deployments"), updateDeploymentAPIHandler)
	mux.HandleFunc(pat.Post("/login"), loginAPIHandler)

	apiCertFile := viper.GetString("ApiHttpsCertificate")
	apiKeyFile := viper.GetString("ApiHttpsKey")
	log.Fatal(http.ListenAndServeTLS(":8000", apiCertFile, apiKeyFile, mux))
}
