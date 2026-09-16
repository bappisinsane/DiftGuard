package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"driftguard/pkg/cloud/aws"
	"driftguard/pkg/diff"
)

// copyTF copies the shared fixture into a temp dir for each loop test.
func copyTF(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tfFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// captureStderr redirects os.Stderr and returns a restore func yielding
// everything written while redirected.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	return func() string {
		w.Close()
		os.Stderr = old
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
}

// waitFor polls cond until it holds or the timeout lapses.
func waitFor(timeout time.Duration, cond func() bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestDaemonAlertSequence drives the real loop with tiny intervals: tick 1 has
// drift (alert), tick 2 resolves it (resolution alert), tick 3 is unchanged
// (silence). Exactly the changes alert; steady state stays quiet.
func TestDaemonAlertSequence(t *testing.T) {
	dir := copyTF(t)

	states := []aws.BucketState{
		{Name: "acme-logs", Versioning: "Suspended", ACL: "private"}, // drifts vs HCL "Enabled"
		{Name: "plain"},
	}
	withFakeLive(t, states)

	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("webhook payload not Slack JSON: %v", err)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		mu.Lock()
		got = append(got, payload.Text)
		mu.Unlock()
	}))
	defer srv.Close()

	interval := 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, dir, "", interval, srv.URL) }()

	waitFor(2*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 1 })
	mu.Lock()
	if len(got) != 1 || !strings.Contains(got[0], "1 drift finding(s)") {
		mu.Unlock()
		t.Fatalf("tick 1: want exactly one drift alert, got %v", got)
	}
	if !strings.Contains(got[0], "versioning_configuration.status") {
		mu.Unlock()
		t.Fatalf("alert missing the finding: %q", got[0])
	}
	mu.Unlock()

	// Resolve the drift: live now matches HCL's desired state.
	mu.Lock()
	states[0] = aws.BucketState{Name: "acme-logs", Versioning: "Enabled", ACL: "private"}
	mu.Unlock()

	waitFor(2*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 2 })
	mu.Lock()
	if len(got) != 2 || !strings.Contains(got[1], "drift resolved") {
		mu.Unlock()
		t.Fatalf("tick 2: want exactly one resolution alert, got %v", got)
	}
	mu.Unlock()

	// Unchanged ticks must stay silent.
	time.Sleep(4 * interval)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("unchanged ticks must not alert, got %d alerts: %v", len(got), got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runDaemon returned error: %v", err)
	}
}

// TestDaemonLogOnlyWhenNoWebhook pins the fallback: empty --webhook-url logs
// alerts to stderr instead of posting anywhere.
func TestDaemonLogOnlyWhenNoWebhook(t *testing.T) {
	dir := copyTF(t)
	withFakeLive(t, []aws.BucketState{{Name: "acme-logs", Versioning: "Suspended"}})

	restore := captureStderr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, dir, "", 10*time.Millisecond, "") }()
	time.Sleep(40 * time.Millisecond) // let a few ticks pass
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runDaemon: %v", err)
	}
	logged := restore()
	if !strings.Contains(logged, "alert: :rotating_light:") {
		t.Fatalf("log-only mode did not emit the alert to stderr: %q", logged)
	}
}

// TestDaemonTransientErrorRetries pins that a failing check tick doesn't kill
// the loop: the live fetch errors for a while, then recovers and alerts.
func TestDaemonTransientErrorRetries(t *testing.T) {
	dir := copyTF(t)
	var mu sync.Mutex
	fail := true
	old := fetchStates
	fetchStates = func(_ context.Context, _ string, buckets []string) ([]aws.BucketState, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, context.DeadlineExceeded
		}
		// Drift after recovery so the changed finding set alerts (nil == nil
		// would stay silent, which is correct steady-state behavior).
		return []aws.BucketState{{Name: "acme-logs", Versioning: "Suspended"}}, nil
	}
	t.Cleanup(func() { fetchStates = old })

	restore := captureStderr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, dir, "", 10*time.Millisecond, "") }()
	time.Sleep(40 * time.Millisecond) // a few failing ticks
	mu.Lock()
	fail = false // recover
	mu.Unlock()
	time.Sleep(40 * time.Millisecond) // recovery tick
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("transient error must not stop the daemon: %v", err)
	}
	logged := restore()
	if !strings.Contains(logged, "check failed, retrying next tick") {
		t.Fatalf("failure was not logged: %q", logged)
	}
	if !strings.Contains(logged, "alert: :rotating_light:") {
		// nil -> findings after recovery is a change, so the drift alert fires.
		t.Fatalf("recovery did not resume alerting: %q", logged)
	}
}

func TestAlertText(t *testing.T) {
	f := []diff.Finding{{
		ResourceType: "aws_s3_bucket", ResourceName: "logs",
		Field: "acl", Desired: "private", Actual: "public-read", File: "infra/main.tf", Line: 12,
	}}
	got := alertText(f, "infra")
	for _, want := range []string{"1 drift finding(s) in infra", "aws_s3_bucket.logs.acl", "main.tf:12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("alert missing %q: %q", want, got)
		}
	}
	if alertText(nil, "infra") != ":white_check_mark: DriftGuard: drift resolved in infra" {
		t.Fatalf("empty findings must render the resolution message: %q", alertText(nil, "infra"))
	}
}

func TestPostSlack(t *testing.T) {
	var mu sync.Mutex
	var text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		text = payload.Text
		mu.Unlock()
	}))
	defer srv.Close()
	if err := postSlack(context.Background(), srv.URL, "hi"); err != nil {
		t.Fatalf("postSlack: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if text != "hi" {
		t.Fatalf("webhook got %q", text)
	}
}
