package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestAPIClientPowerOnSkips_whenAlreadyOn(t *testing.T) {
	t.Parallel()

	var powerRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/power":
			powerRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.powerOn(context.Background(), true); err != nil {
		t.Fatalf("powerOn() error = %v", err)
	}

	if got := powerRequests.Load(); got != 0 {
		t.Fatalf("expected no power request, got %d", got)
	}
}

func TestAPIClientPowerOnReturnsError_whenBusy(t *testing.T) {
	t.Parallel()

	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/atx" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(t, w, `{"ok":true,"result":{"busy":true,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
	}))

	err := client.powerOn(context.Background(), true)
	if err == nil {
		t.Fatal("powerOn() error = nil, want busy error")
	}
	if got := err.Error(); got != "ATX is busy performing another operation" {
		t.Fatalf("powerOn() error = %q, want %q", got, "ATX is busy performing another operation")
	}
}

func TestAPIClientPowerOnPosts_whenOff(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	var powerRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			powerRequests.Add(1)
			if got := r.URL.Query().Get("action"); got != "on" {
				t.Fatalf("action = %q, want %q", got, "on")
			}
			if got := r.URL.Query().Get("wait"); got != "1" {
				t.Fatalf("wait = %q, want %q", got, "1")
			}
			if got := r.Method; got != http.MethodPost {
				t.Fatalf("method = %q, want %q", got, http.MethodPost)
			}
			writeJSON(t, w, `{"ok":true,"result":{}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.powerOn(context.Background(), true); err != nil {
		t.Fatalf("powerOn() error = %v", err)
	}

	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
	if got := powerRequests.Load(); got != 1 {
		t.Fatalf("power request count = %d, want 1", got)
	}
}

func TestAPIClientPowerOnReturnsSuccess_whenHTTP500ButStateTurnsOn(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			requestNumber := stateRequests.Add(1)
			if requestNumber == 1 {
				writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
				return
			}
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.powerOn(context.Background(), true); err != nil {
		t.Fatalf("powerOn() error = %v", err)
	}

	if got := stateRequests.Load(); got != 2 {
		t.Fatalf("state request count = %d, want 2", got)
	}
}

func TestAPIClientPowerOnReturnsOriginalError_whenHTTP500AndStateStaysOff(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	err := client.powerOn(context.Background(), true)
	if err == nil {
		t.Fatal("powerOn() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("powerOn() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 2 {
		t.Fatalf("state request count = %d, want 2", got)
	}
}

func TestAPIClientPowerOnReturnsOriginalErrorWithoutRecheck_whenWaitDisabledAndHTTP500(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	err := client.powerOn(context.Background(), false)
	if err == nil {
		t.Fatal("powerOn() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("powerOn() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func newTestAPIClient(t *testing.T, handler http.Handler) *apiClient {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	return &apiClient{
		baseURL:  baseURL,
		username: "admin",
		password: "secret",
		http:     server.Client(),
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, err := fmt.Fprint(w, body)
	if err != nil {
		t.Fatalf("fmt.Fprint() error = %v", err)
	}
}
