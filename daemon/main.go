package main

import (
	"context"
	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"github.com/twa16/meteor-deploy-system/common"
	"golang.org/x/crypto/bcrypt"
	"math/rand"
	"os"
	"strconv"
)

var log = logging.MustGetLogger("mds-daemon")

func main() {
	log.Info("Meteor Deploy System - Manuel Gauto (mgauto@mgenterprises.org)")
	log.Info("Starting...")

	//Setup logging format
	var format = logging.MustStringFormatter(
		`%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`,
	)
	backend1 := logging.NewLogBackend(os.Stderr, "", 0)
	backend1Formatter := logging.NewBackendFormatter(backend1, format)
	logging.SetBackend(backend1Formatter)

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
	db.AutoMigrate(&mds.Deployment{})
	db.AutoMigrate(&mds.User{})
	log.Info("Migration Complete")

	//Ensure admin user exists
	ensureAdminUser(db)

	//Docker: Starting Docker Client
	log.Info("Connecting to Docker")
	cli, err := startDockerClient()
	if err != nil {
		panic("Failed to Connect to Docker: " + err.Error())
	}
	log.Info("Connected to Docker")

	//=====Start API======
	//createDeployment(cli, db, "First-Project", "/tmp/test")
	log.Info("Start Complete! Starting API.")
	startAPI(cli, db)
}

func ensureAdminUser(db *gorm.DB) {
	log.Info("Checking is admin user exists.")
	_, err := getUser(db, "admin")
	if err != nil {
		password := randStr(16)
		createUser(db, "Admin", "User", "admin", "admin@company.com", password)
		log.Info("Created admin user with password: " + password)
	} else {
		log.Info("Admin user exists.")
	}
}

func getUser(db *gorm.DB, username string) (mds.User, error) {
	var user mds.User
	err := db.Where("username = ?", username).First(&user).Error
	return user, err
}

func createUser(db *gorm.DB, firstName string, lastName string, username string, email string, password string) {
	user := mds.User{}
	user.FirstName = firstName
	user.LastName = lastName
	user.Username = username
	user.Email = email
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	user.PasswordHash = passwordHash
	if err != nil {
		log.Fatal("Error hashing password: %s \n", err)
	} else {
		log.Infof("Created User: %s", user.Username)
		db.Create(&user)
	}
}

func startDockerClient() (*docker.Client, error) {
	cli, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}
	return cli, err
}

func loadConfig() {
	viper.SetConfigName("config")                   // name of config file (without extension)
	viper.AddConfigPath("/etc/meteordeploysystem/") // path to look for the config file in
	viper.AddConfigPath(".")                        // optionally look for config in the working directory
	err := viper.ReadInConfig()                     // Find and read the config file
	if err != nil {
		log.Fatal("Fatal error config file: %s \n", err) // Handle errors reading the config file
		panic(err)
	}

	//viper.SetDefault("k", "v")
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
func createDeployment(dClient *docker.Client, db *gorm.DB, projectName string, applicationDirectory string) (*mds.Deployment, error) {
	var port = strconv.Itoa(getNextOpenPort(db))
	var deployment = mds.Deployment{VolumePath: applicationDirectory, AutoStart: true, Port: port, ProjectName: projectName}
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
	log.Info(deployment)
	return &deployment, nil
}

func inspectDeployment(dClient *docker.Client, db *gorm.DB, deploymentId uint) (*mds.Deployment, error) {
	//First, let's grab the deployment description from the DB
	var deployment mds.Deployment
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

func getNextOpenPort(db *gorm.DB) int {
	for true {
		var portTry = 30000 + rand.Intn(10000)
		var deployment mds.Deployment
		if db.Where(&mds.Deployment{Port: strconv.Itoa(portTry)}).First(&deployment).RecordNotFound() {
			return portTry
		}
	}
	//This will never be reached but it makes the compilier happy
	return -1
}

func randStr(strSize int) string {

	var dictionary string

	dictionary = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	var bytes = make([]byte, strSize)
	rand.Read(bytes)
	for k, v := range bytes {
		bytes[k] = dictionary[v%byte(len(dictionary))]
	}
	return string(bytes)
}
