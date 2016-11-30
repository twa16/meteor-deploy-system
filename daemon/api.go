package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/jinzhu/gorm"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"github.com/twa16/meteor-deploy-system/common"
	"goji.io"
	"goji.io/pat"
	"golang.org/x/crypto/bcrypt"
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
	tokenGen, _ := GenerateRandomString(32)
	token.AuthenticationToken = tokenGen
	token.UserID = user.ID
	token.LastSeen = time.Now().Unix()

	//Save it and return it
	database.Create(&token)
	return token, nil
}

//CreateDeployment Called when POST /deployment is called
func CreateDeployment(w http.ResponseWriter, r *http.Request) {
	//Check authentication
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], "deployment.create")
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

		//Start creating deployment
		createDeployment(dClient, database, projectName, destination)
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

//Used to extract the contents of the tarball that is produced by meteor
func extractTarball(filePath string, destination string) error {
	//Add a trailing slash if needed
	if !strings.HasSuffix(destination, "/") {
		destination += "/"
	}
	file, err := os.Open(filePath)

	if err != nil {
		return err
	}

	defer file.Close()

	var fileReader io.ReadCloser = file

	// We should be getting a gz file so decompress
	if fileReader, err = gzip.NewReader(file); err != nil {
		return err
	}
	defer fileReader.Close()

	tarBallReader := tar.NewReader(fileReader)

	// Extracting tarred files

	for {
		header, err := tarBallReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// get the individual filename and extract to the current directory
		filename := header.Name

		switch header.Typeflag {
		case tar.TypeDir:
			// handle directory
			//fmt.Println("Creating directory :", filename)
			err = os.MkdirAll(destination+filename, os.FileMode(header.Mode)) // or use 0755 if you prefer

			if err != nil {
				return err
			}

		case tar.TypeReg:
			// handle normal file
			//fmt.Println("Untarring :", filename)
			writer, err := os.Create(destination + filename)

			if err != nil {
				return err
			}

			io.Copy(writer, tarBallReader)

			err = os.Chmod(destination+filename, os.FileMode(header.Mode))

			if err != nil {
				return err
			}

			writer.Close()
		case tar.TypeSymlink:
			//createSymlink
			writer, err := os.Create(destination + filename)

			if err != nil {
				return err
			}

			io.Copy(writer, tarBallReader)

			err = os.Chmod(destination+filename, os.FileMode(header.Mode))

			if err != nil {
				return err
			}

			writer.Close()
		default:
			fmt.Printf("Unable to untar type : %c in file %s", header.Typeflag, filename)
			return errors.New("Unable to untar type")
		}
	}
	return nil
}

// Called when /containers is called
func getContainers(w http.ResponseWriter, r *http.Request) {
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], "container.list")
	if authCode == 0 {
		containers, err := dClient.ListContainers(docker.ListContainersOptions{})
		if err != nil {
			log.Critical(err)
			fmt.Fprintf(w, "Error getting containers")
		} else {
			jsonResponseBytes, _ := json.Marshal(containers)
			fmt.Fprintf(w, string(jsonResponseBytes))
		}
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

//Called when /deployments is called
func getDeployments(w http.ResponseWriter, r *http.Request) {
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], "deployment.list")
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
	authCode := checkAuthentication(database, r.Header["X-Auth-Token"][0], "deployment.delete")
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
	if (time.Now().Unix() - authenticationKey.LastSeen) > (60 * 60 * 24 * 7) {
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
	mux.HandleFunc(pat.Get("/containers"), getContainers)
	mux.HandleFunc(pat.Get("/deployments"), getDeployments)
	mux.HandleFunc(pat.Delete("/deployment"), deleteDeployment)
	mux.HandleFunc(pat.Post("/deployment"), CreateDeployment)
	mux.HandleFunc(pat.Post("/login"), login)

	log.Fatal(http.ListenAndServe(":8000", mux))
}
