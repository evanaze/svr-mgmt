package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://ai-kvm.spitz-pickerel.ts.net"

type config struct {
	baseURL            string
	username           string
	password           string
	insecureSkipVerify bool
	timeout            time.Duration
	wait               bool
}

type apiClient struct {
	baseURL  *url.URL
	username string
	password string
	http     *http.Client
}

type apiResponse[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Error  string `json:"error"`
}

type httpStatusError struct {
	statusCode int
	body       string
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("GLKVM returned HTTP %d: %s", e.statusCode, e.body)
}

type atxState struct {
	Busy    bool `json:"busy"`
	Enabled bool `json:"enabled"`
	LEDs    struct {
		Power bool `json:"power"`
		HDD   bool `json:"hdd"`
	} `json:"leds"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, command, err := parseArgs(args)
	if err != nil {
		return err
	}

	if command == "" || command == "help" {
		printUsage(os.Stdout)
		return nil
	}

	if cfg.password == "" {
		return errors.New("missing password; set GLKVM_PASSWORD or pass -password")
	}

	client, err := newAPIClient(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	if err := client.login(ctx); err != nil {
		return err
	}

	switch command {
	case "status":
		state, err := client.atxState(ctx)
		if err != nil {
			return err
		}
		printState(state)
	case "on":
		return client.powerOn(ctx, cfg.wait)
	case "off":
		return client.setPower(ctx, "off", cfg.wait)
	case "force-off", "off-hard":
		return client.setPower(ctx, "off_hard", cfg.wait)
	case "reset", "reset-hard":
		return client.setPower(ctx, "reset_hard", cfg.wait)
	case "click":
		return client.click(ctx, "power", cfg.wait)
	case "click-long":
		return client.click(ctx, "power_long", cfg.wait)
	case "reset-click":
		return client.click(ctx, "reset", cfg.wait)
	default:
		return fmt.Errorf("unknown command %q", command)
	}

	return nil
}

func parseArgs(args []string) (config, string, error) {
	cfg := config{
		baseURL:            envDefault("GLKVM_URL", defaultBaseURL),
		username:           envDefault("GLKVM_USER", "admin"),
		password:           os.Getenv("GLKVM_PASSWORD"),
		insecureSkipVerify: envBoolDefault("GLKVM_INSECURE_SKIP_VERIFY", true),
		timeout:            10 * time.Second,
		wait:               true,
	}

	fs := flag.NewFlagSet("svr-mgmt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.baseURL, "url", cfg.baseURL, "GLKVM base URL, for example https://ai-kvm")
	fs.StringVar(&cfg.username, "user", cfg.username, "GLKVM username")
	fs.StringVar(&cfg.password, "password", cfg.password, "GLKVM password")
	fs.BoolVar(&cfg.insecureSkipVerify, "insecure-skip-verify", cfg.insecureSkipVerify, "skip TLS certificate verification for self-signed KVM certs")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "request timeout")
	fs.BoolVar(&cfg.wait, "wait", cfg.wait, "wait for ATX power operation to finish")

	if err := fs.Parse(args); err != nil {
		return cfg, "", err
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return cfg, "", nil
	}
	if len(remaining) > 1 {
		return cfg, "", fmt.Errorf("expected one command, got %d: %s", len(remaining), strings.Join(remaining, " "))
	}

	return cfg, remaining[0], nil
}

func newAPIClient(cfg config) (*apiClient, error) {
	rawURL := cfg.baseURL
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	baseURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse GLKVM URL: %w", err)
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("GLKVM URL %q is missing a host", cfg.baseURL)
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.insecureSkipVerify}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &apiClient{
		baseURL:  baseURL,
		username: cfg.username,
		password: cfg.password,
		http: &http.Client{
			Transport: transport,
			Jar:       jar,
		},
	}, nil
}

func (c *apiClient) login(ctx context.Context) error {
	form := url.Values{}
	form.Set("user", c.username)
	form.Set("passwd", c.password)

	var response apiResponse[map[string]string]
	if err := c.doWithBody(ctx, http.MethodPost, "/api/auth/login", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &response); err != nil {
		return err
	}
	if !response.OK {
		return apiError(response.Error)
	}
	return nil
}

func (c *apiClient) atxState(ctx context.Context) (atxState, error) {
	var response apiResponse[atxState]
	if err := c.do(ctx, http.MethodGet, "/api/atx", nil, &response); err != nil {
		return atxState{}, err
	}
	if !response.OK {
		return atxState{}, apiError(response.Error)
	}
	return response.Result, nil
}

func (c *apiClient) setPower(ctx context.Context, action string, wait bool) error {
	query := url.Values{}
	query.Set("action", action)
	query.Set("wait", boolQuery(wait))

	var response apiResponse[map[string]json.RawMessage]
	if err := c.do(ctx, http.MethodPost, "/api/atx/power", query, &response); err != nil {
		return err
	}
	if !response.OK {
		return apiError(response.Error)
	}

	fmt.Printf("sent ATX power action: %s\n", action)
	return nil
}

func (c *apiClient) powerOn(ctx context.Context, wait bool) error {
	state, err := c.atxState(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return errors.New("ATX power control is disabled")
	}
	if state.Busy {
		return errors.New("ATX is busy performing another operation")
	}
	if state.LEDs.Power {
		fmt.Println("server already powered on")
		return nil
	}

	err = c.setPower(ctx, "on", wait)
	if err == nil || !wait {
		return err
	}

	var statusErr httpStatusError
	if !errors.As(err, &statusErr) || statusErr.statusCode != http.StatusInternalServerError {
		return err
	}

	state, stateErr := c.atxState(ctx)
	if stateErr != nil {
		return err
	}
	if state.LEDs.Power {
		fmt.Println("server powered on despite GLKVM reporting HTTP 500")
		return nil
	}

	return err
}

func (c *apiClient) click(ctx context.Context, button string, wait bool) error {
	query := url.Values{}
	query.Set("button", button)
	query.Set("wait", boolQuery(wait))

	var response apiResponse[map[string]json.RawMessage]
	if err := c.do(ctx, http.MethodPost, "/api/atx/click", query, &response); err != nil {
		return err
	}
	if !response.OK {
		return apiError(response.Error)
	}

	fmt.Printf("sent ATX button click: %s\n", button)
	return nil
}

func (c *apiClient) do(ctx context.Context, method string, path string, query url.Values, out any) error {
	return c.doWithBody(ctx, method, path, query, nil, "", out)
}

func (c *apiClient) doWithBody(ctx context.Context, method string, path string, query url.Values, requestBody io.Reader, contentType string, out any) error {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-KVMD-User", c.username)
	req.Header.Set("X-KVMD-Passwd", c.password)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, endpoint.Redacted(), err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return httpStatusError{statusCode: res.StatusCode, body: strings.TrimSpace(string(body))}
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode GLKVM response: %w: %s", err, strings.TrimSpace(string(body)))
	}

	return nil
}

func apiError(message string) error {
	if message == "" {
		return errors.New("GLKVM API returned ok=false")
	}
	return fmt.Errorf("GLKVM API returned ok=false: %s", message)
}

func printState(state atxState) {
	fmt.Printf("enabled: %t\n", state.Enabled)
	fmt.Printf("busy:    %t\n", state.Busy)
	fmt.Printf("power:   %s\n", onOff(state.LEDs.Power))
	fmt.Printf("hdd:     %s\n", onOff(state.LEDs.HDD))
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: svr-mgmt [flags] <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  status       Show ATX power/HDD LED state")
	fmt.Fprintln(w, "  on           Power on if currently off")
	fmt.Fprintln(w, "  off          Soft power off via ACPI power button")
	fmt.Fprintln(w, "  force-off    Long-press power button")
	fmt.Fprintln(w, "  reset        Hardware reset")
	fmt.Fprintln(w, "  click        Short press power button")
	fmt.Fprintln(w, "  click-long   Long press power button")
	fmt.Fprintln(w, "  reset-click  Short press reset button")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags can also be provided with env vars:")
	fmt.Fprintln(w, "  -url                   GLKVM_URL, defaults to https://ai-kvm")
	fmt.Fprintln(w, "  -user                  GLKVM_USER, defaults to admin")
	fmt.Fprintln(w, "  -password              GLKVM_PASSWORD, required")
	fmt.Fprintln(w, "  -insecure-skip-verify  GLKVM_INSECURE_SKIP_VERIFY, defaults to true")
	fmt.Fprintln(w, "  -timeout               defaults to 10s")
	fmt.Fprintln(w, "  -wait                  defaults to true")
}

func envDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBoolDefault(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolQuery(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
