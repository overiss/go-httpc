// Package httpc provides a high-performance HTTP client with load balancing,
// circuit breaking, health checks, and optional observability hooks.
package httpc

import (
	"github.com/overiss/go-httpc/client"
	"github.com/overiss/go-httpc/internal/engine"
	"github.com/overiss/go-httpc/simple"
)

type (
	// Client is a configured, reusable HTTP client.
	Client = client.Client
	// Config holds settings for Client.
	Config = client.Config
	// RequestParams are per-request settings.
	RequestParams = client.RequestParams
	// ReqCopyOptions controls copying from *http.Request.
	ReqCopyOptions = client.ReqCopyOptions
	// OAuth2Config builds a token source for automatic authorization.
	OAuth2Config = client.OAuth2Config
)

// New creates a Client from Config.
var New = client.New

// HealthCheckOK200 reports whether the status code is exactly 200.
func HealthCheckOK200(resp *Response) bool { return client.HealthCheckOK200(resp) }

// HealthCheckOK2xx reports whether the status code is in the 2xx range.
func HealthCheckOK2xx(resp *Response) bool { return client.HealthCheckOK2xx(resp) }

// SimpleParams are per-call settings for the lightweight API.
type SimpleParams = simple.Params

// SimpleGet performs a one-off GET.
var SimpleGet = simple.Get

// SimplePost performs a one-off POST.
var SimplePost = simple.Post

// SimplePut performs a one-off PUT.
var SimplePut = simple.Put

// SimplePatch performs a one-off PATCH.
var SimplePatch = simple.Patch

// SimpleDelete performs a one-off DELETE.
var SimpleDelete = simple.Delete

type (
	// Headers is a single-value-per-key header map.
	Headers = engine.Headers
	// Query holds URL query parameters.
	Query = engine.Query
	// Response is the result of a completed HTTP call.
	Response = engine.Response
	// HealthCheck validates a completed response.
	HealthCheck = engine.HealthCheck
	// Hooks are optional client callbacks.
	Hooks = engine.Hooks
	// RequestEvent contains metadata shared by hook events.
	RequestEvent = engine.RequestEvent
	// TimeoutEvent is passed to Hooks.OnTimeout.
	TimeoutEvent = engine.TimeoutEvent
	// RequestErrorEvent is passed to Hooks.OnRequestError.
	RequestErrorEvent = engine.RequestErrorEvent
	// HealthCheckEvent is passed to Hooks.OnHealthCheckFailed.
	HealthCheckEvent = engine.HealthCheckEvent
	// CircuitBreakerEvent is passed to Hooks.OnCircuitBreaker.
	CircuitBreakerEvent = engine.CircuitBreakerEvent
	// RequestCompletedEvent is passed to Hooks.OnRequestCompleted.
	RequestCompletedEvent = engine.RequestCompletedEvent
	// RequestCompletedHook is the signature for OnRequestCompleted and CompleteHook.
	RequestCompletedHook = engine.RequestCompletedHook
)

var (
	// ErrCircuitOpen is returned when the circuit breaker rejects a call.
	ErrCircuitOpen = engine.ErrCircuitOpen
	// ErrNoBaseURLs is returned when Client has no upstream hosts configured.
	ErrNoBaseURLs = engine.ErrNoBaseURLs
	// ErrEmptyURL is returned when a simple call has no target URL.
	ErrEmptyURL = engine.ErrEmptyURL
	// ErrHealthCheck is returned when HealthCheck rejects a response.
	ErrHealthCheck = engine.ErrHealthCheck
	// ErrUnexpectedResponse indicates an internal circuit-breaker type mismatch.
	ErrUnexpectedResponse = engine.ErrUnexpectedResponse
)
