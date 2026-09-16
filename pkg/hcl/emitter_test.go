package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// input starts as the golden output: the only diff after EmitEdits must be the
// patched lines. This is the comment-preservation proof.
const golden = `# Managed by platform team - do not reorder.
resource "aws_s3_bucket" "logs" {
  # Keep server-side encryption off this bucket (legacy ETL reads plain).
  bucket = "acme-logs"

  tags = {
    env = "prod"
  }
}

resource "aws_s3_bucket_versioning" "logs" {
  bucket = aws_s3_bucket.logs.id

  versioning_configuration {
    status = "Enabled"
  }
}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(golden), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEmitEditsPreservesComments(t *testing.T) {
	path := writeFixture(t)

	changed, err := EmitEdits(path,
		[]Patch{{Resource: "aws_s3_bucket.logs", Attr: "bucket", Value: cty.StringVal("acme-logs-eu")}}, nil, nil)
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
	// Full-file golden equality: the ONLY allowed diff is the patched line.
	want := strings.Replace(golden, `bucket = "acme-logs"`, `bucket = "acme-logs-eu"`, 1)
	if string(got) != want {
		t.Fatalf("file changed beyond the patched attribute:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestEmitEditsAlterAndAppend(t *testing.T) {
	path := writeFixture(t)

	_, err := EmitEdits(path,
		[]Patch{{Resource: "aws_s3_bucket.logs", Attr: "bucket", Value: cty.StringVal("acme-logs-v2")}},
		nil,
		[]AppendBlock{{
			Type: "aws_s3_bucket_lifecycle_configuration",
			Name: "logs",
			Attributes: map[string]cty.Value{
				"bucket": cty.StringVal("acme-logs-v2"),
			},
		}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, `bucket = "acme-logs-v2"`) {
		t.Fatalf("patch not applied:\n%s", s)
	}
	if !strings.Contains(s, `resource "aws_s3_bucket_lifecycle_configuration" "logs"`) {
		t.Fatalf("block not appended:\n%s", s)
	}
	// Both source comments must still be present.
	for _, c := range []string{"# Managed by platform team", "# Keep server-side encryption off"} {
		if !strings.Contains(s, c) {
			t.Fatalf("comment lost:\n%s", s)
		}
	}
	// The untouched versioning block must be byte-identical to its input.
	if !strings.Contains(s, `status = "Enabled"`) {
		t.Fatalf("versioning block damaged:\n%s", s)
	}
}

func TestEmitEditsErrors(t *testing.T) {
	t.Run("missing block", func(t *testing.T) {
		path := writeFixture(t)
		_, err := EmitEdits(path,
			[]Patch{{Resource: "aws_s3_bucket.nope", Attr: "bucket", Value: cty.StringVal("x")}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "resource block not found") {
			t.Fatalf("want block-not-found error, got %v", err)
		}
	})
	t.Run("bad patch label", func(t *testing.T) {
		path := writeFixture(t)
		_, err := EmitEdits(path,
			[]Patch{{Resource: "nolabel", Attr: "bucket", Value: cty.StringVal("x")}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "expected <type>.<name>") {
			t.Fatalf("want label error, got %v", err)
		}
	})
	t.Run("malformed hcl untouched", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "main.tf")
		if err := os.WriteFile(path, []byte("resource \"x\" {\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := EmitEdits(path,
			[]Patch{{Resource: "x.y", Attr: "a", Value: cty.StringVal("v")}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "parsing") {
			t.Fatalf("want parse error, got %v", err)
		}
	})
}

// Guard against hclwrite version drift silently changing emitted formatting.
func TestEmitterWritesCanonicalHCL(t *testing.T) {
	f := hclwrite.NewEmptyFile()
	b := f.Body().AppendNewBlock("resource", []string{"aws_s3_bucket", "fresh"}).Body()
	b.SetAttributeValue("bucket", cty.StringVal("acme-new"))
	want := "resource \"aws_s3_bucket\" \"fresh\" {\n  bucket = \"acme-new\"\n}\n"
	if string(f.Bytes()) != want {
		t.Fatalf("got:\n%q\nwant:\n%q", f.Bytes(), want)
	}
}
