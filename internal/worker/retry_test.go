package worker

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/test/helper"
	"example.poc/device-monitoring-system/test/mocks"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
)

type retryWrapperMonitorTestSuite struct {
	suite.Suite
	rm          *RetryWrapperMonitor
	mockMonitor *mocks.MockIDeviceMonitor
	mockRepo    *mocks.MockIRepository
}

func (s *retryWrapperMonitorTestSuite) SetupSuite() {
	s.rm = &RetryWrapperMonitor{
		timeout: 30 * time.Second,
	}
}

func (s *retryWrapperMonitorTestSuite) SetupTest() {
	s.mockMonitor = mocks.NewMockIDeviceMonitor(s.T())
	s.mockRepo = mocks.NewMockIRepository(s.T())
	s.rm.monitor = s.mockMonitor
	s.rm.repo = s.mockRepo
}

type testDeviceDto struct {
	deviceID   string
	deviceType string
	deviceHost string
	restPort   int
	grpcPort   int
	restPath   string
	hwVersion  string
	swVersion  string
	fwVersion  string
	checksum   string
	status     string
}

func TestRetryWrapperMonitor(t *testing.T) {
	suite.Run(t, new(retryWrapperMonitorTestSuite))
}

func (s *retryWrapperMonitorTestSuite) TestPollOnceSucceed() {
	testDto := randTestDeviceDto("running", "type-1", "some.faked.host")
	device := repository.Device{
		ID:            1,
		DeviceID:      testDto.deviceID,
		DeviceType:    testDto.deviceType,
		Hostname:      testDto.deviceHost,
		RestPort:      &testDto.restPort,
		GrpcPort:      &testDto.grpcPort,
		RestPath:      &testDto.restPath,
		PollingStatus: lo.ToPtr(repository.PollingInProgress),
		Protocols:     pq.StringArray([]string{"rest", "grpc"}),
	}

	s.mockMonitor.EXPECT().PollDevice(mock.Anything, mock.Anything).Return(&api.PollDeviceResponse{
		Id:       device.DeviceID,
		Type:     device.DeviceType,
		Hw:       testDto.hwVersion,
		Sw:       testDto.swVersion,
		Fw:       testDto.fwVersion,
		Status:   testDto.status,
		Checksum: testDto.checksum,
	}, nil).Once()

	s.mockRepo.EXPECT().CreatePollingHistory(mock.Anything).Return(nil).Run(func(history *repository.PollingHistory) {
		s.NotNil(history)
		s.Equal(testDto.deviceID, history.DeviceID)
		s.Equal(testDto.hwVersion, *history.HwVersion)
		s.Equal(testDto.swVersion, *history.SwVersion)
		s.Equal(repository.PollSucceed, history.PollingResult)
	}).Once()

	s.mockRepo.EXPECT().UpdateDevice(mock.Anything).Return(nil).Run(func(device *repository.Device) {
		s.NotNil(device)
		s.Equal(testDto.deviceID, device.DeviceID)
		s.Equal(repository.PollingDone, *device.PollingStatus)
	}).Once()

	ch := make(chan struct{})
	go func() {
		s.rm.pollDeviceWithBackoff(context.TODO(), &device, api.PollDeviceRequest{
			Hostname: device.Hostname,
			Port:     device.RestPort,
			Path:     device.RestPath,
		})
		ch <- struct{}{}
	}()

	select {
	case <-time.After(3 * time.Second):
		s.T().Fatal("test timed out")
	case <-ch:
	}
}

