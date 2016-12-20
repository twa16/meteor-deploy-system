package mds

import (
	"github.com/jinzhu/gorm"
)

type AuthenticationToken struct {
	gorm.Model
	AuthenticationToken string //Session key used to authorize requests
	UserID              uint   //ID of user that this token belongs to
	LastSeen            int64  //Linux time of last API Call
}

// Represents a "deployment"
type Deployment struct {
	gorm.Model
	ProjectName      string //Name of this project
	ownerID          uint   //ID of user that owns this project
	VolumePath       string //Path to the folder that contains the meteor application on the hose
	AutoStart        bool   //Should the container be started automatically
	ContainerID      string //The ID of the container that contains the application
	Port             string //Port that the application is listening on
	Status           string //Status of the container, updated on inspect
	URL              string //URL used to reach the service. Blank until deployment is complete
	MongoContainerID string //The ID of the container that is running this app's mongo instance
}

type User struct {
	gorm.Model
	FirstName    string
	LastName     string
	Username     string `gorm:"unique"`
	Email        string
	PasswordHash []byte           //BCrypt hash of password
	Permissions  []UserPermission //Permissions that this user has
}

type UserPermission struct {
	gorm.Model
	UserID     uint
	Permission string
}
