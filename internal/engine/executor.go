package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sony/gobreaker"
	"golang.org/x/oauth2"
)

var (
	cachedHTTPTransport     *http.Transport
	cachedHTTPTransportOnce sync.Once
)

func defaultHTTPTransport() *http.Transport {
	cachedHTTPTransportOnce.Do(func() {
		cachedHTTPTransport = &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 64,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		}
	})
	return cachedHTTPTransport
}

// Options configure the shared HTTP executor.
type Options struct {
	BaseURLs              []string
	SingleURL             string
	DefaultHeaders        Headers
	Timeout               time.Duration
	HTTPTransport         *http.Transport
	CircuitBreaker        *gobreaker.Settings
	Hooks                 Hooks
	HealthCheck           HealthCheck
	MaxResponseBytes      int64
	MaxConcurrentRequests int
	OAuth2TokenSource     oauth2.TokenSource
}

func (o Options) withDefaults() Options {
	cfg := o
	if cfg.HTTPTransport == nil {
		cfg.HTTPTransport = defaultHTTPTransport()
	}
	if cfg.DefaultHeaders == nil {
		cfg.DefaultHeaders = make(Headers)
	}
	return cfg
}

// Executor runs HTTP requests (used by client and simple packages).
type Executor struct {
	httpClient         *http.Client
	pool               *loadBalancer
	defaultHeader      http.Header
	hooks              Hooks
	defaultHealthCheck HealthCheck
	maxResponseBytes   int64
	concurrency        *concurrencyLimiter
}

// NewExecutor builds an executor for a long-lived client.
func NewExecutor(opts Options) (*Executor, error) {
	opts = opts.withDefaults()
	if len(opts.BaseURLs) == 0 {
		return nil, ErrNoBaseURLs
	}
	urls := make([]string, len(opts.BaseURLs))
	for i, u := range opts.BaseURLs {
		urls[i] = strings.TrimRight(u, "/")
	}
	e := newExecutorFrom(opts, urls)
	e.concurrency = newConcurrencyLimiter(opts.MaxConcurrentRequests)
	return e, nil
}

// NewSimpleExecutor builds an executor for a one-off call.
func NewSimpleExecutor(opts Options) (*Executor, error) {
	opts = opts.withDefaults()
	if opts.SingleURL == "" {
		return nil, ErrEmptyURL
	}
	return newExecutorFrom(opts, []string{strings.TrimRight(opts.SingleURL, "/")}), nil
}

func newExecutorFrom(opts Options, urls []string) *Executor {
	transport := wrapOAuth2Transport(opts.HTTPTransport, opts.OAuth2TokenSource)
	e := &Executor{
		httpClient:         &http.Client{Transport: transport, Timeout: opts.Timeout},
		defaultHeader:      opts.DefaultHeaders.HTTPHeader(),
		hooks:              opts.Hooks,
		defaultHealthCheck: opts.HealthCheck,
		maxResponseBytes:   opts.MaxResponseBytes,
	}
	var onState func(string, gobreaker.State, gobreaker.State)
	if opts.Hooks.OnCircuitBreaker != nil {
		onState = func(name string, from, to gobreaker.State) {
			e.emitCircuitState(name, from, to)
		}
	}
	e.pool = newLoadBalancer(urls, opts.CircuitBreaker, onState)
	return e
}

// Call describes a single HTTP request.
type Call struct {
	Method       string
	Path         string
	Body         []byte
	Headers      Headers
	Query        map[string][]string
	HealthCheck  HealthCheck
	CompleteHook RequestCompletedHook
}

