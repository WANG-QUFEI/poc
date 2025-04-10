package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"example.poc/device-monitoring-system/test/helper"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type routerTestSuite struct {
	suite.Suite
	repo   *repository.Repo
	router *Router
	helper *helper.Helper
}

func (s *routerTestSuite) SetupSuite() {
	repo, err := repository.NewRepository(config.DatabaseURL())
	if err != nil {
		s.T().Fatalf("failed to get db connection: %v", err)
	}
	s.repo = repo

	deviceTypes := []repository.DeviceType{
		{
			Name: repository.Router,
		},
		{
			Name: repository.Switch,
		},
		{
			Name: repository.Camera,
		},
		{
			Name: repository.DoorAccessSystem,
		},
	}
	err = repo.Conn().Clauses(clause.OnConflict{DoNothing: true}).Create(&deviceTypes).Error
	if err != nil {
		s.T().Fatalf("failed to initialize device types: %v", err)
	}

	ro, err := NewRouter()
	if err != nil {
		s.T().Fatalf("failed to create router: %v", err)
	}
	s.router = ro
}

func (s *routerTestSuite) SetupTest() {
	if err := clearDB(s.repo.Conn()); err != nil {
		s.T().Fatalf("failed to clear db tables between running tests: %v", err)
	}
}

func TestRouter(t *testing.T) {
	suite.Run(t, new(routerTestSuite))
}

