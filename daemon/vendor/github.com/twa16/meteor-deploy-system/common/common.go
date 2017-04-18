/*
 * Copyright 2017 Manuel Gauto (github.com/twa16)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
*/

package mds

import (
	"github.com/jinzhu/gorm"
)

type AuthenticationToken struct {
	gorm.Model
	AuthenticationToken string //Session key used to authorize requests
	UserID              uint   //ID of user that this token belongs to
	LastSeen            int64  //Linux time of last API Call
	Persistent	    bool   //If this is set to true, the key never expires.
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
