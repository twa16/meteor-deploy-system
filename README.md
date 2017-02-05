# Meteor Deployment System
This project aims to create a system to manage the deployment of meteor applications. This is also my first project written in Go so no promises on code quality. I will continue to iterate as I learn more though.

###### This project is under active development

## Configuration
### Permissions
+ **Superuser Permission**  _*.*_ This grants access to all endpoints and objects
+ **Deployment List Permission**
### Architecture

####Daemon
The daemon process runs on the server that will host the applications themselves. The daemon listens for API requests and manages the deployments themselves. The system will eventually support multi-node deployments.

**Authentication Tokens**
+ Authentication tokens are created upon login and expire in 7 days

**Deployment Process**

1. The daemon receives a deployment request from an authorized user.

2. The archive of the meteor application is deployed as a docker container on the machine.

3. An nginx reverse proxy rule is created in order to send traffic to the host.
