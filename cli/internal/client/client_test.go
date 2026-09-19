package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoSendsAuthAndParsesEnvelope(t *testing.T) {
	var gotAuth, gotOrg, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotOrg, gotCT = r.Header.Get("Authorization"), r.Header.Get("X-DBVault-Org"), r.Header.Get("Content-Type")
		if r.URL.Path != "/api/backups" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": Queued{BackupID: "b-" + body["database_id"], JobID: "j1", Status: "queued"}})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "dbv_token", "acme")
	var q Queued
	if err := c.Post(context.Background(), "/backups", map[string]string{"database_id": "d1"}, &q); err != nil {
		t.Fatal(err)
	}
	if q.BackupID != "b-d1" || q.JobID != "j1" {
		t.Fatalf("unexpected response %+v", q)
	}
	if gotAuth != "Bearer dbv_token" || gotOrg != "acme" || gotCT != "application/json" {
		t.Fatalf("headers: auth=%q org=%q ct=%q", gotAuth, gotOrg, gotCT)
	}
}

func TestDoReturnsAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"A backup of this database is already queued or running.","request_id":"abc"}}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "t", "").Post(context.Background(), "/backups", nil, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != "conflict" || apiErr.RequestID != "abc" {
		t.Fatalf("unexpected error %#v", err)
	}
}

func TestDoHandlesNonJSONErrorsAndNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "")
	if err := c.Delete(context.Background(), "/databases/x"); err != nil {
		t.Fatalf("204 should succeed: %v", err)
	}
	var apiErr *Error
	if err := c.Get(context.Background(), "/me", &Me{}); !errors.As(err, &apiErr) || apiErr.Status != 502 {
		t.Fatalf("expected 502 error, got %v", err)
	}
}

func TestUnreachableServer(t *testing.T) {
	err := New("http://127.0.0.1:1", "t", "").Get(context.Background(), "/me", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}
