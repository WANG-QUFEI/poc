package repository

import (
	"time"

	"github.com/lib/pq"
)

type (
	PollingStatus string
	PollingResult string
)

var (
	PollingDone       PollingStatus = "done"
	PollingInProgress PollingStatus = "in_progress"
	PollingCancelled  PollingStatus = "cancelled"
)

const (
	PollSucceed PollingResult = "succeed"
	PollFailed  PollingResult = "failed"

	Router           = "router"
	Switch           = "switch"
	Camera           = "camera"
	DoorAccessSystem = "door_access_system"

	REST = "rest"
	GRPC = "grpc"
)

type DeviceType struct {
	ID          uint `gorm:"primaryKey"`
	Name        string
	Description *string
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	DeletedAt   *time.Time
}

func (DeviceType) TableName() string {
	return "device_types"
}

type Device struct {
	ID            uint `gorm:"primaryKey"`
	DeviceID      string
	DeviceType    string
	Hostname      string
	Protocols     pq.StringArray `gorm:"type:text[]"`
	RestPort      *int
	RestPath      *string
	GrpcPort      *int
	PollingStatus *PollingStatus
	CreatedAt     time.Time `gorm:"autoCreateTime"`
	LastCheckedAt *time.Time
	DeletedAt     *time.Time
}

func (Device) TableName() string {
	return "devices"
}

type PollingHistory struct {
	ID             uint `gorm:"primaryKey"`
	DeviceID       string
	HwVersion      *string
	SwVersion      *string
	FwVersion      *string
	DeviceStatus   *string
	DeviceChecksum *string
	PollingResult  PollingResult
	FailureReason  *string
	CreatedAt      time.Time `gorm:"autoCreateTime"`
}

func (PollingHistory) TableName() string {
	return "polling_history"
}
