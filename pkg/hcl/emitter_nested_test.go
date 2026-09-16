package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

const nestedFixture = `# Managed by platform team - do not reorder.
resource "aws_s3_bucket" "logs" {
  # Keep server-side encryption off this bucket (legacy ETL reads plain).
  bucket = "acme-logs"

  versioning_configuration {
    status = "Enabled"
  }
}
`

func TestEmitEditsNestedPatchExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(nestedFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := EmitEdits(path, nil,
		[]NestedPatch{{Resource: "aws_s3_bucket.logs", Nested: "versioning_configuration", Attr: "status", Value: cty.StringVal("Suspended")}},
		nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, `status = "Suspended"`) {
		t.Fatalf("nested patch not applied:\n%s", s)
	}
	if strings.Contains(s, `status = "Enabled"`) {
		t.Fatalf("old value still present:\n%s", s)
	}
	for _, c := range []string{"# Managed by platform team", "# Keep server-side encryption off"} {
		if !strings.Contains(s, c) {
			t.Fatalf("comment lost:\n%s", s)
		}
	}
	// Full-file equality: only the status line may differ from the fixture.
	want := strings.Replace(nestedFixture, `status = "Enabled"`, `status = "Suspended"`, 1)
	if s != want {
		t.Fatalf("file changed beyond the nested patch:\n--- got ---\n%s\n--- want ---\n%s", s, want)
	}
}

func TestEmitEditsNestedPatchCreated(t *testing.T) {
	src := `# Header comment.
resource "aws_s3_bucket" "plain" {
  bucket = "acme-plain"
}
`
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := EmitEdits(path, nil,
		[]NestedPatch{{Resource: "aws_s3_bucket.plain", Nested: "versioning_configuration", Attr: "status", Value: cty.StringVal("Enabled")}},
		nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"# Header comment.",
		"bucket = \"acme-plain\"",
		"versioning_configuration {",
		"status = \"Enabled\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestBucketNamePrefersAttribute(t *testing.T) {
	refs := parseFixture(t, remediateLikeFixture)
	if got := BucketName(refs[0]); got != "acme-logs" {
		t.Fatalf("got %q, want acme-logs", got)
	}
	if got := BucketName(refs[1]); got != "plain" {
		t.Fatalf("label fallback failed: got %q", got)
	}
}

const remediateLikeFixture = `resource "aws_s3_bucket" "logs" {
  bucket = "acme-logs"
}
resource "aws_s3_bucket" "plain" {
  tags = { env = "prod" }
}
`

func parseFixture(t *testing.T, content string) []ResourceRef {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}
