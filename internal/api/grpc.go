package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/proto"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"google.golang.org/grpc"
)

const defaultGrpcRequestTimeout = 30 * time.Second

type GrpcDeviceMonitor struct {
	clientCache map[string]grpcClientWrapper
	dialOpts    []grpc.DialOption
	rwLock      sync.RWMutex
}

type grpcClientWrapper struct {
	client       proto.DeviceMonitorClient
	lastUsedTime *time.Time // can be utilized for cache eviction
}

func NewGrpcDeviceMonitor(opts ...grpc.DialOption) *GrpcDeviceMonitor {
	return &GrpcDeviceMonitor{
		clientCache: make(map[string]grpcClientWrapper),
		dialOpts:    opts,
		rwLock:      sync.RWMutex{},
	}
}

func (g *GrpcDeviceMonitor) PollDevice(ctx context.Context, req PollDeviceRequest) (*PollDeviceResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	port := config.GrpcPort()
	if req.Port != nil {
		port = *req.Port
	}

	c, err := g.getGrpcClient(req.Hostname, port)
	if err != nil {
		return nil, err
	}

	resp, err := c.GetDeviceData(ctx, &proto.DeviceDataRequest{})
	if err != nil {
		return nil, err
	}
	if err = validateGrpcDeviceDataResp(resp); err != nil {
		return nil, err
	}

	return &PollDeviceResponse{
		Id:       *resp.DeviceId,
		Type:     *resp.DeviceType,
		Hw:       *resp.HardwareVersion,
		Sw:       *resp.SoftwareVersion,
		Fw:       *resp.FirmwareVersion,
		Status:   *resp.Status,
		Checksum: *resp.Checksum,
	}, nil
}

func (g *GrpcDeviceMonitor) getGrpcClient(hostname string, port int) (proto.DeviceMonitorClient, error) {
	target := fmt.Sprintf("%s:%d", hostname, port)
	g.rwLock.RLock()
	gw, ok := g.clientCache[target]
	g.rwLock.RUnlock()
	if ok {
		return gw.client, nil
	}

	g.rwLock.Lock()
	if gw, ok = g.clientCache[target]; ok {
		g.rwLock.Unlock()
		return gw.client, nil
	}

	defer g.rwLock.Unlock()
	conn, err := grpc.NewClient(target, g.dialOpts...)
	if err != nil {
		return nil, err
	}

	gw = grpcClientWrapper{
		client: proto.NewDeviceMonitorClient(conn),
	}
	g.clientCache[target] = gw
	return gw.client, nil
}

func validateGrpcDeviceDataResp(resp *proto.DeviceDataResponse) error {
	if resp == nil {
		return fmt.Errorf("%w: device data is nil", ErrInvalidResponse)
	}
	if err := validation.ValidateStruct(resp,
		validation.Field(&resp.DeviceId, validation.Required),
		validation.Field(&resp.DeviceType, validation.Required),
		validation.Field(&resp.HardwareVersion, validation.Required),
		validation.Field(&resp.SoftwareVersion, validation.Required),
		validation.Field(&resp.FirmwareVersion, validation.Required),
		validation.Field(&resp.Status, validation.Required),
		validation.Field(&resp.Checksum, validation.Required),
	); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	return nil
}
