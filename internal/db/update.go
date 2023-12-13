package db

import (
	log "github.com/sirupsen/logrus"
	"github.com/synctv-org/synctv/internal/model"
	"gorm.io/gorm"
)

type dbVersion struct {
	NextVersion string
	Upgrade     func(*gorm.DB) error
}

const CurrentVersion = "0.0.1"

var dbVersions = map[string]dbVersion{
	"0.0.1": {
		NextVersion: "0.0.2-dev",
		Upgrade:     nil,
	},
}

func upgradeDatabase() error {
	setting := model.Setting{
		Name:  "database_version",
		Type:  model.SettingTypeString,
		Group: model.SettingGroupDatabase,
		Value: CurrentVersion,
	}
	err := FirstOrCreateSettingItemValue(&setting)
	if err != nil {
		return err
	}
	currentVersion := setting.Value
	version, ok := dbVersions[currentVersion]
	if !ok {
		return nil
	}
	currentVersion = version.NextVersion
	for currentVersion != "" {
		version, ok := dbVersions[currentVersion]
		if !ok {
			break
		}
		log.Infof("Upgrading database to version %s", currentVersion)
		if version.Upgrade != nil {
			err = version.Upgrade(db)
			if err != nil {
				return err
			}
		}
		err = UpdateSettingItemValue("database_version", currentVersion)
		if err != nil {
			return err
		}
		currentVersion = version.NextVersion
	}
	return nil
}
