package main

import (
	"github.com/jinzhu/gorm"
	"golang.org/x/crypto/bcrypt"
	"github.com/twa16/go-auth"
)

var authProvider simpleauth.AuthProvider

func initAuthSystem(db *gorm.DB) {
	authProvider.SessionExpireTimeSeconds = 60*60*24*14
	authProvider.Database = db
	authProvider.Startup()
}

//Ensures that an admin account exists and creates one if needed
func ensureAdminUser() {
	log.Info("Checking if admin user exists.")
	_, err := getUser("admin")
	if err != nil {
		password, _ := GenerateRandomString(16)
		createUser("Admin", "User", "admin", "admin@admin.com", password, []string{"*.*"})
		log.Info("Created admin user with password: " + password)
	} else {
		log.Info("Admin user exists.")
	}
}

//Gets a user object from the db by username
func getUser(username string) (simpleauth.User, error) {
	return authProvider.GetUser(username)
}

//Creates a user in the DB
func createUser(firstName string, lastName string, username string, email string, password string, permissions []string) {
	user := simpleauth.User{}
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
			userPermission := simpleauth.Permission{}
			userPermission.AuthUserID = user.ID
			userPermission.Permission = permissionString
			//Add it to permissions
			user.Permissions = append(user.Permissions, userPermission)
		}
		user, err = authProvider.CreateUser(user)
		if err != nil {
			log.Infof("Created User: %s", user.Username)
		} else {
			log.Critical("Error Creating User: %s\n", err.Error())
		}
	}
}
