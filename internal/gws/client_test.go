package gws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fake serves the handful of Gmail endpoints the provider uses, plus the token endpoint.
func fake(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "expires_in": 3600})
			return
		}
		h(w, r)
	}))
	c, err := New(Config{
		ClientID: "id", ClientSecret: "secret", RefreshToken: "rt",
		Endpoint: srv.URL, TokenURL: srv.URL + "/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestRefreshTokenIsExchangedAndReused(t *testing.T) {
	tokenCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "expires_in": 3600})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("Authorization = %q, want Bearer at-1", got)
		}
		_ = json.NewEncoder(w).Encode(Label{ID: "Label_1", Name: "x"})
	}))
	defer srv.Close()
	c, err := New(Config{ClientID: "id", ClientSecret: "s", RefreshToken: "rt", Endpoint: srv.URL, TokenURL: srv.URL + "/token"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := c.GetLabel(context.Background(), "Label_1"); err != nil {
			t.Fatal(err)
		}
	}
	// A token good for an hour must not be re-fetched per request: a whole-filter-set apply
	// would otherwise hammer the token endpoint and get itself rate limited.
	if tokenCalls != 1 {
		t.Errorf("token endpoint called %d times, want 1", tokenCalls)
	}
}

func TestNotFoundIsDistinguishable(t *testing.T) {
	c, srv := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Not Found"}}`))
	})
	defer srv.Close()

	_, err := c.GetFilter(context.Background(), "gone")
	if err == nil {
		t.Fatal("want error")
	}
	// Read() relies on this to drop a resource from state instead of failing the run.
	if !NotFound(err) {
		t.Errorf("NotFound(%v) = false, want true", err)
	}
}

func TestOtherErrorsAreNotNotFound(t *testing.T) {
	c, srv := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient Permission"}}`))
	})
	defer srv.Close()

	_, err := c.GetFilter(context.Background(), "x")
	if NotFound(err) {
		t.Error("403 must not be treated as a missing resource")
	}
	if got := err.Error(); got != "Insufficient Permission (HTTP 403)" {
		t.Errorf("Error() = %q", got)
	}
}

func TestEmptyCriteriaFieldsAreOmitted(t *testing.T) {
	var body map[string]any
	c, srv := fake(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(Filter{ID: "f1"})
	})
	defer srv.Close()

	_, err := c.CreateFilter(context.Background(), Filter{
		Criteria: &FilterCriteria{Query: "from:a.com"},
		Action:   &FilterAction{AddLabelIDs: []string{"Label_1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	crit, _ := body["criteria"].(map[string]any)
	// Sending "from": "" would create a filter matching nothing, silently. The zero value of
	// an unset criterion must not reach the wire at all.
	for _, key := range []string{"from", "to", "subject", "negatedQuery", "sizeComparison", "hasAttachment", "size"} {
		if _, present := crit[key]; present {
			t.Errorf("criteria contains %q for an unset field: %v", key, crit)
		}
	}
	if crit["query"] != "from:a.com" {
		t.Errorf("query = %v", crit["query"])
	}
}

func TestRetryBacksOffOnRateLimitThenSucceeds(t *testing.T) {
	calls := 0
	c, srv := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate Limit Exceeded"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(Filter{ID: "f-ok"})
	})
	defer srv.Close()

	var out *Filter
	err := Retry(context.Background(), 4, func() error {
		var e error
		out, e = c.CreateFilter(context.Background(), Filter{Criteria: &FilterCriteria{Query: "x"}})
		return e
	})
	if err != nil {
		t.Fatalf("Retry returned %v after %d calls", err, calls)
	}
	if out.ID != "f-ok" {
		t.Errorf("id = %q", out.ID)
	}
}

func TestRetryDoesNotRetryClientErrors(t *testing.T) {
	calls := 0
	c, srv := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Filter already exists"}}`))
	})
	defer srv.Close()

	err := Retry(context.Background(), 4, func() error {
		_, e := c.CreateFilter(context.Background(), Filter{Criteria: &FilterCriteria{Query: "x"}})
		return e
	})
	if err == nil {
		t.Fatal("want error")
	}
	// Retrying a 400 just wastes the apply's time and multiplies the error.
	if calls != 1 {
		t.Errorf("made %d calls, want 1", calls)
	}
}

func TestNewRejectsIncompleteCredentials(t *testing.T) {
	if _, err := New(Config{ClientID: "id"}); err == nil {
		t.Error("want an error when only client_id is set")
	}
	if _, err := New(Config{AccessToken: "at"}); err != nil {
		t.Errorf("an access token alone is enough: %v", err)
	}
}

func TestUserIDIsEscapedIntoThePath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": []Label{}})
	}))
	defer srv.Close()
	c, err := New(Config{AccessToken: "at", UserID: "a b@example.com", Endpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListLabels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "/users/a%20b@example.com/labels"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}
