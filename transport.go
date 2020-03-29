package netter

import (
	"net"
	"net/http"
	"time"
)

var defaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		// Limits the time spent establishing a TCP connection
		// Errors:
		// i/o timeout
		Timeout: 30 * time.Second,
		// TCP KeepAlive specifies the interval between keep-alive probes for an active network connection.
		KeepAlive: 30 * time.Second,
	}).DialContext,
	// Limits the time spent reading the headers of the response
	// Errors:
	// net/http: timeout awaiting response headers
	ResponseHeaderTimeout: 30 * time.Second,
	MaxIdleConns:          100,
	// How long an idle connection is kept in the connection pool
	IdleConnTimeout:       90 * time.Second,
	ExpectContinueTimeout: 5 * time.Second,
	DisableKeepAlives:     true,
	MaxIdleConnsPerHost:   -1,
}
