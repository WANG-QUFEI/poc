package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.poc/device-monitoring-system/internal/api"
	"example.poc/device-monitoring-system/internal/business"
	"example.poc/device-monitoring-system/internal/config"
	"example.poc/device-monitoring-system/internal/repository"
	"example.poc/device-monitoring-system/internal/util"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

const defaultHistoryCheckingSize = 20

type Router struct {
	httpClint *http.Client
	repo      repository.IRepository
	psy       api.IPollingStrategy
	router    chi.Router
}

type HTTPClientOptions func(*http.Client)

func NewRouter(opts ...HTTPClientOptions) (*Router, error) {
	repo, err := repository.NewRepository(config.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("failed to get db connection: %w", err)
	}

	c := &http.Client{}
	for _, opt := range opts {
		opt(c)
	}

	r := &Router{
		repo:      repo,
		psy:       &api.DefaultPollingStrategy{},
		httpClint: c,
	}
	r.router = r.getHandler()

	return r, nil
}

func (ro *Router) getHandler() chi.Router {
	mux := chi.NewRouter()
	mux.Put("/devices", ro.handleAddDevices)
	mux.Delete("/devices/{device_id}", ro.handleDeleteDevice)
	mux.Get("/devices/{device_id}", ro.handleGetDeviceByID)
	mux.Get("/devices", ro.handleListingDevices)

	return mux
}

func (ro *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ro.router.ServeHTTP(w, r)
}

func (ro *Router) handleGetDeviceByID(w http.ResponseWriter, r *http.Request) {
	deviceId := chi.URLParam(r, "device_id")
	if deviceId == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}

	deviceId = strings.ReplaceAll(deviceId, " ", "")
	device, err := ro.repo.GetDeviceByID(deviceId)
	if errors.Is(err, repository.ErrRecordNotFound) || device == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get device: %v", err), http.StatusInternalServerError)
		return
	}

	dia, err := business.GetDeviceDiagnostic(ro.repo, *device, defaultHistoryCheckingSize, ro.psy)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get device diagnostics: %v", err), http.StatusInternalServerError)
		return
	}

	util.ResponseAsJSON(w, http.StatusOK, *dia)
}

func (ro *Router) handleListingDevices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	paramPage := q.Get("page")
	paramSize := q.Get("size")
	paramDt := q.Get("device_type")

	var page, size int
	var err error
	if paramPage == "" {
		page = 0
	} else {
		page, err = strconv.Atoi(paramPage)
		if err != nil || page < 0 {
			http.Error(w, "invalid page number", http.StatusBadRequest)
			return
		}
	}

	if paramSize == "" {
		size = 30
	} else {
		size, err = strconv.Atoi(paramSize)
		if err != nil || size <= 0 {
			http.Error(w, "invalid size number", http.StatusBadRequest)
			return
		}
		if size > 1000 {
			http.Error(w, "size number is too large", http.StatusBadRequest)
			return
		}
	}

	dias, total, err := business.GetListOfDevicesDiagnostics(r.Context(), ro.repo, defaultHistoryCheckingSize, ro.psy, page, size, paramDt)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get devices diagnostics: %v", err), http.StatusInternalServerError)
		return
	}

	resp := deviceListingResponse{
		Page:  page,
		Size:  size,
		Total: total,
		Items: dias,
	}
	util.ResponseAsJSON(w, http.StatusOK, resp)
}

func (ro *Router) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	deviceId := chi.URLParam(r, "device_id")
	if deviceId == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}

	deviceId = strings.ReplaceAll(deviceId, " ", "")
	device, err := ro.repo.GetDeviceByID(deviceId)
	if errors.Is(err, repository.ErrRecordNotFound) || device == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to find device: %v", err), http.StatusInternalServerError)
		return
	}

	device.DeletedAt = lo.ToPtr(time.Now())
	if err := ro.repo.UpdateDevice(device); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete device: %v", err), http.StatusInternalServerError)
		return
	}
}

func (ro *Router) handleAddDevices(w http.ResponseWriter, r *http.Request) {
	var req addDevicesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to json decode request: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Devices) == 0 {
		util.ResponseAsJSON(w, http.StatusOK, addDevicesResponse{Results: []deviceAddingResult{}})
		return
	}

	m := make(map[string]deviceInfo)
	for _, device := range req.Devices {
		if err := device.normalize(); err != nil {
			http.Error(w, fmt.Sprintf("request validation error for item %+v: %v", device, err), http.StatusBadRequest)
			return
		}
		m[device.DeviceID] = device
	}

	// get error code by error, simplified logic
	fnErrCode := func(err error) int {
		if errors.Is(err, context.DeadlineExceeded) {
			return 1
		}
		return 2
	}

	var wg sync.WaitGroup
	results := make([]deviceAddingResult, len(m))
	i := 0
	for _, device := range m {
		wg.Add(1)
		i++
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), config.HealthCheckTimeout())
			defer cancel()

			result := deviceAddingResult{
				DeviceID:   device.DeviceID,
				DeviceType: device.DeviceType,
				Hostname:   device.Hostname,
			}
			if err := business.AddDevice(ctx, ro.repo, ro.httpClint, device.DeviceID, device.DeviceType, device.Hostname); err != nil {
				deviceInfo := util.JSONMarshalIgnoreErr(device)
				zerolog.Ctx(r.Context()).Err(err).RawJSON("device_info", deviceInfo).Msgf("failed to add device")
				result.Code = fnErrCode(err)
				result.Error = err.Error()
			}
			results[idx] = result
		}(i - 1)
	}
	wg.Wait()

	util.ResponseAsJSON(w, http.StatusOK, addDevicesResponse{Results: results})
}
