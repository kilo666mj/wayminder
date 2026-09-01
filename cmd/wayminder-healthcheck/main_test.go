package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeErrorBody struct {
	io.Reader
	err error
}

func (body closeErrorBody) Close() error { return body.err }

func TestCheckPreservesStatusAndCloseErrors(t *testing.T) {
	closeErr := errors.New("close response")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       closeErrorBody{Reader: strings.NewReader("not ready"), err: closeErr},
		}, nil
	})}

	err := check(client, "http://wayminder.invalid/readyz")
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("check() error = %v, want status and response close errors", err)
	}
}
