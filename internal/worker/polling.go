package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"github.com/rs/zerolog"
)

type PollingWorker struct {
	repo repository.IRepository
	rest api.IDeviceMonitor
	grpc api.IDeviceMonitor
	psy  api.IPollingStrategy
}

func NewPollingWorker(pollingStrategy api.IPollingStrategy) (*PollingWorker, error) {
	repo, err := repository.NewRepository(config.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	if pollingStrategy == nil {
		pollingStrategy = &api.DefaultPollingStrategy{}
	}

	return &PollingWorker{
		repo: repo,
		rest: api.NewRESTDeviceMonitor(),
		grpc: api.NewGrpcDeviceMonitor(),
		psy:  pollingStrategy,
	}, nil
}

func (w *PollingWorker) Start(ctx context.Context) error {
	dts, err := w.repo.GetAllDeviceTypes()
	if err != nil {
		return fmt.Errorf("failed to get all device types: %w", err)
	}
	if len(dts) == 0 {
		return fmt.Errorf("no device types configured in the database")
	}

	m := make(map[string]api.PollingConfig)
	for _, dt := range dts {
		cfg, err := w.psy.GetPollingConfigByDeviceType(dt.Name)
		if err != nil {
			return fmt.Errorf("failed to get polling config for device type %s: %v", dt.Name, err)
		}
		if err = cfg.Validate(); err != nil {
			return fmt.Errorf("invalid polling config for device type %s: %v", dt.Name, err)
		}

		m[dt.Name] = cfg
	}

	var wg sync.WaitGroup
	for dt, cfg := range m {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx := zerolog.Ctx(ctx).With().
				Str("component", "device_polling_worker").
				Str("device_type", dt).
				Str("polling_interval", cfg.Interval.String()).
				Str("polling_timeout", cfg.Timeout.String()).
				Str("backoff_base_delay", cfg.Backoff.BaseDelay.String()).
				Str("backoff_max_delay", cfg.Backoff.MaxDelay.String()).
				Float64("backoff_factor", cfg.Backoff.Factor).
				Int("polling_batch_size", cfg.BatchSize).Logger().WithContext(ctx)
			w.startPollingDevicesByType(subCtx, dt, cfg)
		}()
	}
	wg.Wait()

	return ctx.Err()
}

func (w *PollingWorker) startPollingDevicesByType(ctx context.Context, deviceType string, cfg api.PollingConfig) {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			devices, err := w.repo.GetDevicesByPollingParameter(repository.DevicePollingParameter{
				DeviceType: deviceType,
				Interval:   cfg.Interval,
				Limit:      cfg.BatchSize,
			})
			if err != nil {
				zerolog.Ctx(ctx).Error().Err(err).Msgf("failed to get devices for type %s", deviceType)
				continue
			}

			if len(devices) == 0 {
				zerolog.Ctx(ctx).Info().Msgf("no devices found for type %s", deviceType)
				continue
			}

			for _, device := range devices {
				zCtx := zerolog.Ctx(ctx).With().
					Str("device_id", device.DeviceID).
					Str("hostname", device.Hostname).
					Str("protocols", fmt.Sprintf("%v", device.Protocols))
				if device.RestPort != nil {
					zCtx.Int("rest_port", *device.RestPort)
				}
				if device.GrpcPort != nil {
					zCtx.Int("grpc_port", *device.GrpcPort)
				}
				if device.RestPath != nil && len(*device.RestPath) > 0 {
					zCtx.Str("rest_path", *device.RestPath)
				}

				subCtx := zCtx.Logger().WithContext(ctx)
				if err := w.pollDevice(subCtx, device, cfg); err != nil {
					zerolog.Ctx(subCtx).Err(err).Msgf("failed to poll device %s", device.DeviceID)
					continue
				}
			}
		case <-ctx.Done():
			zerolog.Ctx(ctx).Info().Msgf("stopping polling devices of type %s, context cancelled", deviceType)
			return
		}
	}
}

func (w *PollingWorker) pollDevice(ctx context.Context, device repository.Device, cfg api.PollingConfig) error {
	var port *int
	var path *string
	var inner api.IDeviceMonitor

	for _, protocol := range device.Protocols {
		switch protocol {
		case repository.REST:
			inner = w.rest
			port = device.RestPort
			path = device.RestPath
		case repository.GRPC:
			inner = w.grpc
			port = device.GrpcPort
		default:
			zerolog.Ctx(ctx).Warn().Msgf("unsupported protocol %s of device %s", protocol, device.DeviceID)
		}
		if inner != nil {
			break
		}
	}
	if inner == nil {
		return fmt.Errorf("no supported protocol found for device %s", device.DeviceID)
	}

	retry := &RetryWrapperMonitor{
		monitor: inner,
		repo:    w.repo,
		timeout: cfg.Timeout,
		backoff: *cfg.Backoff,
	}

	go retry.pollDeviceWithBackoff(ctx, &device, api.PollDeviceRequest{
		Hostname: device.Hostname,
		Port:     port,
		Path:     path,
	})

	return nil
}
