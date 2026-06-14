package simple

import (
	"context"
	"net/http"
	"time"

	"github.com/overiss/go-httpc/v2/internal/engine"
	"github.com/sony/gobreaker"
)

// Params are per-call settings for the lightweight API.
type Params struct {
	URL              string
	Headers          engine.Headers
	Query            engine.Query
	Timeout          time.Duration
	Transport        *http.Transport
	CircuitBreaker   *gobreaker.Settings
	HealthCheck      engine.HealthCheck
	MaxResponseBytes int64
}

func (p Params) engineOptions() engine.Options {
	return engine.Options{
		SingleURL:        p.URL,
		DefaultHeaders:   p.Headers,
		Timeout:          p.Timeout,
		HTTPTransport:    p.Transport,
		CircuitBreaker:   p.CircuitBreaker,
		HealthCheck:      p.HealthCheck,
		MaxResponseBytes: p.MaxResponseBytes,
	}
}

func (p Params) call(method string, body []byte) engine.Call {
	return engine.Call{
		Method:      method,
		Path:        p.URL,
		Body:        body,
		Headers:     p.Headers,
		Query:       engine.CloneQuery(p.Query),
		HealthCheck: p.HealthCheck,
	}
}

func do(ctx context.Context, method string, body []byte, p Params) (*engine.Response, error) {
	exec, err := engine.NewSimpleExecutor(p.engineOptions())
	if err != nil {
		return nil, err
	}
	return exec.Do(ctx, p.call(method, body))
}

func Get(ctx context.Context, p Params) (*engine.Response, error) {
	return do(ctx, http.MethodGet, nil, p)
}

func Post(ctx context.Context, p Params, body []byte) (*engine.Response, error) {
	return do(ctx, http.MethodPost, body, p)
}

func Put(ctx context.Context, p Params, body []byte) (*engine.Response, error) {
	return do(ctx, http.MethodPut, body, p)
}

func Patch(ctx context.Context, p Params, body []byte) (*engine.Response, error) {
	return do(ctx, http.MethodPatch, body, p)
}

func Delete(ctx context.Context, p Params) (*engine.Response, error) {
	return do(ctx, http.MethodDelete, nil, p)
}
