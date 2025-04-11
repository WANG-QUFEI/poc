package business

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

func GetListOfDevicesDiagnostics(ctx context.Context, repo repository.IRepository, historyCheckingSize int, psy api.IPollingStrategy, page, size int, deviceType string) ([]*api.DeviceDiagnostics, int, error) {
	if page < 0 || size <= 0 {
		return nil, 0, fmt.Errorf("illegal argument: invalid page or size")
	}

	var cond string
	if deviceType != "" {
		cond = fmt.Sprintf("device_type = '%s'", deviceType)
	} else {
		cond = "1=1"
	}

	devices, total, err := repo.GetDevicesByPage(page, size, cond)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get devices by page: %w", err)
	}
	if len(devices) == 0 {
		return nil, 0, nil
	}

	slices.SortFunc(devices, func(d1, d2 repository.Device) int {
		return int(d1.ID - d2.ID)
	})

	diagnostics := make([]*api.DeviceDiagnostics, len(devices))
	wg := sync.WaitGroup{}
	for i := range len(devices) {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			device := devices[idx]
			dia, err := GetDeviceDiagnostic(repo, device, historyCheckingSize, psy)
			if err != nil {
				zerolog.Ctx(ctx).Err(err).Msgf("failed to get device diagnostics for device %s", device.DeviceID)
				return
			}
			diagnostics[idx] = dia
		}(i)
	}
	wg.Wait()
	return lo.Filter(diagnostics, func(d *api.DeviceDiagnostics, _ int) bool {
		return d != nil
	}), total, nil
}

func GetDeviceDiagnostic(repo repository.IRepository, device repository.Device, historyCheckingSize int, psy api.IPollingStrategy) (*api.DeviceDiagnostics, error) {
	cfg, err := psy.GetPollingConfigByDeviceType(device.DeviceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get polling config for device of type %s: %w", device.DeviceType, err)
	}
	if err = cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid polling config for device %s: %w", device.DeviceType, err)
	}

	deviceId := device.DeviceID
	history, err := repo.GetDevicePollingHistory(deviceId, historyCheckingSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get device polling history: %w", err)
	}
	if len(history) == 0 {
		return &api.DeviceDiagnostics{
			Id:           device.ID,
			DeviceID:     deviceId,
			DeviceType:   device.DeviceType,
			DeviceHost:   device.Hostname,
			Connectivity: api.Unknown,
		}, nil
	}

	slices.SortFunc(history, func(h1, h2 repository.PollingHistory) int {
		return -h1.CreatedAt.Compare(h2.CreatedAt)
	})

	latest := history[0]
	if IsDeviceOutOfSync(device, latest, cfg) { // the device has not been polled for a long time
		return &api.DeviceDiagnostics{
			Id:            device.ID,
			DeviceID:      deviceId,
			DeviceType:    device.DeviceType,
			DeviceHost:    device.Hostname,
			Connectivity:  api.Unknown,
			LastCheckedAt: &latest.CreatedAt,
		}, nil
	}

	if IsDeviceAlive(device, latest, cfg) {
		return &api.DeviceDiagnostics{
			Id:            device.ID,
			DeviceID:      deviceId,
			DeviceType:    device.DeviceType,
			DeviceHost:    device.Hostname,
			HwVersion:     lo.FromPtr(latest.HwVersion),
			SwVersion:     lo.FromPtr(latest.SwVersion),
			FwVersion:     lo.FromPtr(latest.FwVersion),
			Status:        lo.FromPtr(latest.DeviceStatus),
			Checksum:      lo.FromPtr(latest.DeviceChecksum),
			Connectivity:  api.Connected,
			LastCheckedAt: &latest.CreatedAt,
		}, nil
	}

	if IsDeviceDisconnected(device, history, cfg) {
		return &api.DeviceDiagnostics{
			Id:            device.ID,
			DeviceID:      deviceId,
			DeviceType:    device.DeviceType,
			DeviceHost:    device.Hostname,
			Connectivity:  api.Disconnected,
			LastCheckedAt: &latest.CreatedAt,
		}, nil
	}

	return &api.DeviceDiagnostics{
		Id:            device.ID,
		DeviceID:      deviceId,
		DeviceType:    device.DeviceType,
		DeviceHost:    device.Hostname,
		Connectivity:  api.Connecting,
		LastCheckedAt: &latest.CreatedAt,
	}, nil
}

