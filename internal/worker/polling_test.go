package worker

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/test/helper"
	"example.poc/device-monitoring-system/test/mocks"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type pollingWorkerTestSuite struct {
	suite.Suite
	ctx      context.Context
	worker   *PollingWorker
	tp       *testPollingStrategy
	repo     *repository.Repo
	mockRest *mocks.MockIDeviceMonitor
	mockGrpc *mocks.MockIDeviceMonitor
	tl       *helper.TestLogger
}

type testPollingStrategy struct {
	configMap map[string]api.PollingConfig
}

func (tp *testPollingStrategy) GetPollingConfigByDeviceType(deviceType string) (api.PollingConfig, error) {
	if config, ok := tp.configMap[deviceType]; ok {
		return config, nil
	}
	return api.PollingConfig{}, fmt.Errorf("device type %s not found", deviceType)
}

func (s *pollingWorkerTestSuite) SetupSuite() {
	repo, err := repository.NewRepository(config.DatabaseURL())
	if err != nil {
		s.T().Fatalf("failed to get db connection: %v", err)
	}
	if err = clearDB(repo.Conn()); err != nil {
		s.T().Fatalf("failed to clear db: %v", err)
	}
	if err = initTestDB(repo); err != nil {
		s.T().Fatalf("failed to init test db: %v", err)
	}

	s.repo = repo
	s.tp = &testPollingStrategy{configMap: make(map[string]api.PollingConfig)}
	s.worker = &PollingWorker{
		repo:     repo,
		interval: 3 * time.Second,
	}
	s.tl = helper.NewTestLogger()
	s.ctx = s.tl.ZeroLogger().WithContext(context.Background()) // attach test logger to the context
}

func (s *pollingWorkerTestSuite) SetupTest() {
	if err := s.repo.Conn().Delete(&repository.PollingHistory{}, "1 = 1").Error; err != nil {
		s.T().Fatalf("failed to clear polling history: %v", err)
	}

	s.mockRest = mocks.NewMockIDeviceMonitor(s.T())
	s.mockGrpc = mocks.NewMockIDeviceMonitor(s.T())
	s.worker.rest = s.mockRest
	s.worker.grpc = s.mockGrpc
	s.tl.Flush()
}

func TestPollingWorker(t *testing.T) {
	suite.Run(t, new(pollingWorkerTestSuite))
}

func (s *pollingWorkerTestSuite) TestMockReliableDevices() {
	allDeviceTypes, err := s.repo.GetAllDeviceTypes()
	s.NoError(err)

	devicePollingInterval := 100 * time.Millisecond
	cfg := api.PollingConfig{
		Interval:  devicePollingInterval,
		Timeout:   1 * time.Second,
		BatchSize: 10,
		Backoff: &api.BackoffConfig{
			BaseDelay: 1 * time.Second,
			Factor:    2.0,
			MaxDelay:  60 * time.Second,
		},
	}
	for _, dt := range allDeviceTypes {
		s.tp.configMap[dt.Name] = cfg
	}
	s.worker.psy = s.tp

	s.mockGrpc.EXPECT().PollDevice(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req api.PollDeviceRequest) (*api.PollDeviceResponse, error) {
		return getMockDeviceDataResp(req), nil
	})

	s.mockRest.EXPECT().PollDevice(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req api.PollDeviceRequest) (*api.PollDeviceResponse, error) {
		return getMockDeviceDataResp(req), nil
	})

	allowRunningTime := 10 * devicePollingInterval
	ctx, cancel := context.WithCancel(s.ctx)
	go func() {
		time.Sleep(allowRunningTime)
		cancel()
	}()

	s.worker.Start(ctx)

	var allDevices []repository.Device
	if err := s.repo.Conn().Find(&allDevices).Error; err != nil {
		s.T().Fatalf("failed to get all devices: %v", err)
	}

	for _, device := range allDevices {
		history, err := s.repo.GetDevicePollingHistory(device.DeviceID, 10)
		s.NoError(err)
		s.LessOrEqual(5, len(history)) // we have 10x running time of the polling interval, so having 3 records is reasonable
		for _, h := range history {
			s.Equal(device.DeviceID, h.DeviceID)
			s.Equal(repository.PollSucceed, h.PollingResult)
		}
	}

	// s.T().Log("============== logging results ==============")
	// lines := s.tl.GetLogLines()
	// for _, line := range lines {
	// 	s.T().Log(line)
	// }
}

