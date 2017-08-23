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

// Represents a "deployment"
type Deployment struct {
	gorm.Model
	ProjectName      string //Name of this project
	OwnerID          uint   //ID of user that owns this project
	VolumePath       string //Path to the folder that contains the meteor application on the hose
	AutoStart        bool   //Should the container be started automatically
	ContainerID      string //The ID of the container that contains the application
	Port             string //Port that the application is listening on
	Status           string //Status of the container, updated on inspect
	URL              string //URL used to reach the service. Blank until deployment is complete
	MongoContainerID string //The ID of the container that is running this app's mongo instance
	//IsDead           bool   //true if the deployment is in an irreversible error state
}