func IsDeviceOutOfSync(_ repository.Device, latest repository.PollingHistory, cfg api.PollingConfig) bool {
	// simplified logic for out of sync detection
	return latest.CreatedAt.Before(time.Now().Add(-10 * cfg.Interval))
}

func IsDeviceAlive(_ repository.Device, latest repository.PollingHistory, cfg api.PollingConfig) bool {
	// simplified logic for considering device is alive
	if latest.PollingResult == repository.PollSucceed && latest.CreatedAt.After(time.Now().Add(-2*cfg.Interval)) {
		return true
	}
	return false
}

func IsDeviceDisconnected(_ repository.Device, histories []repository.PollingHistory, _ api.PollingConfig) bool {
	// simplified logic for considering device is disconnected
	numOfEvidences := 10
	if len(histories) < numOfEvidences {
		// not enough history to determine
		return false
	}

	for i := range numOfEvidences {
		if histories[i].PollingResult != repository.PollFailed {
			return false
		}
	}

	return true
}

func AddDevice(ctx context.Context, repo repository.IRepository, client *http.Client, deviceId, deviceType, hostname string, healthCheckPort int) error {
	device, err := repo.GetDeviceByID(deviceId)
	if err != nil && !errors.Is(err, repository.ErrRecordNotFound) {
		return fmt.Errorf("failed to check device db record by deviceId: %w", err)
	}
	if device != nil {
		if device.DeletedAt != nil {
			if err = repo.RestoreDevice(device.ID); err != nil {
				return fmt.Errorf("failed to restore device: %w", err)
			}
		}
		return nil
	}

	path := config.HealthCheckPath()
	path = strings.TrimPrefix(path, "/")
	reqURL := fmt.Sprintf("%s://%s:%d/%s", config.RESTSchema(), hostname, healthCheckPort, path)
	_, err = url.Parse(reqURL)
	if err != nil {
		return fmt.Errorf("failed to parse url %s: %w", reqURL, err)
	}
	header := http.Header{}
	header.Set("Accept", "application/json")

	resp, err := util.SendHttpRequest[api.DeviceHealthCheckResponse](ctx, client, util.HTTPRequestParams{
		Method:       http.MethodGet,
		RequestURL:   reqURL,
		Header:       header,
		DecodeSchema: lo.ToPtr(util.JSON),
	})
	if err != nil {
		return fmt.Errorf("failed to check device health: %w", err)
	}

	healthCheckResp := resp.DecodedValue
	if err = healthCheckResp.Validate(); err != nil {
		return util.HTTPResponseError{
			Code:   resp.Code,
			Header: resp.Header,
			Body:   resp.Body,
			Cause:  fmt.Errorf("invalid health check response: %w", err),
		}
	}
	if healthCheckResp.DeviceID != deviceId {
		return fmt.Errorf("device id mismatch: expected %s, got %s", deviceId, healthCheckResp.DeviceID)
	}
	if healthCheckResp.DeviceType != deviceType {
		return fmt.Errorf("device type mismatch: expected %s, got %s", deviceType, healthCheckResp.DeviceType)
	}

	var restPort, grpcPort *int
	var restPath *string
	protocols := make([]string, 0, len(healthCheckResp.Capabilities))
	for _, cap := range healthCheckResp.Capabilities {
		switch cap.Protocol {
		case repository.REST:
			restPort = cap.Port
			restPath = cap.Path
		case repository.GRPC:
			grpcPort = cap.Port
		}
		protocols = append(protocols, cap.Protocol)
	}

	dt, err := repo.GetDeviceTypeByName(deviceType)
	if err != nil {
		return fmt.Errorf("failed to get device type by name: %w", err)
	}
	if dt == nil {
		if err = repo.CreateDeviceTypes([]*repository.DeviceType{
			{
				Name: deviceType,
			},
		}); err != nil {
			return fmt.Errorf("failed to create device type: %w", err)
		}
	} else if dt.DeletedAt != nil {
		if err = repo.RestoreDeviceType(dt.ID); err != nil {
			return fmt.Errorf("failed to restore device type: %w", err)
		}
	}

	device = &repository.Device{
		DeviceID:   deviceId,
		DeviceType: deviceType,
		Hostname:   hostname,
		Protocols:  pq.StringArray(protocols),
		RestPort:   restPort,
		RestPath:   restPath,
		GrpcPort:   grpcPort,
	}
	if err := repo.CreateDevice(device); err != nil {
		return fmt.Errorf("failed to create device: %w", err)
	}

	return nil
}
