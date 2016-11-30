package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	math "math/rand"
	"os"
	"strconv"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"github.com/twa16/meteor-deploy-system/common"
	"golang.org/x/crypto/bcrypt"
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

	//Load configuration
	loadConfig()

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
	db.AutoMigrate(&mds.UserPermission{})
	//db.Model(&mds.User{}).Related(&mds.UserPermission{})
	db.AutoMigrate(&mds.User{})
	db.AutoMigrate(&mds.AuthenticationToken{})
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

	log.Info("Pulling needed images")
	PullDockerImage(cli, "kadirahq/meteord")
	log.Info("Images Pulled")

	log.Info("Seeding random number generator...")
	math.Seed(time.Now().UTC().UnixNano())

	//=====Start API======
	//createDeployment(cli, db, "First-Project", "/tmp/test")
	log.Info("Start Complete! Starting API.")
	startAPI(cli, db)
}

func ensureAdminUser(db *gorm.DB) {
	log.Info("Checking is admin user exists.")
	_, err := getUser(db, "admin")
	if err != nil {
		password, _ := GenerateRandomString(16)
		createUser(db, "Admin", "User", "admin", "admin@admin.com", password, []string{"*.*"})
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

func createUser(db *gorm.DB, firstName string, lastName string, username string, email string, password string, permissions []string) {
	user := mds.User{}
	user.FirstName = firstName
	user.LastName = lastName
	user.Username = username
	user.Email = email
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	user.PasswordHash = passwordHash
	if err != nil {
		log.Fatalf("Error hashing password: %s \n", err)
	} else {
		//Now let's create the permissions
		for _, permissionString := range permissions {
			//Create the permission object
			userPermission := mds.UserPermission{}
			userPermission.UserID = user.ID
			userPermission.Permission = permissionString
			//Add it to permissions
			user.Permissions = append(user.Permissions, userPermission)
		}
		db.Create(&user)
		log.Infof("Created User: %s", user.Username)
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
	viper.SetConfigName("config")                         // name of config file (without extension)
	viper.AddConfigPath("./config")                       // path to look for the config file in
	viper.AddConfigPath("/etc/meteordeploysystem/config") // path to look for the config file in
	viper.AddConfigPath(".")                              // optionally look for config in the working directory
	//Set defaults
	viper.SetDefault("DataDirectory", "./data")
	viper.SetDefault("ApplicationDirectory", "./apps")

	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {
		log.Fatalf("Fatal error config file: %s \n", err) // Handle errors reading the config file
		panic(err)
	}

	log.Infof("Using config file: %s", viper.ConfigFileUsed())
	for _, key := range viper.AllKeys() {
		log.Infof("Loaded: %s as %s", key, viper.GetString(key))
	}
	//viper.SetDefault("k", "v")
}

// Creates a docker container that will hold the meteor application
// hostname = Name of the container
// volumePath = Directory that contains the meteor application
// externalPort = external port to assign to the container, will be proxied
func createDockerContainer(client *docker.Client, volumePath string, externalPort string, rootURL string, mongoURL string, mongoOplogURL string) (*docker.Container, error) {
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
	//Environmental Variables
	//Format is a slice of strings FOO=BAR
	env := make([]string, 3)
	env[0] = "ROOT_URL=" + rootURL
	env[1] = "MONGO_URL=" + mongoURL
	env[2] = "MONGO_OPLOG_URL=" + mongoOplogURL
	containerConfig.Env = env

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
	//Get a new port
	var port = strconv.Itoa(getNextOpenPort(db))
	//Create a deployment record
	var deployment = mds.Deployment{VolumePath: applicationDirectory, AutoStart: true, Port: port, ProjectName: projectName}
	//Save the record so it gets an ID
	db.Create(&deployment)
	nginxConfig := NginxProxyConfiguration{}
	//Create a docker container for the application
	//Filler values so I can test the proxy stuff.
	//TODO: Load actual env variables
	container, err := createDockerContainer(dClient, deployment.VolumePath, deployment.Port, "", "", "")
	if err != nil {
		log.Critical("Failed to create container: " + err.Error())
		return nil, err
	}
	deployment.ContainerID = container.ID
	err = dClient.StartContainer(container.ID, nil)
	if err != nil {
		log.Critical("Failed to start container: " + err.Error())
		return nil, err
	}
	//If there was no error then the container is running
	deployment.Status = "running"
	log.Info(deployment)
	return &deployment, nil
}

//PullDockerImage Pulls a docker image from the hub
func PullDockerImage(dClient *docker.Client, image string) error {
	pullOptions := docker.PullImageOptions{Repository: image, Tag: "latest"}
	authOptions := docker.AuthConfiguration{}
	err := dClient.PullImage(pullOptions, authOptions)
	return err
}

func inspectDeployment(dClient *docker.Client, db *gorm.DB, deploymentID uint) (*mds.Deployment, error) {
	//First, let's grab the deployment description from the DB
	var deployment mds.Deployment
	db.First(&deployment, deploymentID)

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
		var portTry = 30000 + math.Intn(10000)
		var deployment mds.Deployment
		if db.Where(&mds.Deployment{Port: strconv.Itoa(portTry)}).First(&deployment).RecordNotFound() {
			return portTry
		}
	}
	//This will never be reached but it makes the compilier happy
	return -1
}

// GenerateRandomBytes returns securely generated random bytes.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

// GenerateRandomString returns a URL-safe, base64 encoded
// securely generated random string.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomString(s int) (string, error) {
	b, err := GenerateRandomBytes(s)
	return base64.URLEncoding.EncodeToString(b), err
}
