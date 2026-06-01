package engine

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"

	"github.com/sony/gobreaker"
)

// Hooks are optional callbacks for the configured Client.
type Hooks struct {
	OnTimeout           func(ctx context.Context, e TimeoutEvent)
	OnRequestError      func(ctx context.Context, e RequestErrorEvent)
	OnCircuitBreaker    func(e CircuitBreakerEvent)
	OnHealthCheckFailed func(ctx context.Context, e HealthCheckEvent)
}

type RequestEvent struct {
	Method string
	URL    string
}

type TimeoutEvent struct {
	RequestEvent
	Source string
	Err    error
}

type RequestErrorEvent struct {
	RequestEvent
	Err error
}

type HealthCheckEvent struct {
	RequestEvent
	Response *Response
}

type CircuitBreakerEvent struct {
	Name     string
	Rejected bool
	Err      error
	From     gobreaker.State
	To       gobreaker.State
}

func (e *Executor) emitHealthCheckFailed(ctx context.Context, p Call, target string, resp *Response) {
	if e.hooks.OnHealthCheckFailed == nil {
		return
	}
	e.hooks.OnHealthCheckFailed(ctx, HealthCheckEvent{
		RequestEvent: RequestEvent{Method: p.Method, URL: target},
		Response:     resp,
	})
}

func (e *Executor) emitHooks(ctx context.Context, p Call, target string, err error) {
	if err == nil {
		return
	}

	ev := RequestEvent{Method: p.Method, URL: target}

	if errors.Is(err, ErrCircuitOpen) {
		if e.hooks.OnCircuitBreaker != nil {
			e.hooks.OnCircuitBreaker(CircuitBreakerEvent{
				Rejected: true,
				Err:      err,
			})
		}
		return
	}

	if src, ok := timeoutSource(ctx, err); ok {
		if e.hooks.OnTimeout != nil {
			e.hooks.OnTimeout(ctx, TimeoutEvent{
				RequestEvent: ev,
				Source:       src,
				Err:          err,
			})
		}
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if e.hooks.OnRequestError != nil {
		e.hooks.OnRequestError(ctx, RequestErrorEvent{
			RequestEvent: ev,
			Err:          err,
		})
	}
}

func (e *Executor) emitCircuitState(name string, from, to gobreaker.State) {
	if e.hooks.OnCircuitBreaker == nil {
		return
	}
	e.hooks.OnCircuitBreaker(CircuitBreakerEvent{
		Name: name,
		From: from,
		To:   to,
	})
}

func timeoutSource(ctx context.Context, err error) (source string, ok bool) {
	if err == nil {
		return "", false
	}
	if strings.Contains(err.Error(), "Client.Timeout exceeded") {
		return "client", true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if ctx != nil {
			if _, has := ctx.Deadline(); has && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "context", true
			}
		}
		return "client", true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "client", true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		if ctx != nil {
			if _, has := ctx.Deadline(); has && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "context", true
			}
		}
		return "client", true
	}
	return "", false
}
