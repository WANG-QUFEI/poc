package web

import (
	"fmt"
	"strings"

	"example.poc/device-monitoring-system/internal/api"
)

type addDevicesRequest struct {
	Devices []deviceInfo `json:"devices"`
}

type addDevicesResponse struct {
	Results []deviceAddingResult `json:"results"`
}

type deviceInfo struct {
	DeviceID        string `json:"device_id"`
	DeviceType      string `json:"device_type"`
	Hostname        string `json:"hostname"`
	HealthCheckPort int    `json:"health_check_port"`
}

type deviceAddingResult struct {
	DeviceID   string `json:"device_id"`
	DeviceType string `json:"device_type"`
	Hostname   string `json:"hostname"`
	Code       int    `json:"code"`
	Error      string `json:"error,omitempty"`
}

func (info *deviceInfo) normalize() error {
	info.DeviceID = strings.ReplaceAll(info.DeviceID, " ", "")
	info.DeviceType = strings.ReplaceAll(info.DeviceType, " ", "")
	info.Hostname = strings.ReplaceAll(info.Hostname, " ", "")
	if info.DeviceID == "" {
		return fmt.Errorf("device_id cannot be empty")
	}
	if info.DeviceType == "" {
		return fmt.Errorf("device_type cannot be empty")
	}
	if info.Hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if info.HealthCheckPort < 0 || info.HealthCheckPort > 65535 {
		return fmt.Errorf("health_check_port must be between 0 and 65535")
	}

	return nil
}

type deviceListingResponse struct {
	Page  int                      `json:"page"`
	Size  int                      `json:"size"`
	Total int                      `json:"total"`
	Items []*api.DeviceDiagnostics `json:"items,omitempty"`
}
