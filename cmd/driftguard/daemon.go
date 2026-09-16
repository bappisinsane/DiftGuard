package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"driftguard/pkg/diff"
)

// httpClient bounds webhook delivery so a hung endpoint can't stall the loop.
var httpClient = &http.Client{Timeout: 10 * time.Second}

func daemonCmd() *cobra.Command {
	var dir, region, webhook string
	var every time.Duration
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Re-run drift checks on an interval and alert on changes via Slack webhook",
		RunE: func(cmd *cobra.Command, args []string) error {
			if every <= 0 {
				return fmt.Errorf("--interval must be positive, got %s", every)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runDaemon(ctx, dir, region, every, webhook)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "directory containing .tf files")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to provider chain)")
	cmd.Flags().StringVar(&webhook, "webhook-url", "", "Slack incoming webhook URL (empty logs alerts to stderr)")
	cmd.Flags().DurationVar(&every, "interval", 5*time.Minute, "time between drift checks")
	return cmd
}

// runDaemon loops until ctx is canceled: check, alert when the finding set
// changes, sleep. Transient errors (AWS outages, mid-edit HCL) are logged and
// retried next tick; only ctx cancellation stops the daemon.
func runDaemon(ctx context.Context, dir, region string, every time.Duration, webhook string) error {
	fmt.Fprintf(os.Stderr, "driftguard daemon: watching %s every %s\n", dir, every)
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	var prev []diff.Finding
	for {
		findings, err := scanAndCheck(ctx, dir, region)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "check failed, retrying next tick: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "check: %d finding(s)\n", len(findings))
			if !reflect.DeepEqual(findings, prev) { // alert on change only, not every tick
				if err := postSlack(ctx, webhook, alertText(findings, dir)); err != nil {
					fmt.Fprintf(os.Stderr, "alert failed: %v\n", err)
				}
				prev = findings
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// alertText renders the Slack message; empty findings means the drift->clean
// transition (prev must have been non-empty for the caller to reach here).
func alertText(findings []diff.Finding, dir string) string {
	if len(findings) == 0 {
		return fmt.Sprintf(":white_check_mark: DriftGuard: drift resolved in %s", dir)
	}
	var b strings.Builder
	fmt.Fprintf(&b, ":rotating_light: DriftGuard: %d drift finding(s) in %s", len(findings), dir)
	for _, f := range findings {
		fmt.Fprintf(&b, "\n• %s (%s:%d)", f.String(), filepath.Base(f.File), f.Line)
	}
	return b.String()
}

// postSlack posts a chat message to a Slack incoming webhook. Empty URL logs
// to stderr instead (log-only mode). ponytail: the {"text"} payload also
// satisfies Teams connectors; PagerDuty's Events API envelope lands when needed.
func postSlack(ctx context.Context, url, text string) error {
	if url == "" {
		fmt.Fprintf(os.Stderr, "alert: %s\n", text)
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"text": text}) // map[string]string marshal never fails
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