func (s *routerTestSuite) TestGetDeviceByID() {
	// no device
	req := httptest.NewRequest(http.MethodGet, "/devices/device1", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	s.Equal(http.StatusNotFound, w.Code)

	// insert device data
	d := repository.Device{
		DeviceID:   "device1",
		DeviceType: repository.Router,
		Hostname:   "localhost",
		Protocols:  pq.StringArray([]string{"http", "grpc"}),
		RestPort:   lo.ToPtr(8999),
		GrpcPort:   lo.ToPtr(50051),
	}
	err := s.repo.CreateDevice(&d)
	s.NoError(err)

	// device exists, no polling history
	req = httptest.NewRequest(http.MethodGet, "/devices/device1", nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	s.Equal(http.StatusOK, w.Code)

	var diagnostics api.DeviceDiagnostics
	s.helper.MustDecodeJSON(w.Body.Bytes(), &diagnostics)
	s.Equal(d.DeviceID, diagnostics.DeviceID)
	s.Equal(api.Unknown, diagnostics.Connectivity)

	// insert polling history data, make it looks connected
	ph := repository.PollingHistory{
		DeviceID:       d.DeviceID,
		HwVersion:      lo.ToPtr(helper.RandomString(10)),
		SwVersion:      lo.ToPtr(helper.RandomString(10)),
		FwVersion:      lo.ToPtr(helper.RandomString(10)),
		DeviceChecksum: lo.ToPtr(helper.RandomString(32)),
		DeviceStatus:   lo.ToPtr("running"),
		PollingResult:  repository.PollSucceed,
	}
	err = s.repo.CreatePollingHistory(&ph)
	s.NoError(err)

	req = httptest.NewRequest(http.MethodGet, "/devices/device1", nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	s.Equal(http.StatusOK, w.Code)

	s.helper.MustDecodeJSON(w.Body.Bytes(), &diagnostics)
	s.Equal(d.DeviceID, diagnostics.DeviceID)
	s.Equal(api.Connected, diagnostics.Connectivity)
}

func (s *routerTestSuite) TestListingDevices() {
	d1 := repository.Device{
		DeviceID:   "device1",
		DeviceType: repository.Router,
		Hostname:   "localhost1",
		Protocols:  pq.StringArray([]string{"http", "grpc"}),
	}
	d2 := repository.Device{
		DeviceID:   "device2",
		DeviceType: repository.Switch,
		Hostname:   "localhost2",
		Protocols:  pq.StringArray([]string{"http", "grpc"}),
	}
	d3 := repository.Device{
		DeviceID:   "device3",
		DeviceType: repository.DoorAccessSystem,
		Hostname:   "localhost3",
		Protocols:  pq.StringArray([]string{"http", "grpc"}),
	}
	err := s.repo.CreateDevices([]*repository.Device{&d1, &d2, &d3})
	s.NoError(err)

	d1Interval, err := s.router.psy.GetPollingConfigByDeviceType(d1.DeviceType)
	s.NoError(err)

	d2Interval, err := s.router.psy.GetPollingConfigByDeviceType(d1.DeviceType)
	s.NoError(err)

	d3Interval, err := s.router.psy.GetPollingConfigByDeviceType(d1.DeviceType)
	s.NoError(err)

	d1Histories := []*repository.PollingHistory{
		{
			DeviceID:       d1.DeviceID,
			HwVersion:      lo.ToPtr(helper.RandomString(10)),
			SwVersion:      lo.ToPtr(helper.RandomString(10)),
			FwVersion:      lo.ToPtr(helper.RandomString(10)),
			DeviceChecksum: lo.ToPtr(helper.RandomString(32)),
			DeviceStatus:   lo.ToPtr("running"),
			PollingResult:  repository.PollSucceed,
			CreatedAt:      time.Now(),
		},
		{
			DeviceID:      d1.DeviceID,
			PollingResult: repository.PollFailed,
			CreatedAt:     time.Now().Add(-3 * d1Interval.Interval),
		},
	}
	err = s.repo.CreatePollingHistories(d1Histories)
	s.NoError(err)

	var d2Histories []*repository.PollingHistory
	for i := range 20 {
		d2History := repository.PollingHistory{
			DeviceID:      d2.DeviceID,
			PollingResult: repository.PollFailed,
			CreatedAt:     time.Now().Add(-time.Duration(i) * d2Interval.Interval),
		}
		d2Histories = append(d2Histories, &d2History)
	}
	err = s.repo.CreatePollingHistories(d2Histories)
	s.NoError(err)

	var d3Histories []*repository.PollingHistory
	for i := range 20 {
		var r repository.PollingResult
		if i%2 == 0 {
			r = repository.PollFailed
		} else {
			r = repository.PollSucceed
		}

		d3History := repository.PollingHistory{
			DeviceID:      d3.DeviceID,
			PollingResult: r,
			CreatedAt:     time.Now().Add(-time.Duration(i) * d3Interval.Interval),
		}
		d3Histories = append(d3Histories, &d3History)
	}
	err = s.repo.CreatePollingHistories(d3Histories)
	s.NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	s.Equal(http.StatusOK, w.Code)

	var listingResp deviceListingResponse
	s.helper.MustDecodeJSON(w.Body.Bytes(), &listingResp)
	s.Equal(3, listingResp.Total)
	s.Equal(3, len(listingResp.Items))

	for _, item := range listingResp.Items {
		if item.DeviceID == d1.DeviceID {
			s.Equal(api.Connected, item.Connectivity)
			continue
		}
		if item.DeviceID == d2.DeviceID {
			s.Equal(api.Disconnected, item.Connectivity)
			continue
		}
		if item.DeviceID == d3.DeviceID {
			s.Equal(api.Connecting, item.Connectivity)
			continue
		}
	}
}

func clearDB(db *gorm.DB) error {
	s := strings.Join([]string{"devices", "polling_history"}, ",")
	q := fmt.Sprintf("truncate table %s restart identity cascade", s)
	return db.Exec(q).Error
}

func (s *routerTestSuite) TestAddDevice() {
	s.Run("bad_case_invalid_input", s.addDeviceInvalidInput)
	s.Run("add_3_devices_with_one_succeed", s.add3DevicesWithOneSucceed)
}

func (s *routerTestSuite) addDeviceInvalidInput() {
	reqObj := addDevicesRequest{
		Devices: []deviceInfo{
			{
				DeviceID:   "           ", // intentionally left blank
				DeviceType: "router",
				Hostname:   "localhost1",
			},
			{
				DeviceID:   "device2",
				DeviceType: "switch",
				Hostname:   "localhost2",
			},
		},
	}

	reqBody := getReader(reqObj)
	req := httptest.NewRequest(http.MethodPut, "/devices", reqBody)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	s.Equal(http.StatusBadRequest, w.Code)
	s.T().Logf("expected error: %s", w.Body.String())
}

func (s *routerTestSuite) add3DevicesWithOneSucceed() {
	s.T().Setenv("HEALTH_CHECK_TIMEOUT", "100ms")
	healthCheckPath := config.HealthCheckPath()

	h1 := chi.NewRouter()
	h1.Get(healthCheckPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server1: internal error"))
	})
	s1 := httptest.NewServer(h1)
	defer s1.Close()

	h2 := chi.NewRouter()
	h2.Get(healthCheckPath, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * config.HealthCheckTimeout())
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("server2: ok, but slow and invalid response"))
	})
	s2 := httptest.NewServer(h2)
	defer s2.Close()

	restPort := 8080
	grpcPort := 50055
	h3 := chi.NewRouter()
	h3.Get(healthCheckPath, func(w http.ResponseWriter, r *http.Request) {
		resp := api.DeviceHealthCheckResponse{
			DeviceID:   "device3",
			DeviceType: repository.DoorAccessSystem,
			Capabilities: []api.PollingCapability{
				{
					Protocol: repository.REST,
					Port:     &restPort,
				},
				{
					Protocol: repository.GRPC,
					Port:     &grpcPort,
				},
			},
		}
		util.ResponseAsJSON(w, http.StatusOK, resp)
	})
	s3 := httptest.NewServer(h3)
	defer s3.Close()

	u1, _ := url.Parse(s1.URL)
	u2, _ := url.Parse(s2.URL)
	u3, _ := url.Parse(s3.URL)

	reqObj := addDevicesRequest{
		Devices: []deviceInfo{
			{
				DeviceID:   "device1", // intentionally left blank
				DeviceType: repository.Router,
				Hostname:   u1.Host,
			},
			{
				DeviceID:   "device2",
				DeviceType: repository.Switch,
				Hostname:   u2.Host,
			},
			{
				DeviceID:   "device3",
				DeviceType: repository.DoorAccessSystem,
				Hostname:   u3.Host,
			},
		},
	}

	reqBody := getReader(reqObj)
	req := httptest.NewRequest(http.MethodPut, "/devices", reqBody)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	s.Equal(http.StatusOK, w.Code)
	var resp addDevicesResponse
	s.helper.MustDecodeJSON(w.Body.Bytes(), &resp)
	s.Equal(3, len(resp.Results))

	for _, result := range resp.Results {
		if result.DeviceID == "device3" {
			s.Equal(0, result.Code)
			s.Equal("", result.Error)
		} else {
			s.NotEqual(0, result.Code)
			s.T().Logf("expected error for device %s: %s", result.DeviceID, result.Error)
		}
	}

	device, err := s.repo.GetDeviceByID("device3")
	s.NoError(err)
	s.NotNil(device)
	s.Equal(repository.DoorAccessSystem, device.DeviceType)
	s.Equal(restPort, *device.RestPort)
	s.Equal(grpcPort, *device.GrpcPort)
}

func getReader(a any) io.Reader {
	if a == nil {
		return nil
	}
	bs, err := json.Marshal(a)
	if err != nil {
		panic(fmt.Errorf("json marshal failed: %v", err))
	}
	return bytes.NewBuffer(bs)
}
