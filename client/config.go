package client

import (
	"net/http"
	"time"

	"github.com/overiss/go-httpc/internal/engine"
	"github.com/sony/gobreaker"
)

// Config holds settings for a long-lived Client.
type Config struct {
	BaseURLs         []string
	DefaultHeaders   engine.Headers
	Timeout          time.Duration
	Transport        *http.Transport
	CircuitBreaker   *gobreaker.Settings
	Hooks            engine.Hooks
	HealthCheck      engine.HealthCheck
	MaxResponseBytes int64
}

func (c Config) engineOptions() engine.Options {
	return engine.Options{
		BaseURLs:         c.BaseURLs,
		DefaultHeaders:   c.DefaultHeaders,
		Timeout:          c.Timeout,
		HTTPTransport:    c.Transport,
		CircuitBreaker:   c.CircuitBreaker,
		Hooks:            c.Hooks,
		HealthCheck:      c.HealthCheck,
		MaxResponseBytes: c.MaxResponseBytes,
	}
}
