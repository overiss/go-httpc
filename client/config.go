package client

import (
	"net/http"
	"time"

	"github.com/overiss/go-httpc/internal/engine"
	"github.com/sony/gobreaker"
)

// Config holds settings for a long-lived Client.
// Only BaseURLs is required; all other fields are optional (zero value = disabled / default).
type Config struct {
	// BaseURLs is the list of upstream hosts for round-robin load balancing.
	BaseURLs []string

	// DefaultHeaders are merged into every request (per-request headers override).
	DefaultHeaders engine.Headers

	// Timeout applies to the entire request. Zero = no client timeout.
	Timeout time.Duration

	// Transport is the underlying http.Transport. Nil = tuned default.
	Transport *http.Transport

	// CircuitBreaker enables per-host circuit breaking. Nil = disabled.
	CircuitBreaker *gobreaker.Settings

	// Hooks are optional observability callbacks.
	Hooks engine.Hooks

	// HealthCheck is the default response validator. Nil = disabled.
	HealthCheck engine.HealthCheck

	// MaxResponseBytes limits the response body. Zero = 16 MiB; negative = unlimited.
	MaxResponseBytes int64

	// MaxConcurrentRequests limits in-flight requests for this client instance.
	// Zero or negative = unlimited.
	MaxConcurrentRequests int

	// OAuth2 configures automatic access-token authorization (oauth2.Transport).
	// Nil = disabled.
	OAuth2 OAuth2Config
}

func (c Config) engineOptions() engine.Options {
	return engine.Options{
		BaseURLs:              c.BaseURLs,
		DefaultHeaders:        c.DefaultHeaders,
		Timeout:               c.Timeout,
		HTTPTransport:         c.Transport,
		CircuitBreaker:        c.CircuitBreaker,
		Hooks:                 c.Hooks,
		HealthCheck:           c.HealthCheck,
		MaxResponseBytes:      c.MaxResponseBytes,
		MaxConcurrentRequests: c.MaxConcurrentRequests,
	}
}
