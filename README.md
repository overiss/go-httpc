# go-httpc

High-performance HTTP client for Go: load balancing, per-host circuit breaking, optional health checks, concurrency limits, and hooks.

Import path: `github.com/overiss/go-httpc`  
Requires **Go 1.26+**.

## Table of contents

- [When to use what](#when-to-use-what)
- [Install](#install)
- [Quick start](#quick-start)
- [Configured Client](#configured-client)
  - [Config reference](#config-reference)
  - [HTTP methods](#http-methods)
  - [RequestParams](#requestparams)
  - [Propagating gateway request data](#propagating-gateway-request-data)
- [Simple API](#simple-api)
  - [SimpleParams reference](#simpleparams-reference)
- [Response](#response)
- [Context and cancellation](#context-and-cancellation)
- [Load balancing and failover](#load-balancing-and-failover)
- [Circuit breaker](#circuit-breaker)
- [Concurrency limit](#concurrency-limit)
- [Health checks](#health-checks)
- [OAuth2](#oauth2)
- [Hooks](#hooks)
- [Errors](#errors)
- [Performance and tuning](#performance-and-tuning)
- [FAQ](#faq)
- [Development](#development)

---

## When to use what

| | **Client** (`httpc.New`) | **Simple** (`httpc.SimpleGet`, …) |
|---|--------------------------|-----------------------------------|
| Lifetime | Create once, reuse (DI, service struct) | One call = new internal `http.Client` |
| Base URL | `Config.BaseURLs` + relative path | Full URL in `SimpleParams.URL` |
| Load balancing | Yes (round-robin) | No |
| Per-host circuit breaker | Yes | Single URL only |
| Hooks | Yes | No |
| Concurrency limit | Yes (`MaxConcurrentRequests`) | No |
| Best for | Microservices, workers, hot paths | Scripts, tests, rare outbound calls |

**Rule of thumb:** production traffic → **Client**; a single webhook or CLI call → **Simple**.

---

## Install

```bash
go get github.com/overiss/go-httpc
```

```go
import httpc "github.com/overiss/go-httpc"
```

### Project layout (for contributors)

```
go-httpc/
  httpc.go              # public API
  client/               # Client, Config, RequestParams
  simple/               # Simple* functions
  internal/engine/      # executor, balancer, circuit breaker
```

---

## Quick start

**Minimal client** — only `BaseURLs` is required; everything else is optional:

```go
client, err := httpc.New(httpc.Config{
    BaseURLs: []string{"https://api.example.com"},
})
if err != nil {
    return err
}

resp, err := client.Get(ctx, "/v1/health", httpc.RequestParams{})
if err != nil {
    return err
}
if resp.OK() {
    fmt.Println(string(resp.Body))
}
```

**One-off call:**

```go
resp, err := httpc.SimpleGet(ctx, httpc.SimpleParams{
    URL: "https://api.example.com/v1/health",
})
```

---

## Configured Client

### Config reference

Only **`BaseURLs`** is required. Zero values mean “disabled” or “library default” — safe to omit fields.

| Field | Type | Default / zero | Description |
|-------|------|----------------|-------------|
| `BaseURLs` | `[]string` | **required** | Upstream origins, e.g. `https://api-1.example.com`. Trailing slashes are trimmed. |
| `DefaultHeaders` | `httpc.Headers` | none | Merged into every request. Per-request headers override on key conflict. |
| `Timeout` | `time.Duration` | no timeout | Total timeout for the client (dial + TLS + body read). |
| `Transport` | `*http.Transport` | tuned default | Custom transport; `nil` uses a shared pooled transport. |
| `CircuitBreaker` | `*gobreaker.Settings` | disabled | Per-host breaker; see [Circuit breaker](#circuit-breaker). |
| `Hooks` | `httpc.Hooks` | disabled | Callbacks; see [Hooks](#hooks). |
| `HealthCheck` | `httpc.HealthCheck` | disabled | Default response validator; see [Health checks](#health-checks). |
| `MaxResponseBytes` | `int64` | `16 MiB` | Max body size read into memory. `0` = 16 MiB. Negative = unlimited. |
| `MaxConcurrentRequests` | `int` | unlimited | Max in-flight requests for **this** client instance. `≤0` = unlimited. |
| `OAuth2` | `httpc.OAuth2Config` | disabled | Builds `oauth2.TokenSource`; wraps transport like `oauth2.NewClient`. |

```go
client, err := httpc.New(httpc.Config{
    BaseURLs: []string{
        "https://api-1.example.com",
        "https://api-2.example.com",
    },
    DefaultHeaders: httpc.Headers{
        "Authorization": "Bearer <token>",
    },
    Timeout:               5 * time.Second,
    MaxResponseBytes:      1 << 20, // 1 MiB
    MaxConcurrentRequests: 64,
    HealthCheck:           httpc.HealthCheckOK2xx,
    CircuitBreaker:        &gobreaker.Settings{ /* ... */ },
    Hooks:                 httpc.Hooks{ /* ... */ },
})
```

`New` returns `ErrNoBaseURLs` if `BaseURLs` is empty.

### HTTP methods

All methods take `context.Context`, a **relative path** (unless you use a full URL — see below), and `httpc.RequestParams`.

| Method | Body | Notes |
|--------|------|--------|
| `Get(ctx, path, params)` | — | |
| `Post(ctx, path, body, params)` | `[]byte` | Sets `Content-Type: application/octet-stream` if missing |
| `Put(ctx, path, body, params)` | `[]byte` | |
| `Patch(ctx, path, body, params)` | `[]byte` | |
| `Delete(ctx, path, params)` | — | |
| `Do(ctx, method, path, body, params)` | `[]byte` | Arbitrary HTTP method |

Path resolution:

- `"/v1/users"` + base `https://api-1.example.com` → `https://api-1.example.com/v1/users`
- `"v1/users"` → leading `/` added automatically
- `"https://other.service/absolute"` → used as-is (query from `RequestParams` is still merged)

### RequestParams

Per-request settings (all fields optional):

| Field | Type | Description |
|-------|------|-------------|
| `Headers` | `httpc.Headers` | Single value per key; overrides `DefaultHeaders` for the same key |
| `Query` | `httpc.Query` | Query string parameters (`map[string][]string`) |
| `HealthCheck` | `httpc.HealthCheck` | Overrides client default for this call only |

```go
resp, err := client.Get(ctx, "/v1/users", httpc.RequestParams{
    Headers: httpc.Headers{
        "X-Request-ID": "req-42",
    },
    Query: httpc.Query{
        "limit": {"10"},
        "tag":   {"go", "http"},
    },
})
```

### Propagating gateway request data

Copy headers/query from an incoming `*http.Request` (BFF → backend):

```go
var params httpc.RequestParams
params.WithReqParams(incoming, httpc.ReqCopyOptions{
    CopyHeaders:  true,
    HeaderKeys:   []string{"X-Request-ID", "X-Trace-ID", "Authorization"},
    CopyQuery:    true,
    QueryKeys:    []string{"limit", "cursor"},
    SkipHopByHop: true,
})

resp, err := client.Get(ctx, "/v1/users", params)
```

`ReqCopyOptions`:

| Field | Behavior |
|-------|----------|
| `CopyHeaders` | Copy headers from `r` |
| `CopyQuery` | Copy `r.URL` query |
| `HeaderKeys` | Allow-list; if empty and `CopyHeaders`, copy all (respect `SkipHopByHop`) |
| `QueryKeys` | Allow-list; if empty and `CopyQuery`, copy full query |
| `SkipHopByHop` | Skip `Connection`, `Transfer-Encoding`, `Upgrade`, etc. |

Values already present in `RequestParams.Headers` / `Query` are **not** overwritten by `WithReqParams`.

---

## Simple API

Functions: `SimpleGet`, `SimplePost`, `SimplePut`, `SimplePatch`, `SimpleDelete`.

Each call builds a short-lived executor (new `http.Client` internally). Do **not** use for high QPS — use `Client` instead.

```go
body := []byte(`{"ok":true}`)
resp, err := httpc.SimplePost(ctx, httpc.SimpleParams{
    URL:     "https://api.example.com/hook",
    Headers: httpc.Headers{"Content-Type": "application/json"},
    Timeout: 3 * time.Second,
}, body)
```

### SimpleParams reference

| Field | Default / zero | Description |
|-------|----------------|-------------|
| `URL` | **required** | Full URL including scheme and host |
| `Headers` | none | Request headers |
| `Query` | none | Appended to `URL` |
| `Timeout` | no timeout | Per-call client timeout |
| `Transport` | shared default | Optional custom transport |
| `CircuitBreaker` | disabled | Breaker for this single host |
| `HealthCheck` | disabled | Response validator |
| `MaxResponseBytes` | 16 MiB | Same semantics as `Config` |

No hooks, no load balancing, no `MaxConcurrentRequests`.

---

## Response

```go
type Response struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
}
```

- `resp.OK()` — `true` for status `200–299`
- Body is always buffered in memory (no streaming API)
- On `ErrHealthCheck`, **`resp` is still filled** (status, headers, body) so you can log or parse the upstream error payload

---

## Context and cancellation

Every call respects `context.Context`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

resp, err := client.Get(ctx, "/slow", httpc.RequestParams{})
```

- Deadline / cancel → request aborts (`context.Canceled` or `context.DeadlineExceeded`)
- Waiting for a **concurrency slot** also respects context cancel
- Context cancellation is **not** reported via hooks (by design)

---

## Load balancing and failover

With **two or more** `BaseURLs`:

1. Pick the next **healthy** host (round-robin, lock-free).
2. **Healthy** = per-host circuit is not **open** (see below).
3. Execute the request through that host’s breaker.
4. On transport failure or `ErrCircuitOpen` for that host, **retry the next healthy host in the same `Do` call** (failover).
5. If no healthy hosts remain → `ErrCircuitOpen`.

With **one** `BaseURL`, there is no balancing; breaker still applies to that host.

---

## Circuit breaker

Powered by [sony/gobreaker](https://github.com/sony/gobreaker).

- **One breaker per `BaseURL`** (breaker name = base URL string).
- **Open** host is **skipped** by the balancer.
- **Transport errors** (timeout, connection refused, reset, …) count as failures.
- **HTTP 5xx** is returned to the caller as a normal response — does **not** trip the breaker by itself.
- **Health check failure** (`ErrHealthCheck`) does **not** trip the breaker.

```go
import "github.com/sony/gobreaker"

client, err := httpc.New(httpc.Config{
    BaseURLs: []string{"https://a.example.com", "https://b.example.com"},
    CircuitBreaker: &gobreaker.Settings{
        Name:        "my-service", // overridden per host with host URL
        MaxRequests: 3,
        Interval:    10 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(c gobreaker.Counts) bool {
            return c.ConsecutiveFailures >= 5
        },
    },
})
```

When all hosts are open → `errors.Is(err, httpc.ErrCircuitOpen)`.

---

## Concurrency limit

Limits **simultaneous in-flight requests** for one `Client` instance (not RPS).

```go
client, err := httpc.New(httpc.Config{
    BaseURLs:              []string{"https://api.example.com"},
    MaxConcurrentRequests: 32,
})
```

- One slot is held for the entire `Get`/`Post`/… call (including failover attempts).
- `≤0` — unlimited.
- If the limit is reached, the call waits until a slot is free or **context is cancelled**.

Use this to protect your service from unbounded outbound parallelism.

---

## Health checks

Runs **after** a successful HTTP round-trip (body read), **outside** the circuit breaker’s failure counting.

Built-in:

```go
httpc.HealthCheckOK200  // status == 200
httpc.HealthCheckOK2xx  // 2xx (same as resp.OK())
```

Custom:

```go
func myCheck(resp *httpc.Response) bool {
    return resp.StatusCode == 200 && len(resp.Body) > 0
}

client, _ := httpc.New(httpc.Config{
    BaseURLs:    []string{"https://api.example.com"},
    HealthCheck: myCheck,
})
```

On failure → `errors.Is(err, httpc.ErrHealthCheck)` and non-nil `resp`.

---

## OAuth2

Optional automatic access-token authorization for the configured **Client** only.

`OAuth2` is a function that returns `oauth2.TokenSource` — the same value you would pass to `oauth2.NewClient(ctx, auth)`:

```go
type OAuth2Config func(ctx context.Context) (oauth2.TokenSource, error)
```

The function runs **once** during `httpc.New` (with `context.Background()`). The token source is reused for the client lifetime; token refresh is handled by `oauth2.Transport` (via `ReuseTokenSource`).

### Provider example (mock)

Same idea as `oauth2.NewClient(ctx, auth)` — plug in any type that implements `oauth2.TokenSource`:

```go
import (
    "context"
    "os"

    httpc "github.com/overiss/go-httpc"
    "golang.org/x/oauth2"
)

// myauth.NewTokenSource is your auth package (JWT, client credentials, etc.).
client, err := httpc.New(httpc.Config{
    BaseURLs: []string{"https://upstream.example.com"},
    OAuth2: func(ctx context.Context) (oauth2.TokenSource, error) {
        return myauth.NewTokenSource(ctx, myauth.Config{
            IssuerURL: os.Getenv("AUTH_ISSUER_URL"),
            ClientID:  os.Getenv("CLIENT_ID"),
            Secret:    os.Getenv("CLIENT_SECRET"),
        })
    },
})
if err != nil {
    return err
}

resp, err := client.Get(ctx, "/secure", httpc.RequestParams{})
```

Before each request `oauth2.Transport` calls `Token()` on the source, refreshes the access token when needed, and sets the `Authorization` header.

Minimal mock for tests:

```go
type staticTokenSource struct{ token string }

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
    return &oauth2.Token{AccessToken: s.token, TokenType: "Bearer"}, nil
}

OAuth2: func(_ context.Context) (oauth2.TokenSource, error) {
    return &staticTokenSource{token: "test-token"}, nil
},
```

### Notes

- Do **not** set `Authorization` in `DefaultHeaders` when using `OAuth2` — the transport manages it.
- Per-request `RequestParams.Headers` can still override `Authorization` if needed.
- OAuth2 wraps the same pooled `http.Transport` as a non-OAuth client (circuit breaker, timeouts, etc. still apply).
- Simple API does not support `OAuth2`.

---

## Hooks

**Client only.** Callbacks must be **fast and non-blocking** (offload to a channel/goroutine inside the hook if needed).

| Hook | When |
|------|------|
| `OnTimeout` | Transport/client timeout. `TimeoutEvent.Source`: `"context"` or `"client"`. |
| `OnRequestError` | Other errors (DNS, connection refused, …). Not cancel, not breaker. |
| `OnCircuitBreaker` | Breaker rejected call (`Rejected: true`) or state change (`From` / `To`). `Name` = host URL. |
| `OnHealthCheckFailed` | `HealthCheck` returned false. `Response` is set. |
| `OnRequestCompleted` | After every client call (once per `Get`/`Post`/…). See below. |

`OnRequestCompleted` receives full request/response data for logging:

```go
OnRequestCompleted: func(ctx context.Context, e httpc.RequestCompletedEvent) {
    log.Info().
        Str("method", e.Method).
        Str("url", e.URL).
        Int("status", e.StatusCode).
        Err(e.Err).
        Msg("outbound http")
    // e.RequestBody, e.ResponseBody — copy if logging asynchronously
},
```

| Field | Description |
|-------|-------------|
| `Method` | HTTP method |
| `URL` | Full resolved URL (with query). Empty if URL was not built. |
| `RequestBody` | Request payload (`nil` for GET/DELETE without body) |
| `StatusCode` | HTTP status; `0` if no response (network/breaker error) |
| `ResponseBody` | Response payload; `nil` if no response |
| `Err` | Final error (`nil` on success, including 2xx/4xx/5xx without health check failure) |

Called on success, transport errors, circuit breaker, and health-check failures. Not called for Simple API.

```go
Hooks: httpc.Hooks{
    OnTimeout: func(ctx context.Context, e httpc.TimeoutEvent) {
        log.Printf("timeout %s %s: %v", e.Method, e.URL, e.Err)
    },
    OnRequestError: func(ctx context.Context, e httpc.RequestErrorEvent) {
        log.Printf("error %s %s: %v", e.Method, e.URL, e.Err)
    },
    OnCircuitBreaker: func(e httpc.CircuitBreakerEvent) {
        if e.Rejected {
            log.Printf("breaker open: %s", e.Name)
        }
    },
    OnHealthCheckFailed: func(ctx context.Context, e httpc.HealthCheckEvent) {
        log.Printf("unhealthy %s: %d", e.URL, e.Response.StatusCode)
    },
    OnRequestCompleted: func(ctx context.Context, e httpc.RequestCompletedEvent) {
        log.Printf("%s %s -> %d err=%v", e.Method, e.URL, e.StatusCode, e.Err)
    },
},
```

---

## Errors

| Error | When |
|-------|------|
| `ErrNoBaseURLs` | `New` with empty `BaseURLs` |
| `ErrEmptyURL` | Simple call with empty `URL` |
| `ErrCircuitOpen` | All hosts blocked by breaker, or single host breaker rejected |
| `ErrHealthCheck` | Health check failed after HTTP success |
| `ErrUnexpectedResponse` | Internal breaker type mismatch (should not happen in normal use) |

Always use `errors.Is` / `errors.As` — do not compare with `==` only.

```go
resp, err := client.Get(ctx, "/x", httpc.RequestParams{})
if errors.Is(err, httpc.ErrHealthCheck) {
    // handle unhealthy upstream, inspect resp.Body
    return
}
if err != nil {
  return err
}
```

---

## Performance and tuning

**Do**

- One shared `Client` per upstream dependency.
- Set `MaxResponseBytes` to the smallest size you can tolerate.
- Set `MaxConcurrentRequests` if outbound parallelism must be bounded.
- Reuse a custom `Transport` across clients only if you understand connection pooling implications.

**Default transport** (when `Transport` is nil): `MaxIdleConns=256`, `MaxIdleConnsPerHost=64`, HTTP/2 enabled, 90s idle timeout.

**Avoid**

- `Simple*` in hot loops (allocates `http.Client` per call).
- Heavy work inside hooks.
- Unlimited body size on untrusted upstreams (use `MaxResponseBytes`).

Example tuned transport:

```go
tr := &http.Transport{
    MaxIdleConns:        512,
    MaxIdleConnsPerHost: 128,
    IdleConnTimeout:     90 * time.Second,
    ForceAttemptHTTP2:   true,
}

client, _ := httpc.New(httpc.Config{
    BaseURLs:  []string{"https://api.example.com"},
    Transport: tr,
})
```

---

## FAQ

**Why is my 503 not opening the circuit breaker?**  
Only **transport-level** failures trip the breaker. An HTTP 503 with a completed response is returned normally. Use `HealthCheck` if you want 5xx to fail the call.

**Does health check open the breaker?**  
No. It returns `ErrHealthCheck` but does not increment breaker failures.

**Can I use different paths per host?**  
`BaseURLs` are origins only; path is per request. Hosts should share the same API surface behind the balancer.

**Are redirects followed?**  
Yes, via `net/http` default client behavior.

**Thread-safe?**  
Yes. One `Client` can be used from many goroutines.

**What if I pass a full URL to `Client.Get`?**  
Supported; `BaseURLs` are ignored for that call. Per-host breaker is keyed by the URL’s origin.

---

## Development

```bash
make test    # go test -race
make lint    # golangci-lint
```

CI runs tests and lint on push/PR.
