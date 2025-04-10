package worker

import (
	"context"
	"math"
	"math/rand"
	"strings"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

type RetryWrapperMonitor struct {
	failCount int
	monitor   api.IDeviceMonitor
	repo      repository.IRepository
	timeout   time.Duration
	backoff   api.BackoffConfig
}

type failureReason struct {
	Error string `json:"error"`
	Count int    `json:"count"`
}

func (rm *RetryWrapperMonitor) pollDeviceWithBackoff(ctx context.Context, device *repository.Device, pollReq api.PollDeviceRequest) {
	start := time.Now()
	delay := rm.backoff.BaseDelay

	for {
		reqCtx, cancel := context.WithTimeout(ctx, rm.timeout)
		resp, err := rm.monitor.PollDevice(reqCtx, pollReq)
		cancel()

		device.LastCheckedAt = lo.ToPtr(time.Now())
		var history *repository.PollingHistory
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msgf("failed to poll device data on attempt %d", rm.failCount+1)
			reason := failureReason{
				Error: err.Error(),
				Count: rm.failCount + 1,
			}
			reasonJSON := util.JSONMarshalIgnoreErr(reason)
			history = &repository.PollingHistory{
				DeviceID:      device.DeviceID,
				PollingResult: repository.PollFailed,
				FailureReason: lo.ToPtr(string(reasonJSON)),
			}
		} else if resp != nil {
			data := jsonizePollingResult(*resp)
			zerolog.Ctx(ctx).Info().
				RawJSON("device_data", data).
				Str("duration", time.Since(start).String()).
				Msgf("successfully polled device data on attempt %d", rm.failCount+1)
			device.PollingStatus = lo.ToPtr(repository.PollingDone)
			history = &repository.PollingHistory{
				DeviceID:       device.DeviceID,
				HwVersion:      &resp.Hw,
				SwVersion:      &resp.Sw,
				FwVersion:      &resp.Fw,
				DeviceStatus:   &resp.Status,
				DeviceChecksum: &resp.Checksum,
				PollingResult:  repository.PollSucceed,
			}
		} else {
			zerolog.Ctx(ctx).Error().Msg("inconsistency state: response from device monitor is nil, will abort polling")
		}

		if cErr := rm.repo.CreatePollingHistory(history); cErr != nil {
			zerolog.Ctx(ctx).Err(cErr).Msg("db error: failed to save device polling result")
		}

		if uErr := rm.repo.UpdateDevice(device); uErr != nil {
			zerolog.Ctx(ctx).Err(uErr).Msg("db error: failed to update device database record")
		}

		if err == nil {
			break
		}

		// backoff time with jitter, got idea from https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
		rm.failCount++
		if delay < rm.backoff.MaxDelay {
			n := float64(delay) * rm.backoff.Factor
			n = math.Min(n, float64(rm.backoff.MaxDelay))
			delay = time.Duration(n)
		} else {
			delay = rm.backoff.MaxDelay
		}

		sleep := time.Duration(rand.Int63n(int64(delay)))
		select {
		case <-time.After(sleep):
			zerolog.Ctx(ctx).Info().Int("retry_count", rm.failCount).Msgf("retry polling device %s after sleeping %s", device.DeviceID, sleep.String())
			continue

		case <-ctx.Done():
			zerolog.Ctx(ctx).Info().Msgf("stop polling device %s, context cancelled", device.DeviceID)
			// Update device's polling status to cancelled
			device.PollingStatus = lo.ToPtr(repository.PollingCancelled)
			if uErr := rm.repo.UpdateDevice(device); uErr != nil {
				zerolog.Ctx(ctx).Err(uErr).Msg("db error: failed to update device polling status to 'cancelled'")
			}
			return
		}
	}
}

func jsonizePollingResult(resp api.PollDeviceResponse) []byte {
	copy := resp
	// Mask the device checksum for security reasons
	if len(copy.Checksum) > 2 {
		blur := strings.Repeat("*", len(copy.Checksum)-2)
		copy.Checksum = copy.Checksum[:1] + blur + copy.Checksum[len(copy.Checksum)-1:]
	}

	return util.JSONMarshalIgnoreErr(copy)
}
