package pkg

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"example.poc/device-monitoring-system/proto"
	"example.poc/device-monitoring-system/test/helper"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var states = []string{"operating", "rebooting", "loading configuration", "internal error", "offline"}

var deviceTypes = []string{
	repository.Router,
	repository.Switch,
	repository.Camera,
	repository.DoorAccessSystem,
}

type DeviceSimulator struct {
	r                chi.Router
	gRpcPort         int
	restPort         int
	restPath         string
	stateIdx         int
	deviceID         string
	deviceType       string
	hwVersion        string
	swVersion        string
	fwVersion        string
	checksum         string
	transitionPeriod time.Duration
	proto.UnimplementedDeviceMonitorServer
}

func NewDeviceSimulator() *DeviceSimulator {
	var checksum string
	bs, err := ExecuteExternalChecksumGenerator()
	if err != nil {
		log.Error().Err(err).Msg("failed to execute external checksum generator, use a random one")
		checksum = helper.RandomString(32)
	}
	checksum = string(bs)

	n := rand.Intn(len(deviceTypes))
	ds := &DeviceSimulator{
		gRpcPort:         config.GrpcPort(),
		restPort:         config.RESTApiPort(),
		restPath:         config.RESTApiPath(),
		deviceID:         uuid.NewString(),
		deviceType:       deviceTypes[n],
		hwVersion:        helper.RandomString(10),
		swVersion:        helper.RandomString(10),
		fwVersion:        helper.RandomString(10),
		checksum:         checksum,
		transitionPeriod: time.Second * 10,
	}
	ds.r = ds.getRouter()

	return ds
}

func (ds *DeviceSimulator) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", ds.gRpcPort))
	if err != nil {
		return fmt.Errorf("failed to listen to port %d: %w", ds.gRpcPort, err)
	}

	gs := grpc.NewServer()
	proto.RegisterDeviceMonitorServer(gs, ds)
	go func() {
		if err := gs.Serve(lis); err != nil {
			log.Error().Err(err).Msgf("failed to serve gRPC on port: %d", ds.gRpcPort)
		}
	}()

	go func() {
		ticker := time.NewTicker(ds.transitionPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ds.stateIdx = (ds.stateIdx + 1) % len(states)
				log.Info().Msgf("Device state changed to: %s", states[ds.stateIdx])
			case <-ctx.Done():
				log.Info().Msg("Stopping device simulator due to context being cancelled")
			}
		}
	}()

	if err = http.ListenAndServe(fmt.Sprintf(":%d", ds.restPort), ds); err != nil {
		return fmt.Errorf("failed to serve HTTP on port %d: %w", ds.restPort, err)
	}

	return nil
}

func (ds *DeviceSimulator) GetDeviceData(ctx context.Context, req *proto.DeviceDataRequest) (*proto.DeviceDataResponse, error) {
	switch states[ds.stateIdx] {
	case "operating", "rebooting", "loading configuration":
		return &proto.DeviceDataResponse{
			DeviceId:        &ds.deviceID,
			DeviceType:      &ds.deviceType,
			HardwareVersion: &ds.hwVersion,
			SoftwareVersion: &ds.swVersion,
			FirmwareVersion: &ds.fwVersion,
			Status:          &states[ds.stateIdx],
			Checksum:        &ds.checksum,
		}, nil
	case "internal error":
		return nil, status.Error(codes.Internal, "simulated internal error")
	case "offline":
		time.Sleep(60 * time.Second)
		return nil, status.Error(codes.Unavailable, "simulated timeout error")
	default:
		return nil, status.Error(codes.Unknown, "unknown internal state")
	}
}

func (ds *DeviceSimulator) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ds.r.ServeHTTP(w, req)
}

func (ds *DeviceSimulator) getRouter() chi.Router {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		protos := os.Getenv("PROTOCOLS")
		if protos == "" {
			http.Error(w, "no protocol capabilities configured", http.StatusInternalServerError)
			return
		}

		caps := make([]api.PollingCapability, 0)
		parts := strings.SplitSeq(protos, ",")
		for pro := range parts {
			if strings.EqualFold(pro, "grpc") {
				caps = append(caps, api.PollingCapability{
					Protocol: "grpc",
					Port:     &ds.gRpcPort,
				})
			}
			if strings.EqualFold(pro, "rest") {
				caps = append(caps, api.PollingCapability{
					Protocol: "rest",
					Port:     &ds.restPort,
					Path:     &ds.restPath,
				})
			}
		}

		resp := api.DeviceHealthCheckResponse{
			DeviceID:     ds.deviceID,
			DeviceType:   ds.deviceType,
			Capabilities: caps,
		}
		util.ResponseAsJSON(w, http.StatusOK, resp)
	})

	r.Get(ds.restPath, func(w http.ResponseWriter, r *http.Request) {
		switch states[ds.stateIdx] {
		case "operating", "rebooting", "loading configuration":
			resp := api.RestPollDeviceResponse{
				Id:       ds.deviceID,
				Type:     ds.deviceType,
				Hw:       ds.hwVersion,
				Sw:       ds.swVersion,
				Fw:       ds.fwVersion,
				Status:   states[ds.stateIdx],
				Checksum: ds.checksum,
			}
			util.ResponseAsJSON(w, http.StatusOK, resp)
		case "internal error":
			http.Error(w, "simulated internal error", http.StatusInternalServerError)
		case "offline":
			time.Sleep(60 * time.Second)
			http.Error(w, "simulated timeout error", http.StatusServiceUnavailable)
		default:
			http.Error(w, "unknown internal state", http.StatusNotFound)
		}
	})

	return r
}
