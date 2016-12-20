package main

import (
	"context"

	docker "github.com/fsouza/go-dockerclient"
)

//CreateMongoDBDockerContainer Creates a MongoDB instance in Docker
func CreateMongoDBDockerContainer(client *docker.Client) (*docker.Container, error) {
	//======Container Config=====
	var containerConfig docker.Config
	//Set the image
	containerConfig.Image = "mongo"
	containerConfig.Env = []string{}

	//=====Host Config======
	//Setup Volume Bindings
	var hostConfig docker.HostConfig

	//======Network Config=====
	var networkConfig docker.NetworkingConfig

	//======Container Creation=====
	//Wrapup config
	var config docker.CreateContainerOptions
	config.Config = &containerConfig
	config.HostConfig = &hostConfig
	config.NetworkingConfig = &networkConfig
	config.Context = context.Background()
	//Create Container
	c, err := client.CreateContainer(config)
	return c, err
}
