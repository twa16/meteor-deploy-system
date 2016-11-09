package main

import (
	"context"
	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/op/go-logging"
)

type Deployment struct {
	gorm.Model
	ProjectName string //Name of this project
	ownerID     uint   //ID of user that owns this project
	VolumePath  string //Path to the folder that contains the meteor application on the hose
	AutoStart   bool   //Should the container be started automatically
	ContainerID string //The ID of the container that contains the application
	Port        string //Port that the application is listening on
	Status      string //Status of the container, updated on inspect
}

var log = logging.MustGetLogger("mds-daemon")

func main() {
	log.Info("Meteor Deploy System - Manuel Gauto (mgauto@mgenterprises.org)")
	log.Info("Starting...")

	//Database: Starting Connection
	log.Info("Starting ORM...")
	db, err := gorm.Open("sqlite3", "mds.db")
	if err != nil {
		panic("failed to connect database")
	}
	defer db.Close()
	log.Info("ORM Started.")

	//Database: Migrating Schemas
	log.Info("Migrating Schemas")
	db.AutoMigrate(&Deployment{})
	log.Info("Migration Complete")

	//Docker: Starting Docker Client
	log.Info("Connecting to Docker")
	cli, err := startDockerClient()
	if err != nil {
		panic("Failed to Connect to Docker: " + err.Error())
	}
	log.Info("Connected to Docker")

	//=====Start API======
	log.Info("Start Complete! Starting API.")
	createDeployment(cli, db, "First-Project", "/tmp/test")
	startAPI(cli, db)
}

func startDockerClient() (*docker.Client, error) {
	cli, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}
	return cli, err
}

func startMongo() {

}

// Creates a docker container that will hold the meteor application
// hostname = Name of the container
// volumePath = Directory that contains the meteor application
// externalPort = external port to assign to the container, will be proxied
func createDockerContainer(client *docker.Client, volumePath string, externalPort string) (*docker.Container, error) {
	//======Container Config=====
	var containerConfig docker.Config
	//Set the image
	containerConfig.Image = "kadirahq/meteord"
	//Create the volume that will contain the app code
	containerConfig.Volumes = make(map[string]struct{})
	var v struct{}
	containerConfig.Volumes["/bundle"] = v
	//Ports
	//port80, _ := nat.NewPort("tcp", "80")
	containerConfig.ExposedPorts = make(map[docker.Port]struct{})
	containerConfig.ExposedPorts["80/tcp"] = v

	//=====Host Config======
	//Setup Volume Bindings
	var hostConfig docker.HostConfig
	hostConfig.Binds = []string{volumePath + ":/bundle"}
	//Setup Port Maps
	//Forward a dynamic host port to container. Listen on localhost so that nginx can proxy.
	hostConfig.PortBindings = make(map[docker.Port][]docker.PortBinding)
	hostConfig.PortBindings["80/tcp"] = append(hostConfig.PortBindings["80/tcp"], docker.PortBinding{HostIP: "127.0.0.1", HostPort: externalPort})

	//======Network Config=====
	var networkConfig docker.NetworkingConfig

	//======Container Creation=====
	//Wrapup config
	var config docker.CreateContainerOptions
	config.Name = hostname + "-meteord"
	config.Config = &containerConfig
	config.HostConfig = &hostConfig
	config.NetworkingConfig = &networkConfig
	config.Context = context.Background()
	//Create Container
	c, err := client.CreateContainer(config)
	return c, err
}

//Removes a container
func removeContainer(client *docker.Client, id string) error {
	var options docker.RemoveContainerOptions
	options.ID = id
	options.RemoveVolumes = true
	options.Force = true
	options.Context = context.Background()

	return client.RemoveContainer(options)
}

//Creates and starts a deployment
// projectName cannot contain spaces
func createDeployment(dClient *docker.Client, db *gorm.DB, projectName string, applicationDirectory string) (*Deployment, error) {
	var deployment = Deployment{VolumePath: applicationDirectory, AutoStart: true, Port: "8345", ProjectName: projectName}
	container, err := createDockerContainer(dClient, deployment.VolumePath, deployment.Port)
	if err != nil {
		log.Critical("Failed to create container: " + err.Error())
	}
	deployment.ContainerID = container.ID
	err = dClient.StartContainer(container.ID, nil)
	if err != nil {
		log.Critical("Failed to start container: " + err.Error())
		return nil, err
	}
	//If there was no error then the container is running
	deployment.Status = "running"
	db.Create(&deployment)
	return &deployment, nil
}

func inspectDeployment(dClient *docker.Client, db *gorm.DB, deploymentId uint) (*Deployment, error) {
	//First, let's grab the deployment description from the DB
	var deployment Deployment
	db.First(&deployment, deploymentId)

	//Now let's grab the actual container
	container, err := dClient.InspectContainer(deployment.ContainerID)
	if err != nil {
		return nil, err
	}
	//Save the status
	deployment.Status = container.State.Status
	db.Save(&deployment)

	return &deployment, nil
}
