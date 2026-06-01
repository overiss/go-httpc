package engine

import "net/http"

func (h Headers) HTTPHeader() http.Header {
	if len(h) == 0 {
		return nil
	}
	out := make(http.Header, len(h))
	for k, v := range h {
		out.Set(k, v)
	}
	return out
}

func MergeHeaders(defaults http.Header, perRequest Headers) http.Header {
	if len(defaults) == 0 && len(perRequest) == 0 {
		return nil
	}
	if len(perRequest) == 0 {
		return defaults.Clone()
	}
	out := defaults.Clone()
	if out == nil {
		out = make(http.Header)
	}
	for k, v := range perRequest {
		out.Set(k, v)
	}
	return out
}

var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func IsHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[http.CanonicalHeaderKey(key)]
	return ok
}

func CloneQuery(q Query) map[string][]string {
	if len(q) == 0 {
		return nil
	}
	out := make(map[string][]string, len(q))
	for k, vv := range q {
		cp := make([]string, len(vv))
		copy(cp, vv)
		out[k] = cp
	}
	return out
}
