package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestVerifySendsBearerTokenWithoutRequiringRealNetwork(t *testing.T) {
	var sawAuth bool
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got, want := req.URL.Path, "/v1/me"; got != want {
			t.Fatalf("expected path %q, got %q", want, got)
		}
		if got, want := req.Header.Get("Authorization"), "Bearer arun_usr_secret"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		sawAuth = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	err := Verify(context.Background(), client, &Config{
		APIURL: "http://127.0.0.1:2009",
		APIKey: "arun_usr_secret",
	})

	if err != nil {
		t.Fatalf("expected verification to pass, got %v", err)
	}
	if !sawAuth {
		t.Fatal("expected request to be made")
	}
}

func TestVerifyRejectsUnauthorized(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	err := Verify(context.Background(), client, &Config{
		APIURL: "http://127.0.0.1:2009",
		APIKey: "arun_usr_secret",
	})

	if err == nil || !strings.Contains(err.Error(), "credential rejected") {
		t.Fatalf("expected rejected credential error, got %v", err)
	}
	if strings.Contains(err.Error(), "arun_usr_secret") {
		t.Fatalf("error leaked token: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
