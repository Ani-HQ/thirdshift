package cairo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const accessApplicationEvent = "access_application"

// Tracker posts product events to Cairo's /v2/track ingest.
// Missing Host or WriteKey disables tracking (no-op).
type Tracker struct {
	Host      string
	WriteKey  string
	Namespace string
	Client    *http.Client
	Logger    *slog.Logger
}

type trackRequest struct {
	Type       string         `json:"type"`
	Event      string         `json:"event"`
	UserID     string         `json:"userId,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"`
}

// Enabled reports whether the tracker can reach Cairo.
func (t *Tracker) Enabled() bool {
	return t != nil && strings.TrimSpace(t.Host) != "" && strings.TrimSpace(t.WriteKey) != ""
}

// TrackAccessApplication notifies Dash (via Cairo rules) about a waitlist submit.
// Failures are logged only; callers should treat this as fire-and-forget.
func (t *Tracker) TrackAccessApplication(ctx context.Context, email string, properties map[string]any) error {
	return t.Track(ctx, accessApplicationEvent, email, properties)
}

// Track posts a single event. Returns nil when the tracker is disabled.
func (t *Tracker) Track(ctx context.Context, event, userID string, properties map[string]any) error {
	if !t.Enabled() {
		return nil
	}
	event = strings.TrimSpace(event)
	if event == "" {
		return fmt.Errorf("cairo track: event is required")
	}
	body := trackRequest{
		Type:       "track",
		Event:      event,
		UserID:     strings.TrimSpace(userID),
		Namespace:  strings.TrimSpace(t.Namespace),
		Properties: properties,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cairo track: encode: %w", err)
	}
	url := strings.TrimRight(t.Host, "/") + "/v2/track"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("cairo track: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Write-Key", t.WriteKey)

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cairo track: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("cairo track: unexpected status %d", res.StatusCode)
	}
	return nil
}

// NotifyAccessApplicationAsync tracks in a background goroutine and never
// blocks the waitlist response path.
func (t *Tracker) NotifyAccessApplicationAsync(email string, properties map[string]any) {
	if !t.Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := t.TrackAccessApplication(ctx, email, properties); err != nil {
			logger := t.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Warn("cairo access_application track failed", "error", err)
		}
	}()
}
