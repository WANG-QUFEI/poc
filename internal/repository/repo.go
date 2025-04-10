package repository

import (
	"errors"
	"fmt"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var _ IRepository = &Repo{}

var (
	ErrRecordNotFound = fmt.Errorf("record not found")

	defaultDevicePollingOutdateGap = 30 * time.Minute
)

type SqlSelectionCondition string

type DevicePollingParameter struct {
	DeviceType     string
	Interval       time.Duration
	OutdatedPeriod *time.Duration
	Limit          int
}

type IRepository interface {
	CreateDeviceTypes([]*DeviceType) error
	CreateDevice(device *Device) error
	CreateDevices(devices []*Device) error
	CreatePollingHistory(history *PollingHistory) error
	CreatePollingHistories(histories []*PollingHistory) error
	UpdateDevice(device *Device) error
	GetDeviceByID(deviceID string) (*Device, error)
	GetDevicesByPage(page, size int, condition string) ([]Device, int, error)
	GetAllDeviceTypes() ([]DeviceType, error)
	GetDevicesByPollingParameter(DevicePollingParameter) ([]Device, error)
	GetDevicePollingHistory(deviceID string, limit int) ([]PollingHistory, error)
}

type Repo struct {
	db *gorm.DB
}

func (repo *Repo) Conn() *gorm.DB {
	return repo.db
}

func NewRepository(dsn string) (*Repo, error) {
	if dsn == "" {
		return nil, fmt.Errorf("illegal argument: dsn cannot be empty")
	}

	cfg := &gorm.Config{Logger: logger.Discard}
	if config.EnableGormLogging() {
		cfg.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		return nil, err
	}

	return &Repo{db: db}, nil
}

func (repo *Repo) CreateDeviceTypes(deviceTypes []*DeviceType) error {
	if len(deviceTypes) == 0 {
		return nil
	}
	return repo.db.Create(&deviceTypes).Error
}

func (repo *Repo) CreateDevice(device *Device) error {
	if device == nil {
		return fmt.Errorf("illegal argument: device is nil")
	}
	if device.ID > 0 {
		return fmt.Errorf("illegal argument: device is already persisted with ID %d", device.ID)
	}
	if err := repo.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&device).Error; err != nil {
		return err
	}
	return nil
}

func (repo *Repo) CreateDevices(devices []*Device) error {
	var filteredDevices []*Device
	for _, device := range devices {
		if device == nil {
			continue
		}
		if device.ID > 0 {
			return fmt.Errorf("illegal argument: cannot create device already with database id: %d", device.ID)
		}
		filteredDevices = append(filteredDevices, device)
	}
	if len(filteredDevices) == 0 {
		return nil
	}
	if err := repo.db.Create(&filteredDevices).Error; err != nil {
		return err
	}
	return nil
}

func (repo *Repo) CreatePollingHistory(history *PollingHistory) error {
	if history == nil {
		return fmt.Errorf("illegal argument: polling history is nil")
	}
	if history.ID > 0 {
		return fmt.Errorf("illegal argument: polling history is already persisted with ID %d", history.ID)
	}
	if err := repo.db.Create(&history).Error; err != nil {
		return err
	}
	return nil
}

func (repo *Repo) CreatePollingHistories(histories []*PollingHistory) error {
	var filteredHistories []*PollingHistory
	for _, history := range histories {
		if history == nil {
			continue
		}
		if history.ID > 0 {
			return fmt.Errorf("illegal argument: cannot create polling history already with database id: %d", history.ID)
		}
		filteredHistories = append(filteredHistories, history)
	}
	if len(filteredHistories) == 0 {
		return nil
	}
	if err := repo.db.Create(&filteredHistories).Error; err != nil {
		return err
	}
	return nil
}

func (repo *Repo) UpdateDevice(device *Device) error {
	if device == nil {
		return fmt.Errorf("illegal argument: device is nil")
	}
	if device.ID <= 0 {
		return fmt.Errorf("illegal argument: cannot update unsaved device")
	}
	if err := repo.db.Save(&device).Error; err != nil {
		return err
	}
	return nil
}

func (repo *Repo) GetDeviceByID(deviceID string) (*Device, error) {
	var device Device
	if err := repo.db.Where("device_id = ?", deviceID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &device, nil
}

func (repo *Repo) GetDevicesByPage(page, size int, condition string) ([]Device, int, error) {
	if page < 0 || size <= 0 {
		return nil, 0, fmt.Errorf("illegal argument: invalid page or size")
	}

	q := `select count(*) from devices where deleted_at is null`
	if condition != "" {
		q += " and " + condition
	}
	var count int
	err := repo.db.Raw(q).Scan(&count).Error
	if err != nil {
		return nil, 0, err
	}

	var devices []Device
	err = repo.db.Where(condition).Where("deleted_at is null").Offset(page * size).Limit(size).Order("id asc").Find(&devices).Error
	if err != nil {
		return nil, 0, err
	}
	return devices, count, nil
}

func (repo *Repo) GetAllDeviceTypes() ([]DeviceType, error) {
	var deviceTypes []DeviceType
	err := repo.db.Where("deleted_at is null").Find(&deviceTypes).Error
	return deviceTypes, err
}

func (repo *Repo) GetDevicesByPollingParameter(param DevicePollingParameter) ([]Device, error) {
	if err := param.validate(); err != nil {
		return nil, fmt.Errorf("illegal argument: %w", err)
	}

	q := `update devices set polling_status = @status_in_progress where id in (
		select id from devices where deleted_at is null and device_type = @device_type and
			(
				((polling_status is null or polling_status != @status_in_progress) and (last_checked_at is null or last_checked_at < @recent_checkpoint)) 
					or 
				last_checked_at < @remote_checkpoint 
					or 
				(last_checked_at is null and created_at < @remote_checkpoint)
			)
		order by last_checked_at asc limit @limit
	) returning *`

	var devices []Device
	recentCheckpoint := time.Now().Add(-param.Interval)
	remoteCheckpoint := time.Now().Add(-*param.OutdatedPeriod)
	err := repo.db.Raw(q, map[string]any{
		"status_in_progress": PollingInProgress,
		"device_type":        param.DeviceType,
		"recent_checkpoint":  recentCheckpoint,
		"remote_checkpoint":  remoteCheckpoint,
		"limit":              param.Limit,
	}).Scan(&devices).Error

	return devices, err
}

func (repo *Repo) GetDevicePollingHistory(deviceID string, limit int) ([]PollingHistory, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("illegal argument: limit must be a positive integer")
	}

	var histories []PollingHistory
	err := repo.db.Where("device_id = ?", deviceID).Order("created_at desc").Limit(limit).Find(&histories).Error
	return histories, err
}

func (param *DevicePollingParameter) validate() error {
	if param.DeviceType == "" {
		return fmt.Errorf("illegal argument: device type cannot be empty")
	}
	if param.Interval <= 0 {
		return fmt.Errorf("illegal argument: polling interval must be a positive value")
	}
	if param.Limit <= 0 {
		return fmt.Errorf("illegal argument: limit is must be a positive integer")
	}
	if param.OutdatedPeriod == nil {
		param.OutdatedPeriod = &defaultDevicePollingOutdateGap
	}
	if *param.OutdatedPeriod <= 0 {
		return fmt.Errorf("illegal argument: outdate gap must be a positive value")
	}
	return nil
}
