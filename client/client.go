package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/overiss/go-httpc/v2/internal/engine"
)

// Client is a configured, reusable HTTP client.
type Client struct {
	exec *engine.Executor
}

// New creates a Client from Config.
func New(cfg Config) (*Client, error) {
	opts := cfg.engineOptions()
	if cfg.OAuth2 != nil {
		ts, err := cfg.OAuth2(context.Background())
		if err != nil {
			return nil, fmt.Errorf("httpc: oauth2: %w", err)
		}
		opts.OAuth2TokenSource = ts
	}
	exec, err := engine.NewExecutor(opts)
	if err != nil {
		return nil, err
	}
	return &Client{exec: exec}, nil
}

// Get sends an HTTP GET.
func (c *Client) Get(ctx context.Context, path string, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(http.MethodGet, path, nil))
}

// Post sends an HTTP POST with body.
func (c *Client) Post(ctx context.Context, path string, body []byte, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(http.MethodPost, path, body))
}

// Put sends an HTTP PUT with body.
func (c *Client) Put(ctx context.Context, path string, body []byte, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(http.MethodPut, path, body))
}

// Patch sends an HTTP PATCH with body.
func (c *Client) Patch(ctx context.Context, path string, body []byte, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(http.MethodPatch, path, body))
}

// Delete sends an HTTP DELETE.
func (c *Client) Delete(ctx context.Context, path string, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(http.MethodDelete, path, nil))
}

// Do sends a custom HTTP request.
func (c *Client) Do(ctx context.Context, method, path string, body []byte, params RequestParams) (*engine.Response, error) {
	return c.exec.Do(ctx, params.toCall(method, path, body))
}
