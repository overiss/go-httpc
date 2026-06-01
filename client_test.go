package httpc_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/overiss/go-httpc"
	"github.com/sony/gobreaker"
)

func TestClient_GetWithDefaultsAndPerRequestHeaders(t *testing.T) {
	t.Parallel()

	var gotAuth, gotTrace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTrace = r.Header.Get("X-Trace")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{
		BaseURLs: []string{srv.URL},
		DefaultHeaders: httpc.Headers{
			"Authorization": "Bearer default",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Get(context.Background(), "/v1/items", httpc.RequestParams{
		Headers: httpc.Headers{"X-Trace": "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer default" {
		t.Fatalf("default header: got %q", gotAuth)
	}
	if gotTrace != "abc" {
		t.Fatalf("per-request header: got %q", gotTrace)
	}
}

func TestRequestParams_WithReqParams(t *testing.T) {
	t.Parallel()

	incoming, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/path?a=1&a=2&b=3", nil)
	incoming.Header.Set("X-Trace", "trace-1")
	incoming.Header.Set("Authorization", "incoming")
	incoming.Header.Set("Connection", "keep-alive")

	var params httpc.RequestParams
	params.Headers = httpc.Headers{"Authorization": "override"}
	params.WithReqParams(incoming, httpc.ReqCopyOptions{
		CopyHeaders:  true,
		CopyQuery:    true,
		HeaderKeys:   []string{"X-Trace"},
		QueryKeys:    []string{"a"},
		SkipHopByHop: true,
	})

	if params.Headers["X-Trace"] != "trace-1" {
		t.Fatalf("trace header: got %q", params.Headers["X-Trace"])
	}
	if params.Headers["Authorization"] != "override" {
		t.Fatalf("explicit header must win: got %q", params.Headers["Authorization"])
	}
	if _, ok := params.Headers["Connection"]; ok {
		t.Fatal("hop-by-hop header must not be copied when using HeaderKeys")
	}
	if len(params.Query["a"]) != 2 || params.Query["a"][0] != "1" {
		t.Fatalf("query a: %#v", params.Query["a"])
	}
	if _, ok := params.Query["b"]; ok {
		t.Fatal("query key b must not be copied")
	}
}

func TestClient_WithReqParamsOnOutbound(t *testing.T) {
	t.Parallel()

	var gotTrace, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = r.Header.Get("X-Trace")
		gotLimit = r.URL.Query().Get("limit")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{BaseURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}

	incoming, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://gateway/local?limit=10&other=1", nil)
	incoming.Header.Set("X-Trace", "from-gateway")

	var params httpc.RequestParams
	params.WithReqParams(incoming, httpc.ReqCopyOptions{
		CopyHeaders: true,
		HeaderKeys:  []string{"X-Trace"},
		CopyQuery:   true,
		QueryKeys:   []string{"limit"},
	})

	_, err = c.Get(context.Background(), "/v1/items", params)
	if err != nil {
		t.Fatal(err)
	}
	if gotTrace != "from-gateway" || gotLimit != "10" {
		t.Fatalf("trace=%q limit=%q", gotTrace, gotLimit)
	}
}

func TestClient_RoundRobin(t *testing.T) {
	t.Parallel()

	var a, b atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		a.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srvA.Close)

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		b.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srvB.Close)

	c, err := httpc.New(httpc.Config{BaseURLs: []string{srvA.URL, srvB.URL}})
	if err != nil {
		t.Fatal(err)
	}

	for range 4 {
		if _, err := c.Get(context.Background(), "/", httpc.RequestParams{}); err != nil {
			t.Fatal(err)
		}
	}
	if a.Load() != 2 || b.Load() != 2 {
		t.Fatalf("round-robin counts: a=%d b=%d", a.Load(), b.Load())
	}
}

func TestClient_ContextCancel(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	c, err := httpc.New(httpc.Config{BaseURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = c.Get(ctx, "/", httpc.RequestParams{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestClient_MethodsWithBody(t *testing.T) {
	t.Parallel()

	var method string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{BaseURLs: []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"x":1}`)
	_, err = c.Post(context.Background(), "/x", payload, httpc.RequestParams{})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || string(body) != string(payload) {
		t.Fatalf("post: method=%s body=%q", method, body)
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)

	st := gobreaker.Settings{
		Name:        "test",
		MaxRequests: 1,
		Interval:    time.Second,
		Timeout:     time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
	}

	c, err := httpc.New(httpc.Config{
		BaseURLs:       []string{srv.URL},
		CircuitBreaker: &st,
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		_, _ = c.Get(context.Background(), "/", httpc.RequestParams{})
	}

	_, err = c.Get(context.Background(), "/", httpc.RequestParams{})
	if !errors.Is(err, httpc.ErrCircuitOpen) {
		t.Fatalf("expected circuit open, got %v", err)
	}
}

func TestSimpleGet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	t.Cleanup(srv.Close)

	resp, err := httpc.SimpleGet(context.Background(), httpc.SimpleParams{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK() || string(resp.Body) != "pong" {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, resp.Body)
	}
}

func TestSimpleGet_WithQuery(t *testing.T) {
	t.Parallel()

	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	_, err := httpc.SimpleGet(context.Background(), httpc.SimpleParams{
		URL:   srv.URL,
		Query: httpc.Query{"limit": {"5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("limit") != "5" {
		t.Fatalf("limit=%q", q.Get("limit"))
	}
}

func TestHooks_OnRequestError(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var got httpc.RequestErrorEvent

	c, err := httpc.New(httpc.Config{
		BaseURLs: []string{"http://127.0.0.1:1"}, // nothing listening
		Hooks: httpc.Hooks{
			OnRequestError: func(_ context.Context, e httpc.RequestErrorEvent) {
				mu.Lock()
				got = e
				mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = c.Get(context.Background(), "/x", httpc.RequestParams{})

	mu.Lock()
	defer mu.Unlock()
	if got.Err == nil {
		t.Fatal("expected request error hook")
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method=%s", got.Method)
	}
}

func TestHooks_OnTimeout_client(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var got httpc.TimeoutEvent

	c, err := httpc.New(httpc.Config{
		BaseURLs: []string{srv.URL},
		Timeout:  20 * time.Millisecond,
		Hooks: httpc.Hooks{
			OnTimeout: func(_ context.Context, e httpc.TimeoutEvent) {
				mu.Lock()
				got = e
				mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = c.Get(context.Background(), "/", httpc.RequestParams{})

	mu.Lock()
	defer mu.Unlock()
	if got.Err == nil || got.Source != "client" {
		t.Fatalf("timeout hook: source=%q err=%v", got.Source, got.Err)
	}
}

func TestHooks_OnTimeout_context(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	var mu sync.Mutex
	var got httpc.TimeoutEvent

	c, err := httpc.New(httpc.Config{
		BaseURLs: []string{srv.URL},
		Hooks: httpc.Hooks{
			OnTimeout: func(_ context.Context, e httpc.TimeoutEvent) {
				mu.Lock()
				got = e
				mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	_, _ = c.Get(ctx2, "/", httpc.RequestParams{})

	mu.Lock()
	defer mu.Unlock()
	if got.Source != "context" {
		t.Fatalf("source=%q err=%v", got.Source, got.Err)
	}
}

func TestHooks_OnCircuitBreaker_rejected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var rejections int

	st := gobreaker.Settings{
		Name:        "hooks-test",
		MaxRequests: 1,
		Interval:    time.Second,
		Timeout:     time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 2
		},
	}

	c, err := httpc.New(httpc.Config{
		BaseURLs:       []string{srv.URL},
		CircuitBreaker: &st,
		Hooks: httpc.Hooks{
			OnCircuitBreaker: func(e httpc.CircuitBreakerEvent) {
				if e.Rejected {
					mu.Lock()
					rejections++
					mu.Unlock()
				}
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 4 {
		_, _ = c.Get(context.Background(), "/", httpc.RequestParams{})
	}

	mu.Lock()
	defer mu.Unlock()
	if rejections == 0 {
		t.Fatal("expected circuit breaker rejection hook")
	}
}

func TestHealthCheckOK200(t *testing.T) {
	t.Parallel()
	if !httpc.HealthCheckOK200(&httpc.Response{StatusCode: 200}) {
		t.Fatal("expected 200 to pass")
	}
	if httpc.HealthCheckOK200(&httpc.Response{StatusCode: 201}) {
		t.Fatal("expected 201 to fail")
	}
}

func TestHealthCheckOK2xx(t *testing.T) {
	t.Parallel()
	if !httpc.HealthCheckOK2xx(&httpc.Response{StatusCode: 204}) {
		t.Fatal("expected 204 to pass")
	}
	if httpc.HealthCheckOK2xx(&httpc.Response{StatusCode: 404}) {
		t.Fatal("expected 404 to fail")
	}
}

func TestClient_HealthCheck_default(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{
		BaseURLs:    []string{srv.URL},
		HealthCheck: httpc.HealthCheckOK2xx,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.Get(context.Background(), "/", httpc.RequestParams{})
	if !errors.Is(err, httpc.ErrHealthCheck) {
		t.Fatalf("expected ErrHealthCheck, got %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusTeapot {
		t.Fatalf("response=%v", resp)
	}
}

func TestClient_HealthCheck_perRequestOverride(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{
		BaseURLs:    []string{srv.URL},
		HealthCheck: httpc.HealthCheckOK200,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.Get(context.Background(), "/", httpc.RequestParams{
		HealthCheck: httpc.HealthCheckOK2xx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestHooks_OnHealthCheckFailed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	var got httpc.HealthCheckEvent

	c, err := httpc.New(httpc.Config{
		BaseURLs:    []string{srv.URL},
		HealthCheck: httpc.HealthCheckOK200,
		Hooks: httpc.Hooks{
			OnHealthCheckFailed: func(_ context.Context, e httpc.HealthCheckEvent) {
				mu.Lock()
				got = e
				mu.Unlock()
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = c.Get(context.Background(), "/health", httpc.RequestParams{})

	mu.Lock()
	defer mu.Unlock()
	if got.Response == nil || got.Response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("hook event: %+v", got)
	}
	if got.URL == "" || got.Method != http.MethodGet {
		t.Fatalf("request meta: method=%s url=%s", got.Method, got.URL)
	}
}

func TestClient_MaxResponseBytes(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := httpc.New(httpc.Config{
		BaseURLs:         []string{srv.URL},
		MaxResponseBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Get(context.Background(), "/", httpc.RequestParams{})
	if err == nil {
		t.Fatal("expected body limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthCheckDoesNotTripCircuitBreaker(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	st := &gobreaker.Settings{
		Name:        "health-no-trip",
		MaxRequests: 1,
		Interval:    time.Second,
		Timeout:     time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 2
		},
	}
	c, err := httpc.New(httpc.Config{
		BaseURLs:       []string{srv.URL},
		HealthCheck:    httpc.HealthCheckOK2xx,
		CircuitBreaker: st,
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 5 {
		_, _ = c.Get(context.Background(), "/", httpc.RequestParams{})
	}

	_, err = c.Get(context.Background(), "/", httpc.RequestParams{})
	if errors.Is(err, httpc.ErrCircuitOpen) {
		t.Fatal("health check failure must not open circuit breaker")
	}
}
