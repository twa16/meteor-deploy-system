package main

import (
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/spf13/viper"
)

type NginxInstance struct {
	sitesAvailableDirectory string //Path to the sites-available directory
	sitesEnabledDirectory   string //Path to the sites-enabled directory
	reloadCommand           string //Command to execute when attempting to reload Nginx
}

type NginxProxyConfiguration struct {
	gorm.Model
	domainName      string
	certificatePath string
	privateKeyPath  string
	destination     string
	deploymentID    int
}

// Called when mds wishes to reload Nginx
func (n *NginxInstance) applyChanges() error {
	log.Warning("Reloading Nginx")
	out, err := exec.Command(n.reloadCommand).Output()
	return err
}

func (n *NginxInstance) createProxy(config *NginxProxyConfiguration) (string, error) {
	log.Infof("Creating Proxy for deployment %d", config.ID)
	templateBytes, err := ioutil.ReadFile(viper.GetString("DataDirectory"))
	if err != nil {
		return "", err
	}
	//Convert the bytes to a string
	templateString := string(templateBytes)

	//Build domain name
	domainName := config.domainName
	//Set the values in the configuration
	configString := strings.Replace(templateString, "domainName", domainName, -1)
	configString = strings.Replace(templateString, "certificatePath", config.certificatePath, -1)
	configString = strings.Replace(templateString, "privateKeyPath", config.privateKeyPath, -1)
	configString = strings.Replace(templateString, "destination", config.destination, -1)

	var fileName = n.sitesAvailableDirectory + "/MDS-" + string(config.ID) + ".conf"
	err = ioutil.WriteFile(fileName, []byte(configString), 0644)
	if err != nil {
		return "", err
	}
}
