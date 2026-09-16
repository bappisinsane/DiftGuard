package hcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

const fixture = `
variable "env" {
  type = string
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "logs" {
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

output "bucket" {
  value = aws_s3_bucket.logs.id
}
`

func TestParseFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantRefs int
	}{
		{"mixed file extracts only resource blocks", fixture, 2},
		{"resource only", "resource \"aws_s3_bucket\" \"a\" {\n  bucket = \"b\"\n}\n", 1},
		{"no resources", "variable \"x\" {\n  default = 1\n}\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.tf")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			refs, err := ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if len(refs) != tt.wantRefs {
				t.Fatalf("got %d refs, want %d: %+v", len(refs), tt.wantRefs, refs)
			}
		})
	}
}

func TestParseFileLabelsAndAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := refs[0]
	if r.Type != "aws_s3_bucket" || r.Name != "logs" {
		t.Fatalf("got %s.%s, want aws_s3_bucket.logs", r.Type, r.Name)
	}

	// Literal attribute must evaluate to its HCL value.
	expr, ok := r.Attributes["bucket"]
	if !ok {
		t.Fatal("missing bucket attribute")
	}
	v, diags := expr.Expr.Value(nil)
	if diags.HasErrors() {
		t.Fatalf("evaluating bucket: %v", diags)
	}
	if v != cty.StringVal("acme-logs") {
		t.Fatalf("bucket = %v, want acme-logs", v.AsString())
	}

	// Nested blocks are not attributes.
	if n := len(refs[1].Attributes); n != 1 {
		t.Fatalf("got %d attributes on versioning block, want 1", n)
	}

	// Attribute ranges must fall inside the block range (needed by the Phase 2 emitter).
	br := r.BlockRange
	if ar := r.AttrRanges["bucket"]; ar.Start.Line <= br.Start.Line || ar.End.Line > br.End.Line {
		t.Fatalf("bucket attr range %v outside block range %v", ar, br)
	}
}

func TestParseFileErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantIn  string
	}{
		{"malformed hcl", "resource \"x\" {\n", "parsing"},
		{"missing file", "", "reading"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.tf")
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := ParseFile(path)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Fatalf("error %q does not contain %q", err, tt.wantIn)
			}
			if !strings.Contains(err.Error(), "main.tf") {
				t.Fatalf("error %q does not name the file", err)
			}
		})
	}
}
