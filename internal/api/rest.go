package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/util"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/samber/lo"
)

const defaultRESTRequestTimeout = 30 * time.Second

type RESTDeviceMonitor struct {
	client *http.Client
}

type HTTPClientOptions func(*http.Client)

func NewRESTDeviceMonitor(opts ...HTTPClientOptions) *RESTDeviceMonitor {
	c := &http.Client{}
	if len(opts) > 0 {
		for _, opt := range opts {
			opt(c)
		}
	}
	return &RESTDeviceMonitor{client: c}
}

type RestPollDeviceResponse struct {
	Id       string `json:"device_id"`
	Type     string `json:"device_type"`
	Hw       string `json:"hardware_version"`
	Sw       string `json:"software_version"`
	Fw       string `json:"firmware_version"`
	Status   string `json:"status"`
	Checksum string `json:"checksum"`
}

func (r *RESTDeviceMonitor) PollDevice(ctx context.Context, info PollDeviceRequest) (*PollDeviceResponse, error) {
	if err := info.validate(); err != nil {
		return nil, err
	}

	port := config.RESTApiPort()
	if info.Port != nil {
		port = *info.Port
	}

	path := config.RESTApiPath()
	if info.Path != nil && len(*info.Path) > 0 {
		path = *info.Path
	}
	path = strings.TrimPrefix(path, "/")
	reqURL := fmt.Sprintf("%s://%s:%d/%s", config.RESTSchema(), info.Hostname, port, path)
	u, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request URL '%s': %w", reqURL, err)
	}

	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, defaultRESTRequestTimeout)
		defer cancel()
	}

	header := http.Header{}
	header.Set("Accept", "application/json")
	resp, err := util.SendHttpRequest[RestPollDeviceResponse](ctx, r.client, util.HTTPRequestParams{
		Method:       http.MethodGet,
		RequestURL:   u.String(),
		Header:       header,
		DecodeSchema: lo.ToPtr(util.JSON),
	})
	if err != nil {
		return nil, err
	}

	v := resp.DecodedValue
	if err = validateRESTDeviceDataResp(&v); err != nil {
		return nil, util.HTTPResponseError{
			Code:  resp.Code,
			Body:  resp.Body,
			Cause: err,
		}
	}

	return &PollDeviceResponse{
		Id:       v.Id,
		Type:     v.Type,
		Hw:       v.Hw,
		Sw:       v.Sw,
		Fw:       v.Fw,
		Status:   v.Status,
		Checksum: v.Checksum,
	}, nil
}

func validateRESTDeviceDataResp(resp *RestPollDeviceResponse) error {
	if resp == nil {
		return fmt.Errorf("%w: device data response is nil", ErrInvalidResponse)
	}

	if err := validation.ValidateStruct(resp,
		validation.Field(&resp.Id, validation.Required),
		validation.Field(&resp.Type, validation.Required),
		validation.Field(&resp.Hw, validation.Required),
		validation.Field(&resp.Sw, validation.Required),
		validation.Field(&resp.Fw, validation.Required),
		validation.Field(&resp.Status, validation.Required),
		validation.Field(&resp.Checksum, validation.Required),
	); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	return nil
}
