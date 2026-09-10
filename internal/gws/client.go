// Package gws is a minimal Google Workspace API client: only the calls this provider makes,
// and no google.golang.org/api dependency.
//
// The official client library is a fine thing, but it drags in a large generated surface for
// the six calls used here, and its request/response types do not map cleanly onto Terraform's
// null-vs-empty distinction - which matters a great deal for Gmail filter criteria, where an
// absent field and an empty string mean different things to the API.
package gws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	gmailBase = "https://gmail.googleapis.com/gmail/v1"
	tokenURL  = "https://oauth2.googleapis.com/token"
)

// Config carries the credentials. Either RefreshToken (long lived, the normal case) or
// AccessToken (short lived, for CI) must be set.
type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	AccessToken  string
	UserID       string
	Endpoint     string // overridden in tests
	TokenURL     string // overridden in tests
	HTTP         *http.Client
}

// Client is safe for concurrent use: Terraform applies resources in parallel, so the access
// token cache is mutex guarded rather than refreshed per request.
type Client struct {
	cfg Config

	mu      sync.Mutex
	token   string
	expires time.Time
}

// APIError is a non-2xx response. Status is exposed so callers can distinguish a deleted
// resource (404) from a real failure without string matching.
type APIError struct {
	Status  int
	Message string
	Body    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// NotFound reports whether err is a 404, i.e. the resource is gone and Terraform should drop
// it from state rather than fail.
func NotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusNotFound
	}
	return false
}

func New(cfg Config) (*Client, error) {
	if cfg.AccessToken == "" {
		if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RefreshToken == "" {
			return nil, errors.New("need either access_token, or all of client_id, client_secret and refresh_token")
		}
	}
	if cfg.UserID == "" {
		cfg.UserID = "me"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = gmailBase
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = tokenURL
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	c := &Client{cfg: cfg}
	if cfg.AccessToken != "" {
		c.token = cfg.AccessToken
		// A supplied access token has an unknown expiry; treat it as valid and let the API
		// reject it. Refreshing is impossible without a refresh token anyway.
		c.expires = time.Now().Add(365 * 24 * time.Hour)
	}
	return c, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires.Add(-30*time.Second)) {
		return c.token, nil
	}
	form := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"refresh_token": {c.cfg.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", &APIError{Status: resp.StatusCode, Body: string(body), Message: "token refresh failed"}
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("token endpoint returned no access_token")
	}
	c.token = out.AccessToken
	c.expires = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		var wrapped struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &wrapped)
		return &APIError{Status: resp.StatusCode, Message: wrapped.Error.Message, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) user() string { return url.PathEscape(c.cfg.UserID) }
