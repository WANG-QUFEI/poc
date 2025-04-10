package helper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"example.poc/device-monitoring-system/proto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

type SimpleDeviceMonitorServer struct {
	gs    *grpc.Server
	port  int
	err   error
	resp  *proto.DeviceDataResponse
	delay time.Duration
	proto.UnimplementedDeviceMonitorServer
}

func (s *SimpleDeviceMonitorServer) GetDeviceData(context.Context, *proto.DeviceDataRequest) (*proto.DeviceDataResponse, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *SimpleDeviceMonitorServer) SetPort(port int) {
	s.port = port
}

func (s *SimpleDeviceMonitorServer) SetError(err error) {
	s.err = err
}

func (s *SimpleDeviceMonitorServer) SetResponse(resp *proto.DeviceDataResponse) {
	s.resp = resp
}

func (s *SimpleDeviceMonitorServer) SetDelay(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	s.delay = delay
}

func (s *SimpleDeviceMonitorServer) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "localhost", s.port))
	if err != nil {
		return err
	}

	s.gs = grpc.NewServer()
	proto.RegisterDeviceMonitorServer(s.gs, s)
	return s.gs.Serve(lis)
}

func (s *SimpleDeviceMonitorServer) Stop() {
	if s.gs != nil {
		s.gs.Stop()
	}
}

func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var result []byte
	for range length {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		result = append(result, charset[idx.Int64()])
	}
	return string(result)
}

type TestLogger struct {
	buf *bytes.Buffer
}

func NewTestLogger() *TestLogger {
	return &TestLogger{
		buf: new(bytes.Buffer),
	}
}

func (tl *TestLogger) ZeroLogger() *zerolog.Logger {
	lg := log.Logger.Output(tl.buf)
	return &lg
}

func (tl *TestLogger) GetLogLines() (lines []string) {
	scanner := bufio.NewScanner(tl.buf)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return
}

func (tl *TestLogger) Flush() {
	_, _ = io.ReadAll(tl.buf)
}

type Helper struct {
	t *testing.T
}

func NewHelper(t *testing.T) *Helper {
	return &Helper{t: t}
}

func (h *Helper) MustDecodeJSON(bs []byte, v any) {
	if v == nil {
		h.t.Fatalf("value is nil")
	}
	if err := json.Unmarshal(bs, v); err != nil {
		h.t.Fatalf("failed to decode json: %v", err)
	}
}
