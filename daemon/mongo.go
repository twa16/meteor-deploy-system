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

package main

import (
	"context"

	docker "github.com/fsouza/go-dockerclient"
)

//CreateMongoDBDockerContainer Creates a MongoDB instance in Docker
func CreateMongoDBDockerContainer(client *docker.Client) (*docker.Container, error) {
	//======Container Config=====
	var containerConfig docker.Config
	//Set the image
	containerConfig.Image = "mongo"
	containerConfig.Env = []string{}

	//=====Host Config======
	//Setup Volume Bindings
	var hostConfig docker.HostConfig

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
