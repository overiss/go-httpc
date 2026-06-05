package engine

import (
	"net/http"

	"golang.org/x/oauth2"
)

func wrapOAuth2Transport(base http.RoundTripper, src oauth2.TokenSource) http.RoundTripper {
	if src == nil {
		return base
	}
	return &oauth2.Transport{
		Base:   base,
		Source: oauth2.ReuseTokenSource(nil, src),
	}
}