func (s *retryWrapperMonitorTestSuite) TestPoll3Times() {
	s.rm.backoff = api.BackoffConfig{
		BaseDelay: 100 * time.Millisecond,
		Factor:    3,
		MaxDelay:  1 * time.Second,
	}

	testDto := randTestDeviceDto("running", "type-1", "some.faked.host")
	device := repository.Device{
		ID:            1,
		DeviceID:      testDto.deviceID,
		DeviceType:    testDto.deviceType,
		Hostname:      testDto.deviceHost,
		PollingStatus: lo.ToPtr(repository.PollingInProgress),
		Protocols:     pq.StringArray([]string{"rest", "grpc"}),
	}

	s.mockMonitor.EXPECT().PollDevice(mock.Anything, mock.Anything).Return(nil, fmt.Errorf("fake error")).Twice()
	s.mockMonitor.EXPECT().PollDevice(mock.Anything, mock.Anything).Return(&api.PollDeviceResponse{
		Id:       device.DeviceID,
		Type:     device.DeviceType,
		Hw:       testDto.hwVersion,
		Sw:       testDto.swVersion,
		Fw:       testDto.fwVersion,
		Status:   testDto.status,
		Checksum: testDto.checksum,
	}, nil).Once()

	s.mockRepo.EXPECT().CreatePollingHistory(mock.Anything).Return(nil).Run(func(history *repository.PollingHistory) {
		s.NotNil(history)
		s.Equal(testDto.deviceID, history.DeviceID)
		s.Equal(repository.PollFailed, history.PollingResult)
		s.NotNil(history.FailureReason)
		s.Contains(*history.FailureReason, "fake error")
	}).Twice()
	s.mockRepo.EXPECT().CreatePollingHistory(mock.Anything).Return(nil).Run(func(history *repository.PollingHistory) {
		s.NotNil(history)
		s.Equal(testDto.deviceID, history.DeviceID)
		s.Equal(repository.PollSucceed, history.PollingResult)
	}).Once()

	s.mockRepo.EXPECT().UpdateDevice(mock.Anything).Run(func(device *repository.Device) {
		s.Equal(repository.PollingInProgress, *device.PollingStatus)
	}).Return(nil).Twice()
	s.mockRepo.EXPECT().UpdateDevice(mock.Anything).Return(nil).Run(func(device *repository.Device) {
		s.Equal(repository.PollingDone, *device.PollingStatus)
	}).Once()

	ch := make(chan struct{})
	go func() {
		s.rm.pollDeviceWithBackoff(context.TODO(), &device, api.PollDeviceRequest{})
		ch <- struct{}{}
	}()

	select {
	case <-time.After(3 * time.Second):
		s.T().Fatal("test timed out")
	case <-ch:
	}
}

func (s *retryWrapperMonitorTestSuite) TestContextCancelled() {
	s.rm.backoff = api.BackoffConfig{
		BaseDelay: 100 * time.Millisecond,
		Factor:    3,
		MaxDelay:  10 * time.Second,
	}

	testDto := randTestDeviceDto("running", "type-1", "some.faked.host")
	device := repository.Device{
		ID:            1,
		DeviceID:      testDto.deviceID,
		DeviceType:    testDto.deviceType,
		Hostname:      testDto.deviceHost,
		PollingStatus: lo.ToPtr(repository.PollingInProgress),
		Protocols:     pq.StringArray([]string{"rest", "grpc"}),
	}

	s.mockMonitor.EXPECT().PollDevice(mock.Anything, mock.Anything).Return(nil, fmt.Errorf("fake error: service unavailable"))

	s.mockRepo.EXPECT().CreatePollingHistory(mock.Anything).Return(nil)

	s.mockRepo.EXPECT().UpdateDevice(mock.Anything).Return(nil)

	ch := make(chan struct{})
	ctx, cancel := context.WithCancel(context.TODO())

	go func() {
		s.rm.pollDeviceWithBackoff(ctx, &device, api.PollDeviceRequest{})
		ch <- struct{}{}
	}()

	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	select {
	case <-time.After(3 * time.Second):
		s.T().Fatal("test timed out")
	case <-ch:
	}

	// verify that the device's status is set to PollingCancelled
	s.Equal(repository.PollingCancelled, *device.PollingStatus)
}

func randTestDeviceDto(status, deviceType, host string) testDeviceDto {
	return testDeviceDto{
		deviceID:   helper.RandomString(8),
		deviceType: deviceType,
		deviceHost: host,
		restPort:   50000 + rand.Intn(1000),
		grpcPort:   60000 + rand.Intn(1000),
		restPath:   "/monitoring",
		hwVersion:  helper.RandomString(10),
		swVersion:  helper.RandomString(10),
		fwVersion:  helper.RandomString(10),
		checksum:   helper.RandomString(32),
		status:     status,
	}
}

func clearDB(db *gorm.DB) error {
	s := strings.Join([]string{"device_types", "devices", "polling_history"}, ",")
	q := fmt.Sprintf("truncate table %s restart identity cascade", s)
	return db.Exec(q).Error
}
