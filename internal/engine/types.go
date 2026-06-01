package engine

import (
	"errors"
	"net/http"
)

// Headers is a single-value-per-key header map.
type Headers map[string]string

// Query holds URL query parameters (multi-value per key).
type Query map[string][]string

// Response is the result of a completed HTTP call.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// OK reports whether the status code is in the 2xx range.
func (r *Response) OK() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// HealthCheck validates a completed response.
type HealthCheck func(resp *Response) bool

var (
	ErrCircuitOpen        = errors.New("httpc: circuit breaker is open")
	ErrNoBaseURLs         = errors.New("httpc: no base URLs configured")
	ErrEmptyURL           = errors.New("httpc: empty URL")
	ErrHealthCheck        = errors.New("httpc: health check failed")
	ErrUnexpectedResponse = errors.New("httpc: unexpected circuit breaker response")
)

func ResolveHealthCheck(defaultCheck, requestCheck HealthCheck) HealthCheck {
	if requestCheck != nil {
		return requestCheck
	}
	return defaultCheck
}
