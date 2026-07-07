package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAPIClientLoginAndAtxStateUseSessionCookieWithoutKVMDHeaders(t *testing.T) {
	t.Parallel()

	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			if got := r.Header.Get("X-KVMD-User"); got != "" {
				t.Fatalf("login X-KVMD-User = %q, want empty", got)
			}
			if got := r.Header.Get("X-KVMD-Passwd"); got != "" {
				t.Fatalf("login X-KVMD-Passwd = %q, want empty", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Fatalf("login Content-Type = %q, want %q", got, "application/x-www-form-urlencoded")
			}
			body := mustReadBody(t, r)
			if got := body.Get("user"); got != "admin" {
				t.Fatalf("login user = %q, want %q", got, "admin")
			}
			if got := body.Get("passwd"); got != "secret" {
				t.Fatalf("login passwd = %q, want %q", got, "secret")
			}
			http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session-token", Path: "/"})
			writeJSON(t, w, `{"ok":true,"result":{}}`)
		case "/api/atx":
			if got := r.Header.Get("X-KVMD-User"); got != "" {
				t.Fatalf("atx X-KVMD-User = %q, want empty", got)
			}
			if got := r.Header.Get("X-KVMD-Passwd"); got != "" {
				t.Fatalf("atx X-KVMD-Passwd = %q, want empty", got)
			}
			if cookie, err := r.Cookie("auth_token"); err != nil {
				t.Fatalf("atx cookie error = %v", err)
			} else if cookie.Value != "session-token" {
				t.Fatalf("atx auth_token = %q, want %q", cookie.Value, "session-token")
			}
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"leds":{"power":false,"hdd":false}}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.login(context.Background()); err != nil {
		t.Fatalf("login() error = %v", err)
	}

	state, err := client.atxState(context.Background())
	if err != nil {
		t.Fatalf("atxState() error = %v", err)
	}
	if state.Busy {
		t.Fatal("atxState().Busy = true, want false")
	}
}

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
		http: &http.Client{
			Transport: server.Client().Transport,
			Jar:       mustCookieJar(t),
		},
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

func mustReadBody(t *testing.T, r *http.Request) url.Values {
	t.Helper()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	values, parseErr := url.ParseQuery(strings.TrimSpace(string(data)))
	if parseErr != nil {
		t.Fatalf("url.ParseQuery() error = %v", parseErr)
	}
	return values
}

func mustCookieJar(t *testing.T) http.CookieJar {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	return jar
}
