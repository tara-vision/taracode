package cmd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// fakeInternet answers every request for a non-loopback host that goes through
// http.DefaultTransport with handler, for the rest of the test; requests to loopback addresses
// (the fake LLM and search servers) pass through unchanged. The release check and the search
// providers build their clients on the default transport, so nothing they send leaves the machine.
// The handler sees the original host in r.Host.
func fakeInternet(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	forward := &http.Transport{}
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if isLoopback(r.URL.Hostname()) {
			return original.RoundTrip(r)
		}
		out := r.Clone(r.Context())
		out.Host = r.URL.Host
		out.URL.Scheme, out.URL.Host = target.Scheme, target.Host
		return forward.RoundTrip(out)
	})
	t.Cleanup(func() {
		http.DefaultTransport = original
		forward.CloseIdleConnections()
	})
}