func (e *Executor) Do(ctx context.Context, call Call) (resp *Response, err error) {
	var target string
	defer func() {
		e.emitRequestCompleted(ctx, call, target, resp, err)
	}()

	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = e.concurrency.acquire(ctx); err != nil {
		return nil, err
	}
	defer e.concurrency.release()

	if e.pool == nil || e.pool.hostCount() == 0 {
		return nil, ErrEmptyURL
	}

	if strings.HasPrefix(call.Path, "http://") || strings.HasPrefix(call.Path, "https://") {
		target, err = appendQuery(call.Path, call.Query)
		if err != nil {
			return nil, err
		}
		resp, err = e.doTarget(ctx, call, target)
		return resp, err
	}

	if e.pool.hostCount() == 1 {
		var base string
		base, ok := e.pool.nextHealthy()
		if !ok {
			e.emitHooks(ctx, call, "", ErrCircuitOpen)
			return nil, ErrCircuitOpen
		}
		target, err = e.buildTarget(base, call.Path, call.Query)
		if err != nil {
			return nil, err
		}
		resp, err = e.doTarget(ctx, call, target)
		return resp, err
	}

	resp, target, err = e.doWithFailover(ctx, call)
	return resp, err
}

// doWithFailover tries healthy upstreams in round-robin order.
func (e *Executor) doWithFailover(ctx context.Context, call Call) (*Response, string, error) {
	attempts := e.pool.hostCount()
	var lastErr error
	var lastTarget string

	for range attempts {
		base, ok := e.pool.nextHealthy()
		if !ok {
			if lastErr != nil {
				return nil, lastTarget, lastErr
			}
			e.emitHooks(ctx, call, "", ErrCircuitOpen)
			return nil, "", ErrCircuitOpen
		}

		target, err := e.buildTarget(base, call.Path, call.Query)
		if err != nil {
			return nil, "", err
		}
		lastTarget = target

		resp, err := e.doThroughHost(ctx, call, base, target)
		if err == nil {
			if err := e.validateHealth(ctx, call, target, resp); err != nil {
				return resp, target, err
			}
			return resp, target, nil
		}

		lastErr = err
		if errors.Is(err, ErrCircuitOpen) {
			continue
		}
	}

	if lastErr != nil {
		e.emitHooks(ctx, call, lastTarget, lastErr)
	}
	return nil, lastTarget, lastErr
}

func (e *Executor) doTarget(ctx context.Context, call Call, target string) (*Response, error) {
	base := hostFromTarget(target)
	if base == "" {
		return e.doOnce(ctx, call, target)
	}

	resp, err := e.doThroughHost(ctx, call, base, target)
	if err != nil {
		e.emitHooks(ctx, call, target, err)
		return resp, err
	}
	if err := e.validateHealth(ctx, call, target, resp); err != nil {
		return resp, err
	}
	return resp, nil
}

func (e *Executor) doThroughHost(ctx context.Context, call Call, base, target string) (*Response, error) {
	c := e.pool.circuitFor(base)
	if c == nil {
		return e.doOnce(ctx, call, target)
	}
	return c.do(func() (*Response, error) {
		return e.doOnce(ctx, call, target)
	})
}

func (e *Executor) doOnce(ctx context.Context, call Call, target string) (*Response, error) {
	var body io.Reader
	if len(call.Body) > 0 {
		body = bytes.NewReader(call.Body)
	}

	req, err := http.NewRequestWithContext(ctx, call.Method, target, body)
	if err != nil {
		return nil, err
	}

	merged := MergeHeaders(e.defaultHeader, call.Headers)
	if len(merged) > 0 {
		req.Header = merged
	}
	if len(call.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := readResponseBody(resp.Body, e.maxResponseBytes)
	if err != nil {
		return nil, err
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       data,
	}, nil
}

func (e *Executor) validateHealth(ctx context.Context, call Call, target string, resp *Response) error {
	check := ResolveHealthCheck(e.defaultHealthCheck, call.HealthCheck)
	if check == nil || check(resp) {
		return nil
	}
	e.emitHealthCheckFailed(ctx, call, target, resp)
	return ErrHealthCheck
}

func (e *Executor) buildTarget(base, path string, query map[string][]string) (string, error) {
	if path == "" {
		path = "/"
	} else if path[0] != '/' {
		path = "/" + path
	}
	return appendQuery(base+path, query)
}

func hostFromTarget(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/")
}

func appendQuery(raw string, query map[string][]string) (string, error) {
	if len(query) == 0 {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, vv := range query {
		for _, v := range vv {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
