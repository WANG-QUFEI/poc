package api

import (
	"context"
	"fmt"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

var _ IDeviceMonitor = (*GrpcDeviceMonitor)(nil)

var _ IDeviceMonitor = (*RESTDeviceMonitor)(nil)

type Connectivity string

const (
	Connected    Connectivity = "connected"
	Disconnected Connectivity = "disconnected"
	Unknown      Connectivity = "unknown"
	Connecting   Connectivity = "connecting"
)

var (
	ErrInvalidResponse = fmt.Errorf("invalid server response")
)

type IDeviceMonitor interface {
	PollDevice(context.Context, PollDeviceRequest) (*PollDeviceResponse, error)
}

type PollDeviceRequest struct {
	Hostname string  `json:"hostname"`
	Port     *int    `json:"port"`
	Path     *string `json:"path"`
}

type PollDeviceResponse struct {
	Id       string `json:"id"`
	Type     string `json:"type"`
	Hw       string `json:"hw_version"`
	Sw       string `json:"sw_version"`
	Fw       string `json:"fw_version"`
	Status   string `json:"status"`
	Checksum string `json:"checksum"`
}

func (info *PollDeviceRequest) validate() error {
	if info.Hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if info.Port != nil {
		if *info.Port < 0 || *info.Port > 65535 {
			return fmt.Errorf("invalid port number: %d", *info.Port)
		}
	}
	return nil
}

type IPollingStrategy interface {
	GetPollingConfigByDeviceType(string) (PollingConfig, error)
}

type BackoffConfig struct {
	BaseDelay time.Duration `json:"backoff_base_delay"`
	Factor    float64       `json:"backoff_factor"`
	MaxDelay  time.Duration `json:"backoff_max_delay"`
}

type PollingConfig struct {
	Interval  time.Duration  `json:"interval"`
	Timeout   time.Duration  `json:"request_timeout"`
	BatchSize int            `json:"batch_size"`
	Backoff   *BackoffConfig `json:"backoff"`
}

func (pc *PollingConfig) Validate() error {
	if pc == nil {
		return fmt.Errorf("polling config cannot be nil")
	}

	if err := validation.ValidateStruct(pc,
		validation.Field(&pc.Interval, validation.Min(time.Duration(1*time.Millisecond)).Error("polling interval must be greater than or equal to 1 millisecond")),
		validation.Field(&pc.Timeout, validation.Min(time.Duration(10*time.Millisecond)).Error("polling timeout must be greater than or equal to 10 millisecond")),
		validation.Field(&pc.BatchSize, validation.Min(1).Error("polling batch size must be greater than or equal to 1")),
		validation.Field(&pc.Backoff, validation.Required.Error("backoff config cannot be nil")),
	); err != nil {
		return err
	}

	cfg := pc.Backoff
	if err := validation.ValidateStruct(cfg,
		validation.Field(&cfg.BaseDelay, validation.Min(time.Duration(10*time.Millisecond)).Error("backoff base delay must be greater than or equal to 10 millisecond")),
		validation.Field(&cfg.Factor, validation.Min(1.0).Error("backoff factor must be greater than or equal to 1")),
		validation.Field(&cfg.MaxDelay, validation.Min(time.Duration(100*time.Millisecond)).Error("backoff max delay must be greater than or equal to 100 millisecond")),
	); err != nil {
		return err
	}

	if pc.Backoff.BaseDelay >= pc.Backoff.MaxDelay {
		return fmt.Errorf("backoff base delay must be less than or equal to backoff max delay")
	}

	return nil
}

type DefaultPollingStrategy struct{}

func (s *DefaultPollingStrategy) GetPollingConfigByDeviceType(deviceType string) (PollingConfig, error) {
	switch deviceType {
	case repository.Router:
		return PollingConfig{
			Interval:  30 * time.Second,
			Timeout:   10 * time.Second,
			BatchSize: config.GetPollingBatchSize(),
			Backoff: &BackoffConfig{
				BaseDelay: 1 * time.Second,
				MaxDelay:  120 * time.Second,
				Factor:    2.0,
			},
		}, nil
	case repository.Switch:
		return PollingConfig{
			Interval:  60 * time.Second,
			Timeout:   10 * time.Second,
			BatchSize: config.GetPollingBatchSize(),
			Backoff: &BackoffConfig{
				BaseDelay: 1 * time.Second,
				MaxDelay:  300 * time.Second,
				Factor:    2.0,
			},
		}, nil
	case repository.Camera:
		return PollingConfig{
			Interval:  10 * time.Second,
			Timeout:   3 * time.Second,
			BatchSize: config.GetPollingBatchSize(),
			Backoff: &BackoffConfig{
				BaseDelay: 500 * time.Millisecond,
				MaxDelay:  60 * time.Second,
				Factor:    2.0,
			},
		}, nil
	case repository.DoorAccessSystem:
		return PollingConfig{
			Interval:  10 * time.Second,
			Timeout:   3 * time.Second,
			BatchSize: config.GetPollingBatchSize(),
			Backoff: &BackoffConfig{
				BaseDelay: 500 * time.Millisecond,
				MaxDelay:  30 * time.Second,
				Factor:    2.0,
			},
		}, nil
	default:
		return PollingConfig{}, fmt.Errorf("unsupported device type: %s", deviceType)
	}
}

type DeviceDiagnostics struct {
	Id            uint         `json:"id"`
	DeviceID      string       `json:"device_id"`
	DeviceType    string       `json:"device_type"`
	DeviceHost    string       `json:"device_host"`
	HwVersion     string       `json:"hw_version"`
	SwVersion     string       `json:"sw_version"`
	FwVersion     string       `json:"fw_version"`
	Status        string       `json:"status"`
	Checksum      string       `json:"checksum"`
	Connectivity  Connectivity `json:"connectivity"`
	LastCheckedAt *time.Time   `json:"last_checked_at,omitempty"`
}

type PollingCapability struct {
	Protocol string  `json:"protocol"`
	Port     *int    `json:"port,omitempty"`
	Path     *string `json:"path,omitempty"`
}

type DeviceHealthCheckResponse struct {
	DeviceID     string              `json:"device_id"`
	DeviceType   string              `json:"device_type"`
	Capabilities []PollingCapability `json:"capabilities"`
}

func (resp *DeviceHealthCheckResponse) Validate() error {
	if resp.DeviceID == "" {
		return fmt.Errorf("device_id cannot be empty")
	}
	if resp.DeviceType == "" {
		return fmt.Errorf("device_type cannot be empty")
	}
	if len(resp.Capabilities) == 0 {
		return fmt.Errorf("capabilities cannot be empty")
	}
	for _, capability := range resp.Capabilities {
		if capability.Protocol == "" {
			return fmt.Errorf("protocol cannot be empty")
		}
		if capability.Port != nil && (*capability.Port < 0 || *capability.Port > 65535) {
			return fmt.Errorf("invalid port number: %d", *capability.Port)
		}
	}

	return nil
}
