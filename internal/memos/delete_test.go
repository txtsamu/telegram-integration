package memos

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteMemo(t *testing.T) {
	var method, path, token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, token = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"memos/abc"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL).WithAccessToken("secret-token")
	if err := client.DeleteMemo(context.Background(), "memos/abc"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/api/memos/abc" {
		t.Fatalf("%s %s", method, path)
	}
	if token != "Bearer secret-token" {
		t.Fatalf("token %q was not sent", token)
	}
}

func TestDeleteMemoUsesLegacyPath(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.Path == "/api/instance/profile" {
			// Force the legacy namespace, as Memos 0.31 does.
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v1/instance/profile" {
			fmt.Fprint(w, `{"version":"0.31.0"}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.GetInstanceProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.APIPrefix() != "/api/v1" {
		t.Fatalf("prefix %q", client.APIPrefix())
	}
	if err := client.DeleteMemo(context.Background(), "memos/abc"); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/memos/abc" {
		t.Fatalf("legacy path %q", path)
	}
}

func TestDeleteMemoReportsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"unauthorized"}`)
	}))
	defer server.Close()

	err := NewClient(server.URL).DeleteMemo(context.Background(), "memos/abc")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusForbidden || apiErr.Message != "unauthorized" {
		t.Fatalf("error %v", err)
	}
}

func TestDeleteMemoRejectsInvalidName(t *testing.T) {
	if err := NewClient("http://127.0.0.1:1").DeleteMemo(context.Background(), "tasks/abc"); err == nil {
		t.Fatal("accepted a name that is not a memo")
	}
}
