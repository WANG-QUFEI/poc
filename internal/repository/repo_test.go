package repository_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type dbTestSuite struct {
	suite.Suite
	repo repository.IRepository
}

func (s *dbTestSuite) SetupSuite() {
	// s.T().Setenv("ENABLE_GORM_LOGGING", "true")
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
}

func (s *dbTestSuite) SetupTest() {
	db := s.repo.(*repository.Repo).Conn()
	if err := clearDB(db); err != nil {
		s.T().Fatalf("failed to clear database tables: %v", err)
	}
}

func TestRepository(t *testing.T) {
	suite.Run(t, new(dbTestSuite))
}

func (s *dbTestSuite) TestGetDeviceByDID() {
	deviceID := "test-device-id"
	_, err := s.repo.GetDeviceByID(deviceID)
	s.ErrorIs(err, repository.ErrRecordNotFound)

	device := repository.Device{
		DeviceID:   deviceID,
		DeviceType: repository.Router,
		Hostname:   "localhost",
		Protocols:  pq.StringArray([]string{"http", "grpc"}),
	}
	err = s.repo.CreateDevice(&device)
	s.NoError(err)

	d, err := s.repo.GetDeviceByID(deviceID)
	s.NoError(err)
	s.Equal(deviceID, d.DeviceID)
}

func (s *dbTestSuite) TestGetAllDeviceTypes() {
	allTypes, err := s.repo.GetAllDeviceTypes()
	s.NoError(err)
	s.Len(allTypes, 4)
}

func (s *dbTestSuite) TestGetDevicesByPollingParameter() {
	pollingInterval := 10 * time.Second
	outdatedPeriod := 30 * time.Second
	limit := 5

	param := repository.DevicePollingParameter{
		DeviceType:     repository.Router,
		Interval:       pollingInterval,
		OutdatedPeriod: &outdatedPeriod,
		Limit:          limit,
	}

	devices, err := s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 0)

	d1 := repository.Device{
		DeviceID:   uuid.NewString(),
		DeviceType: repository.Router,
		Hostname:   "zimpler.com",
		Protocols:  pq.StringArray([]string{"grpc"}),
	}
	err = s.repo.CreateDevice(&d1)
	s.NoError(err)

	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 1)

	d1 = devices[0]
	d1.LastCheckedAt = lo.ToPtr(time.Now().Add(-pollingInterval / 2))
	d1.PollingStatus = nil
	err = s.repo.UpdateDevice(&d1)
	s.NoError(err)
	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 0)

	d1.LastCheckedAt = nil
	d1.PollingStatus = lo.ToPtr(repository.PollingInProgress)
	err = s.repo.UpdateDevice(&d1)
	s.NoError(err)
	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 0)

	d1.LastCheckedAt = nil
	d1.PollingStatus = lo.ToPtr(repository.PollingInProgress)
	d1.CreatedAt = time.Now().Add(-outdatedPeriod - 10*time.Millisecond)
	err = s.repo.UpdateDevice(&d1)
	s.NoError(err)
	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 1)

	d1.PollingStatus = lo.ToPtr(repository.PollingDone)
	d1.LastCheckedAt = lo.ToPtr(time.Now().Add(-pollingInterval - 10*time.Millisecond))
	err = s.repo.UpdateDevice(&d1)
	s.NoError(err)
	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 1)

	d1.PollingStatus = lo.ToPtr(repository.PollingInProgress)
	d1.LastCheckedAt = lo.ToPtr(time.Now().Add(-outdatedPeriod - 10*time.Millisecond))
	err = s.repo.UpdateDevice(&d1)
	s.NoError(err)
	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, 1)

	var otherDevices []*repository.Device
	for range 10 {
		d := d1
		d.ID = 0
		d.DeviceID = uuid.NewString()
		d.LastCheckedAt = nil
		d.PollingStatus = &repository.PollingDone
		otherDevices = append(otherDevices, &d)
	}
	err = s.repo.CreateDevices(otherDevices)
	s.NoError(err)

	devices, err = s.repo.GetDevicesByPollingParameter(param)
	s.NoError(err)
	s.Len(devices, param.Limit)
}

func (s *dbTestSuite) TestGetDevicesByPage() {
	var devices []*repository.Device
	for range 1000 {
		d := repository.Device{
			DeviceID:   uuid.NewString(),
			DeviceType: repository.Router,
			Hostname:   "localhost",
			Protocols:  pq.StringArray([]string{"http", "grpc"}),
		}
		devices = append(devices, &d)
	}
	err := s.repo.CreateDevices(devices)
	s.NoError(err)

	page := 89
	size := 10
	condition := fmt.Sprintf("device_type = '%s'", repository.Router)
	got, total, err := s.repo.GetDevicesByPage(page, size, condition)
	s.NoError(err)
	s.Len(got, size)
	s.Equal(1000, total)

	slices.SortFunc(got, func(d1, d2 repository.Device) int {
		return int(d1.ID - d2.ID)
	})
	s.Equal(uint(891), got[0].ID)

	size = 100
	got, total, err = s.repo.GetDevicesByPage(page, size, condition)
	s.NoError(err)
	s.Len(got, 0)
}

func clearDB(db *gorm.DB) error {
	s := strings.Join([]string{"devices", "polling_history"}, ",")
	q := fmt.Sprintf("truncate table %s restart identity cascade", s)
	return db.Exec(q).Error
}
