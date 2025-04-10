package util

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"

	"github.com/rs/zerolog/log"
)

type SerializationSchema int

const (
	JSON SerializationSchema = iota
	URLEncoded
)

var ErrEmptyResponseBody = fmt.Errorf("empty response body")

type HTTPRequestParams struct {
	Method       string
	RequestURL   string
	Header       http.Header
	URLParams    url.Values
	RequestBody  any
	EncodeFunc   func(any) ([]byte, error)
	EncodeSchema *SerializationSchema
	DecodeFunc   func([]byte) (any, error)
	DecodeSchema *SerializationSchema
}

type HTTPResponse[T any] struct {
	Code         int
	Header       http.Header
	Body         []byte
	DecodedValue T
}

type HTTPResponseError struct {
	Code   int
	Header http.Header
	Body   []byte
	Cause  error
}

func (err HTTPResponseError) Error() string {
	return fmt.Sprintf("unexpected http response, code: %d, body: '%s', cause: %v", err.Code, err.Body, err.Cause)
}

func IsErr(err, target error) bool {
	if errors.Is(err, target) {
		return true
	}
	var httpErr HTTPResponseError
	if errors.As(err, &httpErr) {
		return IsErr(httpErr.Cause, target)
	}
	return false
}

func (params HTTPRequestParams) validate() error {
	if params.Method == "" {
		return fmt.Errorf("field Method cannot be empty")
	}
	if params.RequestURL == "" {
		return fmt.Errorf("field RequestURL cannot be empty")
	}
	if _, err := url.Parse(params.RequestURL); err != nil {
		return fmt.Errorf("unparsable RequestURL '%s': %v", params.RequestURL, err)
	}
	if params.EncodeSchema != nil {
		switch *params.EncodeSchema {
		case JSON, URLEncoded:
		default:
			return fmt.Errorf("unsupported EncodeSchema: %v", *params.EncodeSchema)
		}
	}
	if params.DecodeSchema != nil && *params.DecodeSchema != JSON {
		return fmt.Errorf("unsupported DecodeSchema: %v", *params.DecodeSchema)
	}

	return nil
}

func SendHttpRequest[T any](ctx context.Context, client *http.Client, params HTTPRequestParams) (*HTTPResponse[T], error) {
	if client == nil {
		return nil, fmt.Errorf("http client cannot be nil")
	}
	if err := params.validate(); err != nil {
		return nil, fmt.Errorf("invalid argument HTTPRequestParams: %v", err)
	}

	reqURL := params.RequestURL
	if len(params.URLParams) > 0 {
		reqURL += "?" + params.URLParams.Encode()
	}

	reqBody, err := getRequestBody(params)
	if err != nil {
		return nil, fmt.Errorf("failed to get request body: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, params.Method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header = params.Header

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read from response body: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, HTTPResponseError{
			Code:   resp.StatusCode,
			Header: resp.Header,
			Body:   body,
			Cause:  fmt.Errorf("non 2xx response"),
		}
	}

	var t T
	if params.DecodeSchema != nil || params.DecodeFunc != nil {
		if len(body) == 0 {
			return nil, HTTPResponseError{
				Code:   resp.StatusCode,
				Header: resp.Header,
				Body:   body,
				Cause:  ErrEmptyResponseBody,
			}
		}
		if params.DecodeSchema != nil {
			if *params.DecodeSchema != JSON {
				return nil, fmt.Errorf("unsupported DecodeSchema: only JSON decoding is supported")
			}
			if err := json.Unmarshal(body, &t); err != nil {
				return nil, HTTPResponseError{
					Code:   resp.StatusCode,
					Header: resp.Header,
					Body:   body,
					Cause:  fmt.Errorf("failed to json unmarshal response body: %v", err),
				}
			}
		} else {
			if a, err := params.DecodeFunc(body); err != nil {
				return nil, HTTPResponseError{
					Code:   resp.StatusCode,
					Header: resp.Header,
					Body:   body,
					Cause:  fmt.Errorf("failed to decode response body with the passed decoding function: %v", err),
				}
			} else if v, ok := a.(T); ok {
				t = v
			} else {
				return nil, HTTPResponseError{
					Code:   resp.StatusCode,
					Header: resp.Header,
					Body:   body,
					Cause:  fmt.Errorf("unexpected type of the returned value from the decoding function, want: %T, got: %T, value: '%v'", t, a, a),
				}
			}
		}
	}

	return &HTTPResponse[T]{
		Code:         resp.StatusCode,
		Header:       resp.Header,
		Body:         body,
		DecodedValue: t,
	}, nil
}

func getRequestBody(params HTTPRequestParams) (io.Reader, error) {
	if mayHaveRequestBody(params.Method) && !IsNil(params.RequestBody) {
		switch {
		case params.EncodeSchema != nil:
			switch *params.EncodeSchema {
			case JSON:
				if bs, err := json.Marshal(params.RequestBody); err != nil {
					return nil, fmt.Errorf("failed to json encode request body: %v", err)
				} else {
					return bytes.NewReader(bs), nil
				}
			case URLEncoded:
				if v, ok := params.RequestBody.(url.Values); ok {
					return bytes.NewBufferString(v.Encode()), nil
				} else {
					return nil, fmt.Errorf("RequestBody is expected to be of type url.Values when EncodeSchema is URLEncoded")
				}
			default:
				return nil, fmt.Errorf("unsupported EncodeSchema: %v", *params.EncodeSchema)
			}
		case params.EncodeFunc != nil:
			if bs, err := params.EncodeFunc(params.RequestBody); err != nil {
				return nil, fmt.Errorf("failed to encode request body using the passed encoding function: %v", err)
			} else {
				return bytes.NewReader(bs), nil
			}
		default:
			if r, ok := params.RequestBody.(io.Reader); ok {
				return r, nil
			} else {
				return nil, fmt.Errorf("RequestBody is expected to be of type io.Reader when EncodeSchema and EncodeFunc are not provided")
			}
		}
	}

	return nil, nil
}

func ResponseAsJSON(w http.ResponseWriter, status int, a any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if !IsNil(a) {
		err := json.NewEncoder(w).Encode(a)
		if err != nil {
			log.Err(err).Msg("json encoding error")
		}
	}
}

func mayHaveRequestBody(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

func IsNil(a any) bool {
	return a == nil || (reflect.ValueOf(a).Kind() == reflect.Ptr && reflect.ValueOf(a).IsNil())
}
