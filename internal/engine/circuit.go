package engine

import (
	"errors"

	"github.com/sony/gobreaker"
)

type circuit struct {
	name    string
	breaker *gobreaker.CircuitBreaker
}

func newCircuit(settings *gobreaker.Settings, onStateChange func(name string, from, to gobreaker.State)) *circuit {
	if settings == nil {
		return nil
	}
	s := *settings
	if s.Name == "" {
		s.Name = "httpc"
	}
	userOnState := s.OnStateChange
	s.OnStateChange = func(name string, from, to gobreaker.State) {
		if userOnState != nil {
			userOnState(name, from, to)
		}
		if onStateChange != nil {
			onStateChange(name, from, to)
		}
	}
	return &circuit{name: s.Name, breaker: gobreaker.NewCircuitBreaker(s)}
}

func (c *circuit) isOpen() bool {
	if c == nil || c.breaker == nil {
		return false
	}
	return c.breaker.State() == gobreaker.StateOpen
}

func (c *circuit) do(fn func() (*Response, error)) (*Response, error) {
	if c == nil || c.breaker == nil {
		return fn()
	}
	v, err := c.breaker.Execute(func() (any, error) {
		return fn()
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, ErrCircuitOpen
		}
		return nil, err
	}
	resp, ok := v.(*Response)
	if !ok {
		return nil, ErrUnexpectedResponse
	}
	return resp, nil
}
