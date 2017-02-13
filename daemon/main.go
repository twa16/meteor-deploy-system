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
	"github.com/pkg/errors"
)

var log = logging.MustGetLogger("mds-daemon")
var nginx NginxInstance

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
	db.AutoMigrate(&NginxProxyConfiguration{})
	log.Info("Migration Complete")

	log.Info("Creating Neccessary Directories.")
	err = os.MkdirAll(viper.GetString("CertDestination"), 0777)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(viper.GetString("DataDirectory"), 0777)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(viper.GetString("ApplicationDirectory"), 0777)
	if err != nil {
		panic(err)
	}


	//Ensure admin user exists
	ensureAdminUser(db)

	//Setup Nginx
	nginx = NginxInstance{}
	nginx.ReloadCommand = viper.GetString("NginxReloadCommand")
	nginx.SitesDirectory = viper.GetString("NginxSitesDestination")

	//Docker: Starting Docker Client
	log.Info("Connecting to Docker")
	cli, err := startDockerClient()
	if err != nil {
		panic("Failed to Connect to Docker: " + err.Error())
	}
	log.Info("Connected to Docker")

	log.Info("Pulling needed images (This can take a while the first time)...")
	PullDockerImage(cli, "abernix/meteord")
	PullDockerImage(cli, "mongo")
	log.Info("Images Pulled")

	log.Info("Seeding random number generator...")
	math.Seed(time.Now().UTC().UnixNano())

	//=====Start API======
	log.Info("Ensuring HTTPS Certificates Exist")
	apiKeyFile := viper.GetString("ApiHttpsKey")
	apiCertFile := viper.GetString("ApiHttpsCertificate")
	log.Debugf("Using Key File: %s\n", apiKeyFile)
	log.Debugf("Using Certificate File: %s\n", apiCertFile)
	//Check to see if cert exists
	_, errKey := os.Stat(apiKeyFile)
	_, errCert := os.Stat(apiCertFile)
	if os.IsNotExist(errKey) || os.IsNotExist(errCert) {
		log.Warning("Generating HTTPS Certificate for API")
		os.Remove(apiKeyFile)
		os.Remove(apiCertFile)
		privateKey, certificate, err := CreateSelfSignedCertificate(viper.GetString("ApiHost"))
		if err != nil {
			log.Fatalf("Error generating API Certificates: %s\n", err.Error())
			panic(err)
		}
		err = WriteCertificateToFile(certificate, apiCertFile)
		if err != nil {
			log.Fatalf("Error saving certificate: %s\n", err.Error())
			panic(err)
		}
		err = WritePrivateKeyToFile(privateKey, apiKeyFile)
		if err != nil {
			log.Fatalf("Error saving private key: %s\n", err.Error())
			panic(err)
		}
		log.Info("API Certificate Generation Complete.")
	}

	//Start Deployment Monitor
	go func(dClient *docker.Client, db *gorm.DB) {
		log.Info("Started Deployment Monitor")
		for true {
			InspectDeployments(dClient, db)
			time.Sleep(time.Second * 5)
		}
	}(cli, db)

	//createDeployment(cli, db, "First-Project", "/tmp/test")
	log.Info("Start Complete! Starting API.")
	startAPI(cli, db)
}

//Ensures that an admin account exists and creates one if needed
func ensureAdminUser(db *gorm.DB) {
	log.Info("Checking if admin user exists.")
	_, err := getUser(db, "admin")
	if err != nil {
		password, _ := GenerateRandomString(16)
		createUser(db, "Admin", "User", "admin", "admin@admin.com", password, []string{"*.*"})
		log.Info("Created admin user with password: " + password)
	} else {
		log.Info("Admin user exists.")
	}
}

//Gets a user object from the db by username
func getUser(db *gorm.DB, username string) (mds.User, error) {
	var user mds.User
	err := db.Where("username = ?", username).First(&user).Error
	return user, err
}

//Creates a user in the DB
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

//Starts the connect to the docker daemon
func startDockerClient() (*docker.Client, error) {
	cli, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}
	return cli, err
}

