package main

import (
	"github.com/jinzhu/gorm"
	"golang.org/x/crypto/bcrypt"
	"github.com/twa16/meteor-deploy-system/common"
)

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
