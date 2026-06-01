package engine

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sony/gobreaker"
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
	BaseURLs         []string
	SingleURL        string
	DefaultHeaders   Headers
	Timeout          time.Duration
	HTTPTransport    *http.Transport
	CircuitBreaker   *gobreaker.Settings
	Hooks            Hooks
	HealthCheck      HealthCheck
	MaxResponseBytes int64
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
	balancer           *roundRobin
	circuit            *circuit
	defaultHeader      http.Header
	hooks              Hooks
	defaultHealthCheck HealthCheck
	maxResponseBytes   int64
	baseURL            string
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
	return newExecutorFrom(opts, urls, ""), nil
}

// NewSimpleExecutor builds an executor for a one-off call.
func NewSimpleExecutor(opts Options) (*Executor, error) {
	opts = opts.withDefaults()
	if opts.SingleURL == "" {
		return nil, ErrEmptyURL
	}
	return newExecutorFrom(opts, nil, strings.TrimRight(opts.SingleURL, "/")), nil
}

func newExecutorFrom(opts Options, urls []string, singleURL string) *Executor {
	e := &Executor{
		httpClient:         &http.Client{Transport: opts.HTTPTransport, Timeout: opts.Timeout},
		defaultHeader:      opts.DefaultHeaders.HTTPHeader(),
		hooks:              opts.Hooks,
		defaultHealthCheck: opts.HealthCheck,
		maxResponseBytes:   opts.MaxResponseBytes,
		baseURL:            singleURL,
	}
	if len(urls) > 0 {
		e.balancer = newRoundRobin(urls)
	}
	var onState func(string, gobreaker.State, gobreaker.State)
	if opts.Hooks.OnCircuitBreaker != nil {
		onState = func(name string, from, to gobreaker.State) {
			e.emitCircuitState(name, from, to)
		}
	}
	e.circuit = newCircuit(opts.CircuitBreaker, onState)
	return e
}

// Call describes a single HTTP request.
type Call struct {
	Method      string
	Path        string
	Body        []byte
	Headers     Headers
	Query       map[string][]string
	HealthCheck HealthCheck
}

func (e *Executor) Do(ctx context.Context, call Call) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	target, err := e.resolveURL(call.Path, call.Query)
	if err != nil {
		return nil, err
	}

	resp, err := e.circuit.do(func() (*Response, error) {
		return e.doOnce(ctx, call, target)
	})
	if err != nil {
		e.emitHooks(ctx, call, target, err)
		return resp, err
	}

	if err := e.validateHealth(ctx, call, target, resp); err != nil {
		return resp, err
	}
	return resp, nil
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

func (e *Executor) resolveURL(path string, query map[string][]string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return appendQuery(path, query)
	}

	base := e.baseURL
	if base == "" && e.balancer != nil {
		base = e.balancer.nextURL()
	}
	if base == "" {
		return "", ErrEmptyURL
	}

	if path == "" {
		path = "/"
	} else if path[0] != '/' {
		path = "/" + path
	}
	return appendQuery(base+path, query)
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