func (s *pollingWorkerTestSuite) TestMockUnReliableDevices() {
	dts, err := s.repo.GetAllDeviceTypes()
	s.NoError(err)

	pollingInterval := 100 * time.Millisecond
	pollingTimeout := 100 * time.Millisecond
	cfg := api.PollingConfig{
		Interval:  pollingInterval,
		Timeout:   pollingTimeout,
		BatchSize: 10,
		Backoff: &api.BackoffConfig{
			BaseDelay: 100 * time.Millisecond,
			Factor:    4.0,
			MaxDelay:  1 * time.Second,
		},
	}
	for _, dt := range dts {
		s.tp.configMap[dt.Name] = cfg
	}
	s.worker.psy = s.tp

	lock := &sync.Mutex{}
	deviceMap := make(map[string]int)

	run := func(_ context.Context, req api.PollDeviceRequest) (*api.PollDeviceResponse, error) {
		key := fmt.Sprintf("%s:%d", req.Hostname, *req.Port)
		var count int
		lock.Lock()
		if c, ok := deviceMap[key]; ok {
			count = c
			deviceMap[key] = c + 1
		} else {
			count = 1
			deviceMap[key] = count
		}
		lock.Unlock()

		if count%3 == 0 {
			return nil, fmt.Errorf("device %s is not reachable", key)
		}
		if count%3 == 1 {
			// mock slow response
			time.Sleep(2 * pollingTimeout)
			return nil, fmt.Errorf("device %s is not reachable", key)
		}

		return getMockDeviceDataResp(req), nil
	}

	s.mockGrpc.EXPECT().PollDevice(mock.Anything, mock.Anything).RunAndReturn(run)
	s.mockRest.EXPECT().PollDevice(mock.Anything, mock.Anything).RunAndReturn(run)

	allowRunningTime := 20 * pollingInterval
	ctx, cancel := context.WithCancel(s.ctx)
	go func() {
		time.Sleep(allowRunningTime)
		cancel()
	}()

	s.worker.Start(ctx)

	var allDevices []repository.Device
	if err := s.repo.Conn().Find(&allDevices).Error; err != nil {
		s.T().Fatalf("failed to get all devices: %v", err)
	}

	for _, device := range allDevices {
		total := 0
		numOfSuccess := 0
		history, err := s.repo.GetDevicePollingHistory(device.DeviceID, 100)
		s.NoError(err)
		for _, h := range history {
			total++
			if h.PollingResult == repository.PollSucceed {
				numOfSuccess++
			}
		}
		s.Greater(numOfSuccess, 0)
		s.LessOrEqual(numOfSuccess, total/3)
	}

	// s.T().Log("============== logging results ==============")
	// lines := s.tl.GetLogLines()
	// for _, line := range lines {
	// 	s.T().Log(line)
	// }
}

func getMockDeviceDataResp(req api.PollDeviceRequest) *api.PollDeviceResponse {
	return &api.PollDeviceResponse{
		Hw:       helper.RandomString(10),
		Sw:       helper.RandomString(10),
		Fw:       helper.RandomString(15),
		Checksum: helper.RandomString(32),
		Status:   "running",
	}
}

func initTestDB(repo *repository.Repo) error {
	dts := []*repository.DeviceType{
		{Name: repository.Router},
		{Name: repository.Switch},
		{Name: repository.Camera},
		{Name: repository.DoorAccessSystem},
	}
	if err := repo.CreateDeviceTypes(dts); err != nil {
		return fmt.Errorf("failed to create device types: %w", err)
	}

	// for each device type, we create 3 devices
	for _, dt := range dts {
		for range 3 {
			protos := []string{repository.REST, repository.GRPC}
			randProto := protos[rand.Intn(len(protos))]
			device := repository.Device{
				DeviceID:   helper.RandomString(10),
				DeviceType: dt.Name,
				Hostname:   fmt.Sprintf("%s.example.com", helper.RandomString(10)),
				Protocols:  pq.StringArray{randProto},
			}

			restPort := 50000 + rand.Intn(1000)
			gRpcPort := 60000 + rand.Intn(1000)
			if randProto == repository.REST {
				device.RestPort = &restPort
				device.RestPath = lo.ToPtr("/api/v1/device")
			} else {
				device.GrpcPort = &gRpcPort
			}

			if err := repo.CreateDevice(&device); err != nil {
				return fmt.Errorf("failed to create device: %w", err)
			}
		}
	}

	return nil
}