//loadConfig I bet you can guess what this function does
func loadConfig() {
	viper.SetConfigName("config")                         // name of config file (without extension)
	viper.AddConfigPath("./config")                       // path to look for the config file in
	viper.AddConfigPath("/etc/mds/config") // path to look for the config file in
	viper.AddConfigPath(".")                              // optionally look for config in the working directory
	//Set defaults
	viper.SetDefault("DataDirectory", "./data")
	viper.SetDefault("ApplicationDirectory", "./apps")
	viper.SetDefault("ApiHttpsKey", "./ssl/api.key")
	viper.SetDefault("ApiHttpsCertificate", "./ssl/api.cert")

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
func createDockerContainer(client *docker.Client, volumePath string, externalPort string, rootURL string, mongoURL string, mongoOplogURL string, meteorSettings string, environment []string, mongoContainer *docker.Container) (*docker.Container, error) {
	//======Container Config=====
	var containerConfig docker.Config
	//Set the image
	containerConfig.Image = "abernix/meteord"
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
	if meteorSettings != "" {
		env[2] = "METEOR_SETTINGS=" + meteorSettings
	}
	//Append all custom variables
	for _, variable := range environment {
		if variable != "" {
			env = append(env, variable)
		}
	}
	//TODO: Enable this
	//env[2] = "MONGO_OPLOG_URL=" + mongoOplogURL
	containerConfig.Env = env

	//=====Host Config======
	//Setup Volume Bindings
	var hostConfig docker.HostConfig
	hostConfig.Binds = []string{volumePath + ":/bundle"}
	//Setup Port Maps
	//Forward a dynamic host port to container. Listen on localhost so that nginx can proxy.
	hostConfig.PortBindings = make(map[docker.Port][]docker.PortBinding)
	hostConfig.PortBindings["80/tcp"] = append(hostConfig.PortBindings["80/tcp"], docker.PortBinding{HostIP: "127.0.0.1", HostPort: externalPort})
	//Link mongo container if necessary. The proper URLs will already be set if this is provided.
	if mongoContainer != nil {
		hostConfig.Links = []string{mongoContainer.ID + ":mongo"}
	}

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

//removeContainer Removes a container
func removeContainer(client *docker.Client, id string) error {
	var options docker.RemoveContainerOptions
	options.ID = id
	options.RemoveVolumes = true
	options.Force = true
	options.Context = context.Background()

	return client.RemoveContainer(options)
}

//Updates and restarts a deployment
// projectName cannot contain spaces
func updateDeployment(dClient *docker.Client, db *gorm.DB, deploymentID int, applicationDirectory string, meteorSettings string, environment []string) (*mds.Deployment, error) {
	/*
	 * Step 1: Get the original deployment
	 */
	log.Info("Deployment Update requested for Deployment ID: %d\n", deploymentID)
	var deployment mds.Deployment
	var nginxConfig NginxProxyConfiguration

	//Get deployment object
	err := db.Where("id = ?", deploymentID).First(&deployment).Error
	if err != nil {
		log.Critical(err)
		return nil, errors.New("Deployment with that ID does not exist")
	}
	//Get nginx config
	err = db.Where("deployment_id = ?", deployment.ID).First(&nginxConfig).Error
	if err != nil {
		log.Critical(err)
		return nil, errors.New("Could not find NginxConfig for that deployment")
	}
	log.Debugf("Deployment Update Started for %s\n", deployment.ProjectName)

	//Get Mongo Container
	mongoContainer, err := dClient.InspectContainer(deployment.MongoContainerID)
	if err != nil {
		log.Warning("Error getting Mongo container for update of "+deployment.ProjectName)
		return nil, err
	}

	/*
	 * Step 2: Cleanup old container
	 */
	//Stop the container
	err = dClient.StopContainer(deployment.ContainerID, 10)
	if err != nil {
		log.Warning(err)
	}

	//Remove the container
	//This method sets the volume remove flag as well
	err = removeContainer(dClient, deployment.ContainerID)
	if err != nil {
		log.Warning(err)
	}

	/*
	 * Step 3: Update deployment object
	 */
	//Update Deployment with new values
	deployment.VolumePath = applicationDirectory
	db.Save(&deployment)

	//He set the URLs
	mongoURL := "mongodb://mongo"
	mongoOpsLogURL := ""

	/*
	 * Step 4: Create new docker container
	 */
	//Create a docker container for the application
	log.Debugf("Starting Docker Container\n")
	container, err := createDockerContainer(dClient, deployment.VolumePath, deployment.Port, "http://"+nginxConfig.DomainName, mongoURL, mongoOpsLogURL, meteorSettings, environment, mongoContainer)
	if err != nil {
		log.Critical("Failed to create container: " + err.Error())
		return nil, err
	}
	//Start the new docker container
	err = dClient.StartContainer(container.ID, nil)
	log.Debugf("Container created: %s\n", container.ID)
	if err != nil {
		log.Critical("Failed to start container: " + err.Error())
		return nil, err
	}

	/*
	 * Step 5: Save new container information
	 */
	//Set the Container ID
	deployment.ContainerID = container.ID
	//Save deployment Info
	db.Save(&deployment)

	/*
	 * Step 6: Recreate proxy
	 */
	//Generate HTTPS settings if needed
	if nginxConfig.IsHTTPS {
		log.Infof("Generating HTTPS configuration for update of %s\n", deployment.ProjectName)
		nginxConfig = nginx.GenerateHTTPSSettings(nginxConfig)
	}
	log.Debugf("Creating nginx proxy for update of %s", deployment.ProjectName)
	_, err = nginx.CreateProxy(db, &nginxConfig)
	if err != nil {
		log.Critical("Error Creating Proxy: " + err.Error())
		return nil, err
	}

	/*
	 * Step 7: Save New Deployment Information
	 */
	//If there was no error then the container is running
	deployment.Status = "running"
	//Save deployment Info
	db.Save(&deployment)
	return &deployment, nil
}

//Creates and starts a deployment
// projectName cannot contain spaces
func createDeployment(dClient *docker.Client, db *gorm.DB, projectName string, applicationDirectory string, meteorSettings string, environment []string) (*mds.Deployment, error) {
	log.Info("Deployment Creation Started for %s\n", projectName)
	//Get a new port
	var port = strconv.Itoa(GetNextOpenPort(db))
	log.Debugf("Using port: %s\n", port)
	//Create a deployment record
	var deployment = mds.Deployment{VolumePath: applicationDirectory, AutoStart: true, Port: port, ProjectName: projectName}
	//Save the record so it gets an ID
	db.Create(&deployment)
	log.Debugf("Deployment Created and Saved\n")
	//This reserves a domainName and initializes an NginxProxyConfiguration
	nginxConfig := ReserveDomainName(db)
	log.Debugf("Domain Name Reserved: %s", nginxConfig.DomainName)
	//set URL on deployment
	deployment.URL = nginxConfig.DomainName
	//Save deployment Info
	db.Save(&deployment)
	//TODO: Actually allow https
	nginxConfig.IsHTTPS = true
	//Set the deploymentID
	nginxConfig.DeploymentID = deployment.ID
	//Set the destination
	nginxConfig.Destination = "http://127.0.0.1:" + port
	//Prepare MongoDB Stuff
	mongoURL := "mongodb://mongo"
	mongoOpsLogURL := ""
	var mongoContainer *docker.Container
	//Check to see if the daemon is set manage mongo
	if viper.GetBool("AutoManageMongoDB") {
		//Create a new mongo instance
		mongoContainerInstance, err := CreateMongoDBDockerContainer(dClient)
		//This tells the compilier that we intentionally are shadowing the variable's intitial value.
		mongoContainer = mongoContainerInstance
		//Set the ID of the mongo container
		deployment.MongoContainerID = mongoContainer.ID
		//Save deployment Info
		db.Save(&deployment)
		//Now we start the MongoDB container
		err = dClient.StartContainer(mongoContainer.ID, nil)
		log.Debugf("MognoDB Container created: %s\n", mongoContainer.ID)
		if err != nil {
			log.Critical("Failed to start MongoDB container: " + err.Error())
			return nil, err
		}

		if err != nil {
			log.Criticalf("Failed to create MongoDB container: %s\n", err.Error())
			return nil, err
		}
	} else {
		//If the application isn't set to manage mongo then set the urls to what is in the config
		mongoURL = viper.GetString("MongoDBURL")
		mongoOpsLogURL = viper.GetString("MongoDBOpsLog")
	}

	//Create a docker container for the application
	log.Debugf("Starting Docker Container\n")
	container, err := createDockerContainer(dClient, deployment.VolumePath, deployment.Port, "http://"+nginxConfig.DomainName, mongoURL, mongoOpsLogURL, meteorSettings, environment, mongoContainer)
	if err != nil {
		log.Critical("Failed to create container: " + err.Error())
		return nil, err
	}
	//Set the Container ID
	deployment.ContainerID = container.ID
	//Save deployment Info
	db.Save(&deployment)
	err = dClient.StartContainer(container.ID, nil)
	log.Debugf("Container created: %s\n", container.ID)
	if err != nil {
		log.Critical("Failed to start container: " + err.Error())
		return nil, err
	}
	//Generate HTTPS settings if needed
	if nginxConfig.IsHTTPS {
		log.Infof("Generating HTTPS configuration for %s\n", projectName)
		nginxConfig = nginx.GenerateHTTPSSettings(nginxConfig)
	}
	log.Debugf("Creating nginx proxy for %s", projectName)
	_, err = nginx.CreateProxy(db, &nginxConfig)
	if err != nil {
		log.Critical("Error Creating Proxy: " + err.Error())
		return nil, err
	}
	//If there was no error then the container is running
	deployment.Status = "running"
	//Save deployment Info
	db.Save(&deployment)
	return &deployment, nil
}

//PullDockerImage Pulls a docker image from the hub
func PullDockerImage(dClient *docker.Client, image string) error {
	pullOptions := docker.PullImageOptions{Repository: image, Tag: "latest"}
	authOptions := docker.AuthConfiguration{}
	err := dClient.PullImage(pullOptions, authOptions)
	return err
}

//InspectDeployments Inspects all deployments and stores updated status in database.
func InspectDeployments(dClient *docker.Client, db *gorm.DB) {
	var deployments []mds.Deployment
	database.Find(&deployments)
	for _, deployment := range deployments {
		inspectResult, err := inspectDeployment(dClient, database, deployment.ID)
		if err != nil {
			log.Warning(err)
		}
		if inspectResult.Status != deployment.Status {
			log.Infof("Update Deployment %d to status %s from %s\n", deployment.ID, inspectResult.Status, deployment.Status)
		}
		db.Save(&inspectResult)
	}
}

//Internal call that checks the status of a deployment and updates the DB
func inspectDeployment(dClient *docker.Client, db *gorm.DB, deploymentID uint) (*mds.Deployment, error) {
	//First, let's grab the deployment description from the DB
	var deployment mds.Deployment
	db.First(&deployment, deploymentID)

	if deployment.ContainerID != "" {
		//Now let's grab the actual container
		container, err := dClient.InspectContainer(deployment.ContainerID)
		if err != nil {
			return nil, err
		}
		//Save the status
		deployment.Status = container.State.Status
		db.Save(&deployment)
	}
	return &deployment, nil
}

func DeleteDeployment(dClient *docker.Client, db *gorm.DB, deploymentID uint) error {
	//First, get the deployment object
	deployment := mds.Deployment{}
	resp := db.First(&deployment, deploymentID)
	if resp.RecordNotFound() {
		return errors.New("Deployment not found")
	}

	//Let's delete the nginx proxy config
	nginx.DeleteProxyConfiguration(db, deployment.URL)


	//Stop the container
	err := dClient.StopContainer(deployment.ContainerID, 10)
	if err != nil {
		log.Warning(err)
	}
	//Remove the container
	//This method sets the volume remove flag as well
	err = removeContainer(dClient, deployment.ContainerID)
	if err != nil {
		log.Warning(err)
	}

	//Delete Record
	db.Delete(&deployment)
	return nil
}

//GetNextOpenPort Generates an available port. This checks against the DB but does not reserve it
// so technically, conflicts are possible.
func GetNextOpenPort(db *gorm.DB) int {
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
