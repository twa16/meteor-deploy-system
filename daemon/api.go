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
		inspectResult, _ := inspectDeployment(dClient, database, deployment.ID)
		deployments[i] = *inspectResult
	}
	jsonBytes, _ := json.Marshal(deployments)
	fmt.Fprintf(w, string(jsonBytes))
}

func startAPI(dockerParam *docker.Client, db *gorm.DB) {
	dClient = dockerParam
	database = db
	mux := goji.NewMux()
	mux.HandleFunc(pat.Get("/ping"), ping)
	mux.HandleFunc(pat.Get("/containers"), getContainers)
	mux.HandleFunc(pat.Get("/deployments"), getDeployments)

	http.ListenAndServe("localhost:8000", mux)
}
