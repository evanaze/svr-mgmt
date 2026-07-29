package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
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
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
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

func TestAPIClientPowerOnSkips_whenInSleepState(t *testing.T) {
	t.Parallel()

	var powerRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"sleep","leds":{"power":false,"hdd":false}}}`)
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
		writeJSON(t, w, `{"ok":true,"result":{"busy":true,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
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
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
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
				writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
				return
			}
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
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
	client := newTestAPIClientWithTimeout(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}), 100*time.Millisecond)

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
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
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

func TestAPIClientPowerOnRecovers_whenHTTP500AndParentContextExhausted(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			requestNumber := stateRequests.Add(1)
			if requestNumber == 1 {
				writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
				return
			}
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/power":
			// Simulate the GLKVM holding the connection while waiting for the
			// ATX operation to complete, then returning HTTP 500.
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	// Give the parent context a timeout shorter than the POST delay so it
	// expires before the re-check can run — reproducing the bug where the
	// shared context was exhausted by the wait=1 POST.
	ctx, cancel := context.WithTimeout(context.Background(), 550*time.Millisecond)
	defer cancel()

	if err := client.powerOn(ctx, true); err != nil {
		t.Fatalf("powerOn() error = %v", err)
	}

	if got := stateRequests.Load(); got != 2 {
		t.Fatalf("state request count = %d, want 2", got)
	}
}

