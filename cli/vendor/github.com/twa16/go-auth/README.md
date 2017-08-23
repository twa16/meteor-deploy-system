# Go-Auth

Simple authentication helper for GO.

[![GoDoc](https://godoc.org/github.com/twa16/go-auth?status.svg)](https://godoc.org/github.com/twa16/go-auth)

## Overview
This library was created to reduce the amount of boilerplate code I had
to write to support authentication in my applications. It serves as
a basic skeleton for an authentication system. The system is designed to
facilitate the use of user permissions which represented as dot delimited strings.
 
 

## Examples

##### Creating a User
```go
var userToCreate User
userToCreate.Username = "testuser"
userToCreate.FirstName = "Test"
userToCreate.LastName = "User"
userToCreate.Email = "test@testcompany.com"
userToCreate.PhoneNumber = "1234567890"
userToCreate.Permissions = []Permission{{Permission: "test.pass"}}

userToCreate, err := authProvider.CreateUser(userToCreate)
if err != nil {
	t.Fatal(err.Error())
}
```

##### Check is a user has a permission
```go
user, _ := authProvider.GetUser("testuser")
hasPerm, err := authProvider.CheckPermission(user.ID, "test.pass")
if err != nil {
	//Handle error
}
if hasPerm {
	//User has this permission
}
```

Please check _auth_test.go_ for more examples

