package search

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// seenRequest is what the fake web saw of one request.
type seenRequest struct {
	Method, Host, Path string
	Query              url.Values
	Form               url.Values
	Header             http.Header
}

// fakeWeb is an in-process stand-in for the search engines: every request a client from client()
// sends lands on handler, whatever host it names, and is recorded.
type fakeWeb struct {
	mu       sync.Mutex
	requests []seenRequest
	target   *url.URL
}

func newFakeWeb(t *testing.T, handler http.HandlerFunc) *fakeWeb {
	t.Helper()
	w := &fakeWeb{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.mu.Lock()
		w.requests = append(w.requests, seenRequest{Method: r.Method, Host: r.Host, Path: r.URL.Path,
			Query: r.URL.Query(), Form: r.PostForm, Header: r.Header.Clone()})
		w.mu.Unlock()
		handler(rw, r)
	}))
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	w.target = target
	return w
}

// client sends every request to the fake, keeping the original host in the Host header.
func (w *fakeWeb) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		out := r.Clone(r.Context())
		out.Host = r.URL.Host
		out.URL.Scheme, out.URL.Host = w.target.Scheme, w.target.Host
		return http.DefaultTransport.RoundTrip(out)
	})}
}

func (w *fakeWeb) seen() []seenRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]seenRequest(nil), w.requests...)
}

// brokenClient fails every request before it leaves the process.
func brokenClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network is down")
	})}
}
