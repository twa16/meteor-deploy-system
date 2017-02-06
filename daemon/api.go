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

//Called when /login is called
func login(w http.ResponseWriter, r *http.Request) {
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

func handleLoginAttempt(username string, password string, persistentToken bool) (mds.AuthenticationToken, error) {
	user, err := getUser(database, username)
	//Make sure the user exists
	if err != nil {
		log.Warningf("Error retrieving user during login: %s \n", err)
		//These errors to the user are intentionally vague
		return mds.AuthenticationToken{}, errors.New("Username or password incorrect")
	}
	//Check the password against the user object
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) != nil {
		//The passwords did not match
		//These errors to the user are intentionally vague
		return mds.AuthenticationToken{}, errors.New("Username or password incorrect")
	}
	//Let's create a token
	token := mds.AuthenticationToken{}
	tokenGen, _ := GenerateRandomString(32)
	token.AuthenticationToken = tokenGen
	token.UserID = user.ID
	token.LastSeen = time.Now().Unix()

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
		createDeployment(dClient, database, projectName, destination, r.Form["settings"][0], customEnvironmentalVariables)
		fmt.Fprintf(w, "")
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
func getDeployments(w http.ResponseWriter, r *http.Request) {
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], ListDeploymentPermission)
	if authCode == 0 {
		var deployments []mds.Deployment
		database.Find(&deployments)
		for i, deployment := range deployments {
			inspectResult, err := inspectDeployment(dClient, database, deployment.ID)
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
		fmt.Fprintf(w, "Unauthorized")
	}
}

//Called when DELETE /deployment is called an id should be passed as a query parameter
func deleteDeployment(w http.ResponseWriter, r *http.Request) {
	//TODO: Tear this apart and redo it
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], DeleteDeploymentPermission)
	if authCode == 0 {
		//Process the query parameters
		r.ParseForm()
		//Make sure an id was submitted
		if len(r.Form["id"]) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid Request")
			return
		}
		//Get the id of the item to remove and retrieve the item
		var id = r.Form["id"][0]
		var deployment mds.Deployment
		recordExists := database.First(&deployment, id).RecordNotFound()
		//Check to see if the item exists
		if recordExists {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "Record Not Found")
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
		fmt.Fprintf(w, "Deleted")
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

// Checks authentication
// Returns 0 if ok, 1 if unauthorized, 2 if expired session
func checkAuthentication(db *gorm.DB, key string, permissionNeeded string) int {
	//Get the auth token
	var authenticationKey mds.AuthenticationToken
	db.Where("authentication_token=?", key).Find(&authenticationKey).First(&authenticationKey)

	//If the token hasn't been used in a week. Force a relogin.
	if authenticationKey.Persistent == false && (time.Now().Unix() - authenticationKey.LastSeen) > (60 * 60 * 24 * 7) {
		return 2
	}

	//Get user of the token
	var user mds.User
	db.First(&user, authenticationKey.UserID)
	db.Model(&user).Related(&user.Permissions)

	for _, permission := range user.Permissions {
		//*.* should be good for everything
		if permission.Permission == "*.*" {
			updateLastSeen(db, authenticationKey)
			return 0
		}
		//Check for a match, return 0 if it exists
		if permission.Permission == permissionNeeded {
			updateLastSeen(db, authenticationKey)
			return 0
		}
	}

	//Otherwise, they are unauthorized
	return 1
}

// Updates the lastseen field on the AuthenticationToken and saves it to the DB
func updateLastSeen(db *gorm.DB, authenticationKey mds.AuthenticationToken) {
	authenticationKey.LastSeen = time.Now().Unix()
	db.Save(authenticationKey)
}

func startAPI(dockerParam *docker.Client, db *gorm.DB) {
	dClient = dockerParam
	database = db
	mux := goji.NewMux()
	mux.HandleFunc(pat.Get("/ping"), ping)
	mux.HandleFunc(pat.Get("/deployments"), getDeployments)
	mux.HandleFunc(pat.Delete("/deployment"), deleteDeployment)
	mux.HandleFunc(pat.Post("/deployment"), createDeploymentEndpoint)
	mux.HandleFunc(pat.Post("/login"), login)

	apiCertFile := viper.GetString("ApiHttpsCertificate")
	apiKeyFile := viper.GetString("ApiHttpsKey")
	log.Fatal(http.ListenAndServeTLS(":8000", apiCertFile, apiKeyFile, mux))
}
