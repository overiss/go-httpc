package client

import "github.com/overiss/go-httpc/v2/internal/engine"

// HealthCheckOK200 reports whether the status code is exactly 200.
func HealthCheckOK200(resp *engine.Response) bool {
	return resp != nil && resp.StatusCode == 200
}

// HealthCheckOK2xx reports whether the status code is in the 2xx range.
func HealthCheckOK2xx(resp *engine.Response) bool {
	return resp != nil && resp.OK()
}
