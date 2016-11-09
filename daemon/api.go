package main

import (
	"encoding/json"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	"goji.io"
	"goji.io/pat"
	"net/http"
)

var dClient *docker.Client
var database *gorm.DB

func ping(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "PONG")
}

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

func getDeployments(w http.ResponseWriter, r *http.Request) {
	var deployments []Deployment
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
	var deployment Deployment
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

	log.Info("API Starting")
	log.Fatal(http.ListenAndServe(":8000", mux))
}