func TestSetPower_RecoversFromHTTP500_whenActionOff(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			if got := r.URL.Query().Get("action"); got != "off" {
				t.Fatalf("action = %q, want %q", got, "off")
			}
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.setPower(context.Background(), "off", true); err != nil {
		t.Fatalf("setPower() error = %v", err)
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func TestSetPower_RecoversFromHTTP500_whenActionOffHard(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			if got := r.URL.Query().Get("action"); got != "off_hard" {
				t.Fatalf("action = %q, want %q", got, "off_hard")
			}
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.setPower(context.Background(), "off_hard", true); err != nil {
		t.Fatalf("setPower() error = %v", err)
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func TestSetPower_RecoversFromHTTP500_whenActionResetHard(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			requestNumber := stateRequests.Add(1)
			if requestNumber == 1 {
				writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
				return
			}
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.setPower(context.Background(), "reset_hard", true); err != nil {
		t.Fatalf("setPower() error = %v", err)
	}
	if got := stateRequests.Load(); got != 2 {
		t.Fatalf("state request count = %d, want 2", got)
	}
}

func TestSetPower_ReturnsOriginalError_whenHTTP500AndStateUnchanged(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClientWithTimeout(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}), 100*time.Millisecond)

	err := client.setPower(context.Background(), "off", true)
	if err == nil {
		t.Fatal("setPower() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("setPower() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func TestSetPower_ReturnsErrorWithoutRecheck_whenWaitDisabledAndHTTP500(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"off","leds":{"power":false,"hdd":false}}}`)
		case "/api/atx/power":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	err := client.setPower(context.Background(), "off", false)
	if err == nil {
		t.Fatal("setPower() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("setPower() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 0 {
		t.Fatalf("state request count = %d, want 0", got)
	}
}

func TestClick_RecoversFromHTTP500_whenWaitTrue(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/click":
			if got := r.URL.Query().Get("button"); got != "power" {
				t.Fatalf("button = %q, want %q", got, "power")
			}
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	if err := client.click(context.Background(), "power", true); err != nil {
		t.Fatalf("click() error = %v", err)
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func TestClick_ReturnsOriginalError_whenHTTP500AndStateBusy(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClientWithTimeout(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":true,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/click":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}), 100*time.Millisecond)

	err := client.click(context.Background(), "power", true)
	if err == nil {
		t.Fatal("click() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("click() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 1 {
		t.Fatalf("state request count = %d, want 1", got)
	}
}

func TestClick_ReturnsErrorWithoutRecheck_whenWaitDisabledAndHTTP500(t *testing.T) {
	t.Parallel()

	var stateRequests atomic.Int32
	client := newTestAPIClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/atx":
			stateRequests.Add(1)
			writeJSON(t, w, `{"ok":true,"result":{"busy":false,"enabled":true,"power":"on","leds":{"power":true,"hdd":false}}}`)
		case "/api/atx/click":
			http.Error(w, "Server got itself in trouble", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	err := client.click(context.Background(), "power", false)
	if err == nil {
		t.Fatal("click() error = nil, want HTTP 500 error")
	}
	if got := err.Error(); got != "GLKVM returned HTTP 500: Server got itself in trouble" {
		t.Fatalf("click() error = %q, want %q", got, "GLKVM returned HTTP 500: Server got itself in trouble")
	}
	if got := stateRequests.Load(); got != 0 {
		t.Fatalf("state request count = %d, want 0", got)
	}
}

func newTestAPIClient(t *testing.T, handler http.Handler) *apiClient {
	return newTestAPIClientWithTimeout(t, handler, 10*time.Second)
}

func newTestAPIClientWithTimeout(t *testing.T, handler http.Handler, timeout time.Duration) *apiClient {
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
		timeout: timeout,
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

func TestParseArgs_WaitDefaultsFalse(t *testing.T) {
	t.Parallel()

	cfg, _, err := parseArgs([]string{"status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.wait {
		t.Fatal("cfg.wait = true, want false by default")
	}
}

func TestParseArgs_KeepAwakeLongFlag(t *testing.T) {
	t.Parallel()

	cfg, command, err := parseArgs([]string{"-keep-awake", "status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if !cfg.keepAwake {
		t.Fatal("cfg.keepAwake = false, want true")
	}
	if command != "status" {
		t.Fatalf("command = %q, want %q", command, "status")
	}
}

func TestParseArgs_KeepAwakeShortAlias(t *testing.T) {
	t.Parallel()

	cfg, command, err := parseArgs([]string{"-ka", "on"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if !cfg.keepAwake {
		t.Fatal("cfg.keepAwake = false, want true")
	}
	if command != "on" {
		t.Fatalf("command = %q, want %q", command, "on")
	}
}

func TestParseArgs_KeepAwakeDefaultsFalse(t *testing.T) {
	t.Parallel()

	cfg, _, err := parseArgs([]string{"status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.keepAwake {
		t.Fatal("cfg.keepAwake = true, want false by default")
	}
}

func TestParseArgs_CaffeineSchemaDirOverride(t *testing.T) {
	t.Parallel()

	const want = "/tmp/some-schema-dir"
	cfg, _, err := parseArgs([]string{"-caffeine-schema-dir", want, "status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.caffeineSchemaDir != want {
		t.Fatalf("cfg.caffeineSchemaDir = %q, want %q", cfg.caffeineSchemaDir, want)
	}
}

func TestCaffeineGSettingsArgs_EnableTrue(t *testing.T) {
	t.Parallel()

	got := caffeineGSettingsArgs("/schemas/caffeine", true)
	want := []string{
		"--schemadir", "/schemas/caffeine",
		"set",
		"org.gnome.shell.extensions.caffeine",
		"cli-toggle",
		"true",
	}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCaffeineGSettingsArgs_EnableFalse(t *testing.T) {
	t.Parallel()

	got := caffeineGSettingsArgs("/schemas/caffeine", false)
	want := []string{
		"--schemadir", "/schemas/caffeine",
		"set",
		"org.gnome.shell.extensions.caffeine",
		"cli-toggle",
		"false",
	}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefaultCaffeineSchemaDir_PrefersExistingCompiledSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	compiled := filepath.Join(dir, "gschemas.compiled")
	if err := os.WriteFile(compiled, []byte{}, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	original := caffeineSchemaCandidates
	t.Cleanup(func() { caffeineSchemaCandidates = original })

	caffeineSchemaCandidates = func() []string {
		return []string{"/nonexistent/path", dir, "/also/nonexistent"}
	}

	if got := defaultCaffeineSchemaDir(); got != dir {
		t.Fatalf("defaultCaffeineSchemaDir() = %q, want %q", got, dir)
	}
}

func TestDefaultCaffeineSchemaDir_FallsBackToFirstCandidate(t *testing.T) {
	t.Parallel()

	original := caffeineSchemaCandidates
	t.Cleanup(func() { caffeineSchemaCandidates = original })

	const first = "/nonexistent/first"
	caffeineSchemaCandidates = func() []string {
		return []string{first, "/nonexistent/second"}
	}

	if got := defaultCaffeineSchemaDir(); got != first {
		t.Fatalf("defaultCaffeineSchemaDir() = %q, want %q", got, first)
	}
}

func TestEnableCaffeine_ReturnsErrorWhenSchemaMissing(t *testing.T) {
	t.Parallel()

	missing := t.TempDir()
	err := enableCaffeine(context.Background(), missing)
	if err == nil {
		t.Fatal("enableCaffeine() error = nil, want error for missing schema")
	}
	if !strings.Contains(err.Error(), "gsettings") {
		t.Fatalf("enableCaffeine() error = %q, want it to mention gsettings", err.Error())
	}
}

func TestEnableCaffeine_SucceedsAgainstRealGSettings(t *testing.T) {
	t.Skip("requires the caffeine extension schema installed (mars); validated manually")
}
