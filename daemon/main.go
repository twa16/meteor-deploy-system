package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/op/go-logging"
)

type Deployment struct {
	gorm.Model
	VolumePath string
	AutoStart  bool
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

	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		panic("Failed to Connect to Docker: " + err.Error())
	}
	log.Info("Connected to Docker")
	log.Info("Creating Test Container")
	createDockerContainer(cli, "testhostname", "/tmp", "4356")

	for _, container := range containers {
		fmt.Printf("%s %s\n", container.ID[:10], container.Image)
	}

}

func startDockerClient() (*client.Client, error) {
	cli, err := client.NewEnvClient()
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
func createDockerContainer(client *client.Client, hostname string, volumePath string, externalPort string) {
	//======Container Config=====
	var containerConfig container.Config
	//Set the image
	containerConfig.Image = "kadirahq/meteord"
	//Set the hostname
	containerConfig.Hostname = hostname
	//Create the volume that will contain the app code
	containerConfig.Volumes = make(map[string]struct{})
	var v struct{}
	containerConfig.Volumes["/bundle"] = v
	//Ports
	//port80, _ := nat.NewPort("tcp", "80")
	containerConfig.ExposedPorts["80/tcp"] = v

	//=====Host Config======
	//Setup Volume Bindings
	var hostConfig container.HostConfig
	hostConfig.Binds = []string{"/bundle:" + volumePath}
	//Setup Port Maps
	var portMap nat.PortMap
	//Forward a dynamic host port to container. Listen on localhost so that nginx can proxy.
	portMap["80/tcp"] = PortBinding{HostIP: "127.0.0.1", HostPort: externalPort}
	hostConfig.PortBindings = portMap

	//======Network Config=====
	var networkConfig network.Config
	//Create Container
	c, err := client.ContainerCreate(context.Background(), containerConfig, hostConfig, networkConfig, hostname)
	if err != nil {
		panic(err)
	}
	fmt.Println(c.ID)
}
