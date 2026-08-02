package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
)

// debugWriter is where GLKVM API debug logging goes. It points at stderr so it
// never pollutes the command's stdout.
var debugWriter io.Writer = os.Stderr

type apiClient struct {
	baseURL  *url.URL
	username string
	password string
	debug    bool
	http     *http.Client
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
		debug:    cfg.debug,
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

// setPower issues an ATX power action and returns only once the GLKVM has
// finished performing it. It passes wait=true so the API withholds its HTTP
// response until the hardware operation completes (or the request times out),
// keeping the CLI fully synchronous.
func (c *apiClient) setPower(ctx context.Context, action string) error {
	query := url.Values{}
	query.Set("action", action)
	query.Set("wait", "true")

	var response apiResponse[map[string]json.RawMessage]
	if err := c.do(ctx, http.MethodPost, "/api/atx/power", query, &response); err != nil {
		return err
	}
	if !response.OK {
		return apiError(response.Error)
	}

	fmt.Printf("Server power on: %s\n", action)
	return nil
}

func (c *apiClient) powerOn(ctx context.Context) error {
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
	if state.isPoweredOn() {
		fmt.Println("server already powered on")
		return nil
	}
	return c.setPower(ctx, "on")
}

// click issues an ATX button click and returns only once the GLKVM has finished
// performing it. It passes wait=true so the API withholds its HTTP response
// until the click completes (or the request times out), keeping the CLI fully
// synchronous.
func (c *apiClient) click(ctx context.Context, button string) error {
	query := url.Values{}
	query.Set("button", button)
	query.Set("wait", "true")

	var response apiResponse[map[string]json.RawMessage]
	if err := c.do(ctx, http.MethodPost, "/api/atx/click", query, &response); err != nil {
		return err
	}
	if !response.OK {
		return apiError(response.Error)
	}

	fmt.Printf("Server button click: %s\n", button)
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

	var bodyBytes []byte
	var err error
	if requestBody != nil {
		bodyBytes, err = io.ReadAll(requestBody)
		if err != nil {
			return fmt.Errorf("read request body: %w", err)
		}
	}

	if c.debug {
		logDebugRequest(method, endpoint, contentType, bodyBytes, c.password)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, endpoint.Redacted(), err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if c.debug {
		logDebugResponse(res.StatusCode, body)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return httpStatusError{statusCode: res.StatusCode, body: strings.TrimSpace(string(body))}
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode GLKVM response: %w: %s", err, strings.TrimSpace(string(body)))
	}

	return nil
}

// logDebugRequest writes the outgoing request to stderr with the password
// redacted from both the URL and the body.
func logDebugRequest(method string, endpoint url.URL, contentType string, body []byte, password string) {
	endpoint.RawQuery = redactQuerySecrets(endpoint.RawQuery)
	fmt.Fprintf(debugWriter, "> %s %s\n", method, endpoint.String())
	if contentType != "" {
		fmt.Fprintf(debugWriter, "> content-type: %s\n", contentType)
	}
	if len(body) > 0 {
		s := string(body)
		s = redactBodySecrets(s, password)
		fmt.Fprintf(debugWriter, "> body: %s\n", s)
	}
}

// logDebugResponse writes the received response to stderr.
func logDebugResponse(statusCode int, body []byte) {
	fmt.Fprintf(debugWriter, "< HTTP %d\n", statusCode)
	if len(body) > 0 {
		fmt.Fprintf(debugWriter, "< body: %s\n", strings.TrimSpace(string(body)))
	}
}

// redactQuerySecrets redacts credential-like query values for logging.
func redactQuerySecrets(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	for _, key := range []string{"passwd", "password", "token", "auth_token"} {
		rawQuery = redactFormField(rawQuery, key)
	}
	return rawQuery
}

// redactBodySecrets replaces the known password value with a placeholder in a
// request body so credentials never reach the debug log.
func redactBodySecrets(body string, password string) string {
	if password != "" {
		body = strings.ReplaceAll(body, password, "*****")
	}
	return redactFormField(body, "passwd")
}

func redactFormField(body string, field string) string {
	marker := field + "="
	var sb strings.Builder
	for {
		idx := strings.Index(body, marker)
		if idx < 0 {
			sb.WriteString(body)
			return sb.String()
		}
		sb.WriteString(body[:idx] + marker + "*****")
		body = body[idx+len(marker):]
		if end := strings.IndexAny(body, "&\n"); end >= 0 {
			sb.WriteString(body[end:])
			return sb.String()
		}
		return sb.String()
	}
}
