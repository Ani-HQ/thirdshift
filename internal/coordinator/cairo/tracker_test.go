package cairo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrackerDisabled(t *testing.T) {
	var tracker *Tracker
	if tracker.Enabled() {
		t.Fatal("nil tracker should be disabled")
	}
	if err := (&Tracker{}).Track(context.Background(), "access_application", "a@b.com", nil); err != nil {
		t.Fatalf("disabled tracker should no-op: %v", err)
	}
}

func TestTrackAccessApplication(t *testing.T) {
	var gotPath atomic.Value
	var gotKey atomic.Value
	var gotBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotKey.Store(r.Header.Get("X-Write-Key"))
		body, _ := io.ReadAll(r.Body)
		gotBody.Store(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	tracker := &Tracker{
		Host:      server.URL,
		WriteKey:  "ck_test",
		Namespace: "default",
		Client:    server.Client(),
	}
	if err := tracker.TrackAccessApplication(context.Background(), "dev@example.com", map[string]any{
		"use_case": "Prototype",
		"is_new":   true,
	}); err != nil {
		t.Fatalf("track: %v", err)
	}
	if gotPath.Load() != "/v2/track" {
		t.Fatalf("path = %v", gotPath.Load())
	}
	if gotKey.Load() != "ck_test" {
		t.Fatalf("write key = %v", gotKey.Load())
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody.Load().([]byte), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["type"] != "track" || payload["event"] != "access_application" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["userId"] != "dev@example.com" || payload["namespace"] != "default" {
		t.Fatalf("identity fields = %#v", payload)
	}
	props, _ := payload["properties"].(map[string]any)
	if props["use_case"] != "Prototype" || props["is_new"] != true {
		t.Fatalf("properties = %#v", props)
	}
}

func TestTrackUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	tracker := &Tracker{Host: server.URL, WriteKey: "ck_test", Client: server.Client()}
	if err := tracker.Track(context.Background(), "access_application", "a@b.com", nil); err == nil {
		t.Fatal("expected error for non-2xx")
	}
}

func TestNotifyAccessApplicationAsync(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
		close(done)
	}))
	defer server.Close()
	tracker := &Tracker{Host: server.URL, WriteKey: "ck_test", Client: server.Client()}
	tracker.NotifyAccessApplicationAsync("dev@example.com", map[string]any{"is_new": true})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("async track did not run")
	}
}
