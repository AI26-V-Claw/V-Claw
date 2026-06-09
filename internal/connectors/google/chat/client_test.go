package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.URL.Path = singleJoiningSlash(t.target.Path, req.URL.Path)
	clone.URL.RawQuery = req.URL.RawQuery
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func TestCreateSpaceOmitsDisplayNameForDirectMessage(t *testing.T) {
	var captured map[string]any
	var captureErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			captureErr = err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			captureErr = err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"spaces/abc","spaceType":"DIRECT_MESSAGE"}`))
	}))
	defer server.Close()

	targetURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	client := &http.Client{
		Transport: rewriteTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}

	space, err := CreateSpace(context.Background(), client, CreateSpaceInput{
		DisplayName: "Should be omitted",
		SpaceType:   "DIRECT_MESSAGE",
		MemberUsers: []string{"users/alice@example.com"},
		RequestID:   "req_1",
	})
	if err != nil {
		t.Fatalf("CreateSpace() error = %v", err)
	}
	if captureErr != nil {
		t.Fatalf("capture request body: %v", captureErr)
	}
	if space.SpaceType != "DIRECT_MESSAGE" {
		t.Fatalf("expected direct message space, got %#v", space)
	}

	spacePayload, ok := captured["space"].(map[string]any)
	if !ok {
		t.Fatalf("expected space payload, got %#v", captured)
	}
	if _, ok := spacePayload["displayName"]; ok {
		t.Fatalf("expected displayName to be omitted, got %#v", spacePayload)
	}
	if got := spacePayload["spaceType"]; got != "DIRECT_MESSAGE" {
		t.Fatalf("expected direct message spaceType, got %#v", got)
	}
}
