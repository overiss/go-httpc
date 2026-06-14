package client

import (
	"net/http"

	"github.com/overiss/go-httpc/v2/internal/engine"
)

// RequestParams are per-request settings for Client methods.
type RequestParams struct {
	Headers     engine.Headers
	Query       engine.Query
	HealthCheck engine.HealthCheck
	// CompleteHook overrides Hooks.OnRequestCompleted for this request. Nil = client default.
	CompleteHook engine.RequestCompletedHook
}

// ReqCopyOptions controls what WithReqParams copies from *http.Request.
type ReqCopyOptions struct {
	CopyHeaders  bool
	CopyQuery    bool
	HeaderKeys   []string
	QueryKeys    []string
	SkipHopByHop bool
}

// WithReqParams merges headers and/or query from r into p according to opts.
func (p *RequestParams) WithReqParams(r *http.Request, opts ReqCopyOptions) {
	if p == nil || r == nil {
		return
	}
	if opts.CopyHeaders {
		p.copyHeadersFrom(r, opts)
	}
	if opts.CopyQuery {
		p.copyQueryFrom(r, opts)
	}
}

func (p *RequestParams) copyHeadersFrom(r *http.Request, opts ReqCopyOptions) {
	if p.Headers == nil {
		p.Headers = make(engine.Headers)
	}
	if len(opts.HeaderKeys) > 0 {
		for _, key := range opts.HeaderKeys {
			if opts.SkipHopByHop && engine.IsHopByHopHeader(key) {
				continue
			}
			if v := r.Header.Get(key); v != "" {
				p.Headers[key] = v
			}
		}
		return
	}
	for k := range r.Header {
		if opts.SkipHopByHop && engine.IsHopByHopHeader(k) {
			continue
		}
		if v := r.Header.Get(k); v != "" {
			p.Headers[k] = v
		}
	}
}

func (p *RequestParams) copyQueryFrom(r *http.Request, opts ReqCopyOptions) {
	if r.URL == nil {
		return
	}
	src := r.URL.Query()
	if len(src) == 0 {
		return
	}
	if p.Query == nil {
		p.Query = make(engine.Query)
	}
	if len(opts.QueryKeys) > 0 {
		for _, key := range opts.QueryKeys {
			if vv, ok := src[key]; ok && len(vv) > 0 {
				cp := make([]string, len(vv))
				copy(cp, vv)
				p.Query[key] = cp
			}
		}
		return
	}
	for k, vv := range src {
		cp := make([]string, len(vv))
		copy(cp, vv)
		p.Query[k] = cp
	}
}

func (p RequestParams) toCall(method, path string, body []byte) engine.Call {
	return engine.Call{
		Method:       method,
		Path:         path,
		Body:         body,
		Headers:      p.Headers,
		Query:        engine.CloneQuery(p.Query),
		HealthCheck:  p.HealthCheck,
		CompleteHook: p.CompleteHook,
	}
}
