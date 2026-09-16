package hcl

import (
	"fmt"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Patch sets a literal attribute on an existing resource block.
type Patch struct {
	Resource string // "<type>.<name>", e.g. "aws_s3_bucket.logs"
	Attr     string // attribute to set, e.g. "bucket"
	Value    cty.Value
}

// NestedPatch sets a literal attribute inside a nested block, creating the
// nested block if it does not exist (e.g. versioning_configuration.status).
type NestedPatch struct {
	Resource string // "<type>.<name>"
	Nested   string // nested block type, e.g. "versioning_configuration"
	Attr     string // e.g. "status"
	Value    cty.Value
}

// AppendBlock describes a new resource block to add to the file.
type AppendBlock struct {
	Type       string // Terraform resource type, e.g. "aws_s3_bucket"
	Name       string // Terraform resource name (label), e.g. "logs"
	Attributes map[string]cty.Value
}

// EmitEdits applies patches to one .tf file in place via hclwrite tokens.
// Untouched lines, comments, and blank lines are preserved byte-for-byte.
func EmitEdits(path string, patches []Patch, nested []NestedPatch, appends []AppendBlock) (bool, error) {
	if len(patches)+len(nested)+len(appends) == 0 {
		return false, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	f, diags := hclwrite.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return false, fmt.Errorf("parsing %s: %w", path, diags)
	}

	// Sort all op groups for byte-identical output on identical inputs.
	sortPatches(patches)
	sortNested(nested)
	sortAppends(appends)

	for _, p := range patches {
		b, err := findBlock(f, p.Resource, path)
		if err != nil {
			return false, err
		}
		b.Body().SetAttributeValue(p.Attr, p.Value)
	}

	for _, np := range nested {
		b, err := findBlock(f, np.Resource, path)
		if err != nil {
			return false, err
		}
		nb := b.Body().FirstMatchingBlock(np.Nested, nil)
		if nb == nil {
			nb = b.Body().AppendNewBlock(np.Nested, nil)
		}
		nb.Body().SetAttributeValue(np.Attr, np.Value)
	}

	for _, a := range appends {
		block := f.Body().AppendNewBlock("resource", []string{a.Type, a.Name})
		body := block.Body()
		for _, name := range sortedKeys(a.Attributes) {
			body.SetAttributeValue(name, a.Attributes[name])
		}
	}

	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return changed(src, f.Bytes()), nil
}

// findBlock locates a resource block by "<type>.<name>". hclwrite's block type
// is the literal "resource"; the Terraform resource type and name are labels.
func findBlock(f *hclwrite.File, resource, path string) (*hclwrite.Block, error) {
	typeName, resName, err := splitLabel(resource)
	if err != nil {
		return nil, fmt.Errorf("patch %q: %w", resource, err)
	}
	b := f.Body().FirstMatchingBlock("resource", []string{typeName, resName})
	if b == nil {
		return nil, fmt.Errorf("patch %q: resource block not found in %s", resource, path)
	}
	return b, nil
}

func splitLabel(s string) (typeName, resName string, err error) {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[:i], s[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("expected <type>.<name>")
}

func changed(before, after []byte) bool {
	return string(before) != string(after)
}

func sortPatches(ps []Patch) {
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Resource+"."+ps[i].Attr < ps[j].Resource+"."+ps[j].Attr })
}

func sortNested(ns []NestedPatch) {
	sort.SliceStable(ns, func(i, j int) bool {
		return ns[i].Resource+"."+ns[i].Nested+"."+ns[i].Attr < ns[j].Resource+"."+ns[j].Nested+"."+ns[j].Attr
	})
}

func sortAppends(as []AppendBlock) {
	sort.SliceStable(as, func(i, j int) bool { return as[i].Type+"."+as[i].Name < as[j].Type+"."+as[j].Name })
}

func sortedKeys(m map[string]cty.Value) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
