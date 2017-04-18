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

package simpleauth

import (
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

type AuthProvider struct {
	Database                 *gorm.DB //Database that the auth provider uses to store user information
	SessionExpireTimeSeconds int64    //The time the sessions created by this provider should live
}

type User struct {
	gorm.Model                  //All DB fields
	Username     string         `gorm:"unique"` //The username of the user
	PasswordHash []byte         `json:"-"`      //BCrypt hash of the user's password
	FirstName    string         //First name of the user
	LastName     string         //Last name of the user
	Email        string         `gorm:"unique"` //Email of the user
	PhoneNumber  string         `gorm:"unique"` //Phone number of the users
	Role         string         //String that represents a user's role
	Permissions  []Permission   `gorm:"ForeignKey:AuthUserID"` //The permissions the user has
	UserMetaData []UserMetadata `gorm:"ForeignKey:AuthUserID"` //The metadata of the user
	Sessions     []Session      `gorm:"ForeignKey:AuthUserID"` //Sessions associated with this user
}

type Permission struct {
	gorm.Model `json:"-"` //DB Fields
	AuthUserID uint       //ID of the user this belongs to
	Permission string     //Permission string
}

type UserMetadata struct {
	gorm.Model `json:"-"` //DB Fields
	AuthUserID uint       //ID of the user this belongs to
	Key        string     //Key for the metadata field
	Value      string     //Value for this metadata field
}

type Session struct {
	gorm.Model          `json:"-"` //DB Fields
	AuthenticationToken string     //Session key used to authorize requests
	AuthUserID          uint       //ID of user that this token belongs to
	LastSeen            int64      //Linux time of last API Call
	Persistent          bool       //If this is set to true, the key never expires.
}

type SessionCheckResponse struct {
	AuthSession *Session //Session pointer. Set if the session exists
	IsExpired   bool     //True if the session is expired
}

//Startup This method migrates all models and does all needed one time setup for the authentication provider
func (authProvider AuthProvider) Startup() {
	authProvider.Database.AutoMigrate(&User{})
	authProvider.Database.AutoMigrate(&Permission{})
	authProvider.Database.AutoMigrate(&UserMetadata{})
	authProvider.Database.AutoMigrate(&Session{})
}

//CreateUser Persists the user in the database
func (authProvider AuthProvider) CreateUser(user User) (User, error) {
	tx := authProvider.Database.Begin()
	err := tx.Save(&user).Error
	if err != nil {
		fmt.Println(err)
		//If there is an error rollback and return error
		tx.Rollback()
		return user, err
	}
	//Commit if no error
	tx.Commit()
	return user, err
}

//UpdateUser Update a user's data. This is just a wrapper around CreateUser.
func (authProvider AuthProvider) UpdateUser(user User) (User, error) {
	return authProvider.CreateUser(user)
}

//GetUser Retrieves a user from the database by their username
func (authProvider AuthProvider) GetUser(username string) (User, error) {
	var user User
	err := authProvider.Database.Where("username = ?", username).First(&user).Error
	if err != nil {
		return user, err
	}
	authProvider.Database.Model(&user).Association("Permissions").Find(&user.Permissions)
	authProvider.Database.Model(&user).Association("UserMetaData").Find(&user.UserMetaData)
	authProvider.Database.Model(&user).Association("Sessions").Find(&user.Sessions)
	return user, err
}

//GetUserByID Gets a user from the database by their ID
func (authProvider AuthProvider) GetUserByID(userID uint) (User, error) {
	var user User
	err := authProvider.Database.First(&user, userID).Error
	if err != nil {
		return user, err
	}
	authProvider.Database.Model(&user).Association("Permissions").Find(&user.Permissions)
	authProvider.Database.Model(&user).Association("UserMetaData").Find(&user.UserMetaData)
	authProvider.Database.Model(&user).Association("Sessions").Find(&user.Sessions)
	return user, err
}

//SetUserPassword Sets the user's password
func (authProvider AuthProvider) SetUserPassword(user User, password string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = passwordHash
	_, err = authProvider.UpdateUser(user)
	return err
}

//CheckLogin This function returns the user true if the credentials correspond to a user
func (authProvider AuthProvider) CheckLogin(username string, password string) (bool, error) {
	userObject, err := authProvider.GetUser(username)
	//Check if there is an error
	if err != nil {
		return false, err
	}
	//Check if the user exists
	if userObject.Username == "" {
		return false, nil
	}
	compareResultError := bcrypt.CompareHashAndPassword(userObject.PasswordHash, []byte(password))
	return compareResultError == nil, nil
}

//GenerateSessionKey Generates a Session for a user. If 'persistent' is set to true the session will never expire.
func (authProvider AuthProvider) GenerateSessionKey(userID uint, persistent bool) (Session, error) {
	sessionKey := Session{}
	sessionKey.AuthUserID = userID
	sessionKey.Persistent = persistent
	sessionKey.AuthenticationToken = uuid.NewV4().String()
	sessionKey.LastSeen = time.Now().Unix()
	tx := authProvider.Database.Begin()
	err := tx.Create(&sessionKey).Error
	if err != nil {
		//Rollback and return error if error
		tx.Rollback()
		return sessionKey, err
	}
	//Commit if there is no error
	tx.Commit()
	return sessionKey, err
}

//CheckSessionKey Checks a session key and returns the session if it exists
func (authProvider AuthProvider) CheckSessionKey(sessionKey string) (SessionCheckResponse, error) {
	var session Session
	err := authProvider.Database.Where("authentication_token = ?", sessionKey).First(&session).Error

	curTime := time.Now().Unix()
	checkResponse := SessionCheckResponse{}
	checkResponse.AuthSession = &session
	checkResponse.IsExpired = (curTime - session.LastSeen) > authProvider.SessionExpireTimeSeconds
	return checkResponse, err
}

//UpdateSessionAccessTime Sets the last access time on a session to the current time.
func (authProvider AuthProvider) UpdateSessionAccessTime(session Session) error {
	curTime := time.Now().Unix()
	if (curTime - session.LastSeen) > authProvider.SessionExpireTimeSeconds {
		session.LastSeen = curTime
		err := authProvider.Database.Save(&session).Error
		return err
	}
	return nil
}

//CheckPermission Returns true if the user has the provided permission
func (authProvider AuthProvider) CheckPermission(userID uint, permission string) (bool, error) {
	user, err := authProvider.GetUserByID(userID)
	if err != nil {
		return false, err
	}
	return authProvider.CheckPermissionLogic(permission, user.Permissions), err

}

//CheckPermissionLogic method that contains logic used to process permission checks
func (authProvider AuthProvider) CheckPermissionLogic(permissionReq string, userPermissions []Permission) bool {
	//Split Permission Request
	permReqParts := strings.Split(permissionReq, ".")
	for _, userPerm := range userPermissions {
		userPermParts := strings.Split(userPerm.Permission, ".")
		userPermPartCount := len(userPermParts)
		for ri, permReqPart := range permReqParts {
			//Check if the requested permission is too long
			if (ri + 1) > userPermPartCount {
				break
			}
			//Check if the user permission at this section is a wildcard
			if userPermParts[ri] == "*" {
				return true
			}
			//Check if the indexed parts match
			if permReqPart != userPermParts[ri] {
				break
			}
			//If all other tests pass and this is the last piece, this permission works
			if (ri + 1) == len(permReqParts) {
				return true
			}
		}
	}
	return false
}
