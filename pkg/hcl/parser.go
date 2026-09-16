// Package hcl parses Terraform HCL files into structured resource references.
package hcl

import (
	"fmt"
	"os"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ResourceRef is one `resource "<type>" "<name>"` block from a .tf file.
type ResourceRef struct {
	Type string // e.g. "aws_s3_bucket"
	Name string // e.g. "logs"
	File string // source file path (for targeted patch emission)
	// Attributes maps attribute name -> its expression node.
	Attributes map[string]*hclsyntax.Attribute
	// Nested maps nested block type -> its block node (e.g. "versioning_configuration").
	Nested map[string]*hclsyntax.Block
	// AttrRanges maps attribute name -> source range, for patch emission.
	AttrRanges map[string]hcl.Range
	// BlockRange is the source range of the whole block.
	BlockRange hcl.Range
}

// ParseFile parses the HCL file at path and returns its resource blocks.
func ParseFile(path string) ([]ResourceRef, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	file, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %w", path, diags)
	}

	body := file.Body.(*hclsyntax.Body) // hclsyntax.ParseConfig always returns a *hclsyntax.Body
	var refs []ResourceRef
	for _, block := range body.Blocks {
		if block.Type != "resource" || len(block.Labels) != 2 {
			continue
		}
		nested := make(map[string]*hclsyntax.Block, len(block.Body.Blocks))
		for _, nb := range block.Body.Blocks {
			nested[nb.Type] = nb
		}
		refs = append(refs, ResourceRef{
			Type:       block.Labels[0],
			Name:       block.Labels[1],
			File:       path,
			Attributes: block.Body.Attributes,
			Nested:     nested,
			AttrRanges: attrRanges(block.Body.Attributes),
			BlockRange: block.Range(),
		})
	}
	return refs, nil
}

func attrRanges(attrs map[string]*hclsyntax.Attribute) map[string]hcl.Range {
	r := make(map[string]hcl.Range, len(attrs))
	for name, attr := range attrs {
		r[name] = attr.NameRange
	}
	return r
}

// BucketName returns the effective bucket name: the `bucket` attribute when it
// is a literal, else the HCL label (Terraform derives a name from the label).
// ponytail: variable references and bucket_prefix are not resolved; they
// return the label until Phase 3's full evaluator lands.
func BucketName(r ResourceRef) string {
	if s := AttrString(r, "bucket"); s != "" {
		return s
	}
	return r.Name
}

// AttrString returns the literal string value of a top-level attribute, or ""
// if absent or non-literal (references/expressions defer to the full evaluator).
func AttrString(r ResourceRef, name string) string {
	return attrString(r.Attributes, name)
}

// NestedAttrString returns the literal string value of an attribute inside a
// nested block (e.g. "versioning_configuration" + "status"), or "".
func NestedAttrString(r ResourceRef, nested, name string) string {
	nb, ok := r.Nested[nested]
	if !ok {
		return ""
	}
	return attrString(nb.Body.Attributes, name)
}

func attrString(attrs map[string]*hclsyntax.Attribute, name string) string {
	a, ok := attrs[name]
	if !ok {
		return ""
	}
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() || !v.Type().Equals(cty.String) {
		return ""
	}
	return v.AsString()
}
