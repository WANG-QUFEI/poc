package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"example.poc/device-monitoring-system/test/helper"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type restDeviceMonitorTestSuite struct {
	suite.Suite
	restDeviceMonitor *api.RESTDeviceMonitor
}

func TestRESTDeviceMonitor(t *testing.T) {
	suite.Run(t, new(restDeviceMonitorTestSuite))
}

func (s *restDeviceMonitorTestSuite) TestConnectionFailed() {
	s.restDeviceMonitor = api.NewRESTDeviceMonitor(func(c *http.Client) {
		c.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, fmt.Errorf("dial failed")
			},
		}
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	req := api.PollDeviceRequest{
		Hostname: u.Hostname(),
		Path:     &u.Path,
		Port:     &port,
	}
	_, err := s.restDeviceMonitor.PollDevice(context.Background(), req)
	s.Error(err)
	s.T().Logf("expected error: %v", err)
}

func (s *restDeviceMonitorTestSuite) TestInvalidResponseBody() {
	s.restDeviceMonitor = api.NewRESTDeviceMonitor()
	h := chi.NewRouter()
	h.Get(config.RESTApiPath(), func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("invalid json format: not json object"))
	})
	server := httptest.NewServer(h)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	req := api.PollDeviceRequest{
		Hostname: u.Hostname(),
		Path:     &u.Path,
		Port:     &port,
	}

	_, err := s.restDeviceMonitor.PollDevice(context.Background(), req)
	s.Error(err)
	s.Contains(err.Error(), "failed to json unmarshal response body")
}

func (s *restDeviceMonitorTestSuite) TestInvalidResponseData() {
	deviceID := uuid.NewString()
	deviceType := repository.DoorAccessSystem
	hwVersion := "1.0"
	swVersion := "1.0"
	fwVersion := "1.0"
	status := "active"

	s.restDeviceMonitor = api.NewRESTDeviceMonitor()
	h := chi.NewRouter()
	h.Get(config.RESTApiPath(), func(w http.ResponseWriter, r *http.Request) {
		resp := api.RestPollDeviceResponse{
			Id:       deviceID,
			Type:     deviceType,
			Hw:       hwVersion,
			Sw:       swVersion,
			Fw:       fwVersion,
			Status:   status,
			Checksum: "",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(h)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	req := api.PollDeviceRequest{
		Hostname: u.Hostname(),
		Port:     lo.ToPtr(port),
	}
	_, err := s.restDeviceMonitor.PollDevice(context.Background(), req)
	s.Error(err)
	var hErr util.HTTPResponseError
	s.ErrorAs(err, &hErr)
	s.ErrorIs(hErr.Cause, api.ErrInvalidResponse)
	s.T().Logf("expected error: %v", err)
}

func (s *restDeviceMonitorTestSuite) TestValidResponse() {
	deviceID := uuid.NewString()
	status := "active"
	deviceType := repository.DoorAccessSystem
	hwVersion := helper.RandomString(8)
	swVersion := helper.RandomString(8)
	fwVersion := helper.RandomString(8)
	checksum := helper.RandomString(32)

	s.restDeviceMonitor = api.NewRESTDeviceMonitor()
	h := chi.NewRouter()
	h.Get(config.RESTApiPath(), func(w http.ResponseWriter, r *http.Request) {
		resp := api.RestPollDeviceResponse{
			Id:       deviceID,
			Type:     deviceType,
			Hw:       hwVersion,
			Sw:       swVersion,
			Fw:       fwVersion,
			Status:   status,
			Checksum: checksum,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(h)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	req := api.PollDeviceRequest{
		Hostname: u.Hostname(),
		Port:     &port,
	}

	resp, err := s.restDeviceMonitor.PollDevice(context.Background(), req)
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
