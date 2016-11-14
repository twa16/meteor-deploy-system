package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	"github.com/twa16/meteor-deploy-system/common"
	"goji.io"
	"goji.io/pat"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"time"
)

var sessionExpireTime = 3600 //Session expire time in seconds

var dClient *docker.Client
var database *gorm.DB

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
		fmt.Fprintf(w, "Record Not Found")
		return
	}
	token, err := handleLoginAttempt(r.Form["username"][0], r.Form["password"][0])
	if err != nil {
		fmt.Fprint(w, err.Error())
		return
	}
	jsonBytes, _ := json.Marshal(token)
	fmt.Fprintf(w, string(jsonBytes))
}

func handleLoginAttempt(username string, password string) (mds.AuthenticationToken, error) {
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
	token.AuthenticationToken = randStr(32)
	token.UserID = user.ID
	token.LastSeen = time.Now().Unix()

	//Save it and return it
	database.Create(&token)
	return token, nil
}

// Called when /containers is called
func getContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := dClient.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		log.Critical(err)
		fmt.Fprintf(w, "Error getting containers")
	} else {
		jsonResponseBytes, _ := json.Marshal(containers)
		fmt.Fprintf(w, string(jsonResponseBytes))
	}
}

//Called when /deployments is called
func getDeployments(w http.ResponseWriter, r *http.Request) {
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
}

//Called when DELETE /deployment is called an id should be passed as a query parameter
func deleteDeployment(w http.ResponseWriter, r *http.Request) {
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

	//TODO: Add auth logic

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
	err = removeContainer(dClient, id)
	if err != nil {
		log.Warning(err)
		//w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, err.Error()+"\n")
		//return
	}
	//Delete Record
	database.Delete(&deployment)
	fmt.Fprintf(w, "Deleted")
}

func startAPI(dockerParam *docker.Client, db *gorm.DB) {
	dClient = dockerParam
	database = db
	mux := goji.NewMux()
	mux.HandleFunc(pat.Get("/ping"), ping)
	mux.HandleFunc(pat.Get("/containers"), getContainers)
	mux.HandleFunc(pat.Get("/deployments"), getDeployments)
	mux.HandleFunc(pat.Delete("/deployment"), deleteDeployment)
	mux.HandleFunc(pat.Post("/login"), login)

	log.Fatal(http.ListenAndServe(":8000", mux))
}
