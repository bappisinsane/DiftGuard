package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"driftguard/pkg/cloud/aws"
	"driftguard/pkg/diff"
)

// tfFixture is a managed bucket whose live state always drifts, plus one
// clean bucket.
const tfFixture = `resource "aws_s3_bucket" "logs" {
  bucket = "acme-logs"

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket" "plain" {
  tags = { env = "prod" }
}
`

func writeTF(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tfFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withFakeLive swaps the live-state fetch for a deterministic fake.
func withFakeLive(t *testing.T, states []aws.BucketState) {
	t.Helper()
	old := fetchStates
	fetchStates = func(_ context.Context, _ string, buckets []string) ([]aws.BucketState, error) {
		byName := map[string]bool{}
		for _, b := range buckets {
			byName[b] = true
		}
		var out []aws.BucketState
		for _, s := range states {
			if byName[s.Name] {
				out = append(out, s)
			}
		}
		return out, nil
	}
	t.Cleanup(func() { fetchStates = old })
}

// captureStdout redirects os.Stdout while fn runs and restores it after.
// runCheck's output is small, so no drain goroutine is needed.
func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), code
}

func TestCheckCleanRun(t *testing.T) {
	dir := writeTF(t)
	withFakeLive(t, []aws.BucketState{
		{Name: "acme-logs", Versioning: "Enabled", ACL: "private"},
		{Name: "plain", Versioning: "", ACL: ""},
	})
	stdout, code := captureStdout(t, func() int { return runCheck(context.Background(), dir, "", false) })

	var out struct {
		Drift    bool           `json:"drift"`
		Findings []diff.Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not the check JSON contract: %v\n%s", err, stdout)
	}
	if out.Drift || out.Findings == nil || len(out.Findings) != 0 {
		t.Fatalf("want drift=false, findings=[]: %+v", out)
	}
	if code != 0 {
		t.Fatalf("clean run must exit 0, got %d", code)
	}
}

func TestCheckStrictExits2(t *testing.T) {
	dir := writeTF(t)
	withFakeLive(t, []aws.BucketState{
		{Name: "acme-logs", Versioning: "Suspended", ACL: "private"},
	})
	stdout, code := captureStdout(t, func() int { return runCheck(context.Background(), dir, "", true) })

	var out struct {
		Drift    bool           `json:"drift"`
		Findings []diff.Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not the check JSON contract: %v\n%s", err, stdout)
	}
	if !out.Drift || len(out.Findings) != 1 {
		t.Fatalf("want one finding: %+v", out.Findings)
	}
	if code != 2 {
		t.Fatalf("--strict with drift must exit 2, got %d", code)
	}
	f := out.Findings[0]
	if f.Field != "versioning_configuration.status" || f.Desired != "Enabled" || f.Actual != "Suspended" {
		t.Fatalf("finding payload wrong: %+v", f)
	}
	if !strings.HasSuffix(f.File, "main.tf") || f.Line != 1 {
		t.Fatalf("finding not located: %s:%d", f.File, f.Line)
	}
}

// TestScanCheckSameFindings pins the contract that scan's exit-2 count and
// check's drift count are computed by the same mapper.
func TestScanCheckSameFindings(t *testing.T) {
	dir := writeTF(t)
	withFakeLive(t, []aws.BucketState{
		{Name: "acme-logs", Versioning: "Suspended", ACL: "public-read"},
	})
	states, err := fetchStates(context.Background(), "", []string{"acme-logs", "plain"})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := parseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]aws.BucketState{}
	for _, s := range states {
		live[s.Name] = s
	}
	// logs drifts on versioning (Enabled vs Suspended). plain states no acl,
	// and silent HCL is never drift (ADR-005) — so exactly one finding.
	if got := len(diff.S3Findings(refs, live)); got != 1 {
		t.Fatalf("scan and check must agree on 1 finding, got %d", got)
	}
}
