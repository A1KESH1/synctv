package db

import (
	"errors"

	"github.com/synctv-org/synctv/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetBilibiliVendor(userID string) (*model.BilibiliVendor, error) {
	var vendor model.BilibiliVendor
	err := db.Where("user_id = ?", userID).Preload(clause.Associations).First(&vendor).Error
	return &vendor, HandleNotFound(err, "vendor")
}

func CreateOrSaveBilibiliVendor(userID string, vendorInfo *model.BilibiliVendor) (*model.BilibiliVendor, error) {
	vendorInfo.UserID = userID
	return vendorInfo, Transactional(func(tx *gorm.DB) error {
		if errors.Is(tx.First(&model.BilibiliVendor{
			UserID: userID,
		}).Error, gorm.ErrRecordNotFound) {
			return tx.Create(&vendorInfo).Error
		} else {
			return tx.Save(&vendorInfo).Error
		}
	})
}

func DeleteBilibiliVendor(userID string) error {
	return db.Where("user_id = ?", userID).Delete(&model.BilibiliVendor{}).Error
}

func GetAlistVendor(userID string) (*model.AlistVendor, error) {
	var vendor model.AlistVendor
	err := db.Where("user_id = ?", userID).Preload(clause.Associations).First(&vendor).Error
	return &vendor, HandleNotFound(err, "vendor")
}

func CreateOrSaveAlistVendor(userID string, vendorInfo *model.AlistVendor) (*model.AlistVendor, error) {
	vendorInfo.UserID = userID
	return vendorInfo, Transactional(func(tx *gorm.DB) error {
		if errors.Is(tx.First(&model.AlistVendor{
			UserID: userID,
		}).Error, gorm.ErrRecordNotFound) {
			return tx.Create(&vendorInfo).Error
		} else {
			return tx.Save(&vendorInfo).Error
		}
	})
}

func DeleteAlistVendor(userID string) error {
	return db.Where("user_id = ?", userID).Delete(&model.AlistVendor{}).Error
}
