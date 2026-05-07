package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors for the categories the CLI maps to specific exit codes.
// Wrap with %w via fmt.Errorf when adding context.
var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInsufficientCredit = errors.New("insufficient credit")
	ErrNotFound           = errors.New("not found")
	ErrServer             = errors.New("server error")
	ErrBadRequest         = errors.New("bad request")
	ErrConflict           = errors.New("conflict")
)

// APIError carries server-supplied error metadata when present. Callers
// can errors.Is against the sentinels above; APIError unwraps to one of
// them via Unwrap().
type APIError struct {
	Status       int
	Code         string
	Message      string
	BalanceCents *int64
	cause        error
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("%s (status %d): %s", e.Code, e.Status, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("%s (status %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("http %d", e.Status)
}

func (e *APIError) Unwrap() error { return e.cause }

// Client speaks to the control-api over HTTP.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New constructs a client. Pass an empty APIKey for the auth-bootstrap
// case (e.g., login pinging /v1/account itself supplies the key).
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// GetAccount returns GET /v1/account. Used by `auth login` to verify a key.
func (c *Client) GetAccount(ctx context.Context) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodGet, "/v1/account", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRoute is POST /v1/routes. The 201 response includes the route
// token, returned to the caller exactly once by the server.
func (c *Client) CreateRoute(ctx context.Context, req *CreateRouteRequest) (*CreateRouteResponse, error) {
	var out CreateRouteResponse
	if err := c.do(ctx, http.MethodPost, "/v1/routes", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRoutes is GET /v1/routes.
func (c *Client) ListRoutes(ctx context.Context) (*ListRoutesResponse, error) {
	var out ListRoutesResponse
	if err := c.do(ctx, http.MethodGet, "/v1/routes", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRoute is GET /v1/routes/:id.
func (c *Client) GetRoute(ctx context.Context, id string) (*Route, error) {
	var out Route
	if err := c.do(ctx, http.MethodGet, "/v1/routes/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUsage is GET /v1/routes/:id/usage. Used by `route usage` and
// `receipt`; both render against the same response.
func (c *Client) GetUsage(ctx context.Context, id string) (*Usage, error) {
	var out Usage
	if err := c.do(ctx, http.MethodGet, "/v1/routes/"+id+"/usage", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRoute is DELETE /v1/routes/:id. Returns nil on 204, ErrNotFound
// (wrapped in APIError) on 404 (which the server uses for both
// missing-id and already-terminal).
func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/routes/"+id, nil, nil)
}

// do is the single HTTP entry point. It marshals body (if non-nil), adds
// the bearer header (if APIKey is set), executes, and dispatches the
// response into out (if non-nil). 2xx with no body is OK when out is nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		}
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	// Error path: read body (cap to 64KiB so we don't OOM on a misbehaving
	// server) and try to parse the standard {error: ...} shape.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var eb errorBody
	_ = json.Unmarshal(raw, &eb)
	apiErr := &APIError{
		Status:       resp.StatusCode,
		Code:         eb.Error,
		BalanceCents: eb.BalanceCents,
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		apiErr.cause = ErrUnauthorized
	case resp.StatusCode == http.StatusPaymentRequired:
		apiErr.cause = ErrInsufficientCredit
	case resp.StatusCode == http.StatusNotFound:
		apiErr.cause = ErrNotFound
	case resp.StatusCode == http.StatusConflict:
		apiErr.cause = ErrConflict
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		apiErr.cause = ErrBadRequest
	default:
		apiErr.cause = ErrServer
	}
	if eb.Error == "" {
		apiErr.Message = strings.TrimSpace(string(raw))
	}
	return apiErr
}
