package api_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/proto"
	"example.poc/device-monitoring-system/test/helper"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var usedPort = map[int]struct{}{}

var errNoDeviceInfo = status.Error(codes.NotFound, "no device info")

type grpcDeviceMonitorTestSuite struct {
	suite.Suite
	gdm  *api.GrpcDeviceMonitor
	sdms *helper.SimpleDeviceMonitorServer
}

func (s *grpcDeviceMonitorTestSuite) SetupSuite() {
	port := randPort()
	s.T().Setenv("GRPC_PORT", fmt.Sprintf("%d", port))

	s.sdms = &helper.SimpleDeviceMonitorServer{}
	s.sdms.SetPort(port)
	s.gdm = api.NewGrpcDeviceMonitor(
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	go func() {
		if err := s.sdms.Start(); err != nil {
			s.T().Logf("simpleDeviceMonitorServer stopped with error: %v", err)
		}
	}()
}

func (s *grpcDeviceMonitorTestSuite) SetupTest() {
	s.sdms.SetError(nil)
	s.sdms.SetDelay(0)
	s.sdms.SetResponse(nil)
}

func (s *grpcDeviceMonitorTestSuite) TearDownSuite() {
	s.sdms.Stop()
}

func TestGrpcDeviceMonitor(t *testing.T) {
	suite.Run(t, new(grpcDeviceMonitorTestSuite))
}

func (s *grpcDeviceMonitorTestSuite) TestErrorResponse() {
	s.sdms.SetError(errNoDeviceInfo)
	req := api.PollDeviceRequest{
		Hostname: "localhost",
		Port:     lo.ToPtr(config.GrpcPort()),
	}
	_, err := s.gdm.PollDevice(s.T().Context(), req)
	s.Error(err)
	s.ErrorIs(err, errNoDeviceInfo)
}

func (s *grpcDeviceMonitorTestSuite) TestNilResponse() {
	req := api.PollDeviceRequest{
		Hostname: "localhost",
		Port:     lo.ToPtr(config.GrpcPort()),
	}
	_, err := s.gdm.PollDevice(s.T().Context(), req)
	s.Error(err)
	s.ErrorIs(err, api.ErrInvalidResponse)
}

func (s *grpcDeviceMonitorTestSuite) TestTimeout() {
	s.sdms.SetDelay(100 * time.Millisecond)
	req := api.PollDeviceRequest{
		Hostname: "localhost",
		Port:     lo.ToPtr(config.GrpcPort()),
	}

	ctx := s.T().Context()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err := s.gdm.PollDevice(ctx, req)
	s.Error(err)
	s.Contains(err.Error(), "context deadline exceeded")
}

func (s *grpcDeviceMonitorTestSuite) TestSuccessResponse() {
	deviceID := uuid.NewString()
	status := "operational"
	deviceType := repository.Router
	hwVersion := helper.RandomString(10)
	swVersion := helper.RandomString(10)
	fwVersion := helper.RandomString(10)
	checksum := helper.RandomString(30)

	s.sdms.SetResponse(&proto.DeviceDataResponse{
		DeviceId:        &deviceID,
		DeviceType:      &deviceType,
		HardwareVersion: &hwVersion,
		SoftwareVersion: &swVersion,
		FirmwareVersion: &fwVersion,
		Status:          &status,
		Checksum:        &checksum,
	})

	req := api.PollDeviceRequest{
		Hostname: "localhost",
		Port:     lo.ToPtr(config.GrpcPort()),
	}

	resp, err := s.gdm.PollDevice(s.T().Context(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(deviceID, resp.Id)
	s.Equal(deviceType, resp.Type)
	s.Equal(hwVersion, resp.Hw)
	s.Equal(swVersion, resp.Sw)
	s.Equal(fwVersion, resp.Fw)
	s.Equal(status, resp.Status)
	s.Equal(checksum, resp.Checksum)
}

func randPort() int {
	port := 50000 + rand.Intn(1000)
	if _, ok := usedPort[port]; ok {
		return randPort()
	}
	usedPort[port] = struct{}{}
	return port
}
