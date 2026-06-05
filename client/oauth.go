package client

import (
	"context"

	"golang.org/x/oauth2"
)

// OAuth2Config builds a token source used to authorize outbound requests.
// It is called once during New with context.Background().
// The returned source is passed to oauth2.Transport (same model as oauth2.NewClient).
// Nil disables OAuth2.
type OAuth2Config func(ctx context.Context) (oauth2.TokenSource, error)
