// Command driftguard detects drift between Terraform HCL and live cloud state.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	"driftguard/pkg/cloud/aws"
	"driftguard/pkg/diff"
	dg_hcl "driftguard/pkg/hcl"
)

var version = "1.0.0"

// fetchStates loads live bucket state; a package var so tests can inject a fake.
var fetchStates = liveBucketStates

func main() {
	root := &cobra.Command{
		Use:     "driftguard",
		Short:   "Deterministic IaC drift detection against live cloud state",
		Version: version,
	}
	root.AddCommand(scanCmd(), checkCmd(), remediateCmd(), daemonCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// scanAndCheck is the pipeline shared by scan and check: parse, fetch live
// state, diff. It is the whole drift engine; the commands differ only in how
// they render the findings.
func scanAndCheck(ctx context.Context, dir, region string) ([]diff.Finding, error) {
	refs, err := parseDir(dir)
	if err != nil {
		return nil, err
	}
	buckets := managedBuckets(refs)
	if len(buckets) == 0 {
		fmt.Fprintln(os.Stderr, "no aws_s3_bucket resources found") // notices go to stderr; stdout is command output
		return nil, nil
	}

	states, err := fetchStates(ctx, region, buckets)
	if err != nil {
		return nil, err
	}
	live := make(map[string]aws.BucketState, len(states))
	for _, s := range states {
		live[s.Name] = s
	}
	return diff.S3Findings(refs, live), nil
}

func scanCmd() *cobra.Command {
	var dir, region string
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Compare HCL resource blocks with live AWS S3 state",
		RunE: func(cmd *cobra.Command, args []string) error {
			findings, err := scanAndCheck(cmd.Context(), dir, region)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "RESOURCE\tFIELD\tHCL\tLIVE")
			for _, f := range findings {
				fmt.Fprintf(w, "%s.%s\t%s\t%s\t%s\n", f.ResourceType, f.ResourceName, f.Field, orUnset(f.Desired), orUnset(f.Actual))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if len(findings) > 0 {
				fmt.Printf("\n%d drift finding(s)\n", len(findings))
				// ponytail: scan exits 2 for backwards compatibility with
				// pre-Phase-4 scripts; CI should use `check --strict`.
				os.Exit(2)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "directory containing .tf files")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to provider chain)")
	return cmd
}

func checkCmd() *cobra.Command {
	var dir, region string
	var strict bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "CI gate: findings as JSON; exit 2 on drift with --strict",
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := runCheck(cmd.Context(), dir, region, strict); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "directory containing .tf files")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to provider chain)")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 2 when drift exists (for CI gates)")
	return cmd
}

// runCheck runs the check pipeline and prints the JSON contract to stdout
// (errors go to stderr, so stdout is always parseable JSON). Returns the
// process exit code: 0 clean, 2 strict drift, 1 error.
func runCheck(ctx context.Context, dir, region string, strict bool) int {
	findings, err := scanAndCheck(ctx, dir, region)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if findings == nil { // emit [] not null: empty dir is a normal CI case
		findings = []diff.Finding{}
	}
	out := struct {
		Drift    bool           `json:"drift"`
		Findings []diff.Finding `json:"findings"`
	}{Drift: len(findings) > 0, Findings: findings}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strict && len(findings) > 0 {
		return 2
	}
	return 0
}

func remediateCmd() *cobra.Command {
	var dir, region string
	var write bool
	cmd := &cobra.Command{
		Use:   "remediate",
		Short: "Adopt live cloud values into HCL and emit terraform import commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			refs, err := parseDir(dir)
			if err != nil {
				return err
			}
			buckets := managedBuckets(refs)
			if len(buckets) == 0 {
				return fmt.Errorf("no aws_s3_bucket resources found in %s", dir)
			}

			states, err := liveBucketStates(ctx, region, buckets)
			if err != nil {
				return err
			}
			client, err := s3Client(cmd.Context(), region)
			if err != nil {
				return err
			}
			accountBuckets, err := aws.ListBucketNames(ctx, client)
			if err != nil {
				return err
			}

			plan := diff.PlanS3(refs, states, accountBuckets)
			printPlan(plan)

			if !plan.HasWork() && len(plan.Imports) == 0 {
				fmt.Println("nothing to remediate")
				return nil
			}
			if !write {
				fmt.Println("\ndry run: re-run with --write to apply patches; review imports before running them")
				return nil
			}

			for _, fo := range groupByFile(refs, plan) {
				changed, err := dg_hcl.EmitEdits(fo.file, fo.patches, fo.nested, fo.appends)
				if err != nil {
					return err
				}
				if changed {
					fmt.Printf("patched %s\n", fo.file)
				}
			}
			if len(plan.Imports) > 0 {
				script := "driftguard-import.tf.sh"
				if err := os.WriteFile(script, []byte(strings.Join(plan.Imports, "\n")+"\n"), 0o755); err != nil {
					return fmt.Errorf("writing import script: %w", err)
				}
				fmt.Printf("wrote %s (%d import command(s))\n", script, len(plan.Imports))
			}
			for _, n := range plan.Notes {
				fmt.Printf("NOTE: %s\n", n)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "directory containing .tf files")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to provider chain)")
	cmd.Flags().BoolVar(&write, "write", false, "apply HCL patches and write the import script (default is dry run)")
	return cmd
}

// s3Client builds an S3 client from the provider chain.
func s3Client(ctx context.Context, region string) (*s3.Client, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}
	return s3.NewFromConfig(cfg), nil
}

// liveBucketStates fetches state for the given bucket names concurrently.
func liveBucketStates(ctx context.Context, region string, buckets []string) ([]aws.BucketState, error) {
	if len(buckets) == 0 {
		return nil, nil
	}
	client, err := s3Client(ctx, region)
	if err != nil {
		return nil, err
	}
	return aws.FetchBucketStates(ctx, client, buckets)
}

// fileOps is the set of emitter ops targeting one .tf file.
type fileOps struct {
	file    string
	patches []dg_hcl.Patch
	nested  []dg_hcl.NestedPatch
	appends []dg_hcl.AppendBlock
}

// groupByFile routes every plan op to the file of the resource it touches.
// Appends have no source resource; they go to the file holding the first
// aws_s3_bucket (or the first ref) so multi-file dirs are patched correctly.
func groupByFile(refs []dg_hcl.ResourceRef, plan diff.Plan) []fileOps {
	resToFile := make(map[string]string, len(refs))
	appendTarget := ""
	for _, r := range refs {
		resToFile[r.Type+"."+r.Name] = r.File
		if appendTarget == "" && r.Type == "aws_s3_bucket" {
			appendTarget = r.File
		}
	}
	if appendTarget == "" && len(refs) > 0 {
		appendTarget = refs[0].File
	}

	byFile := make(map[string]*fileOps)
	get := func(f string) *fileOps {
		fo, ok := byFile[f]
		if !ok {
			fo = &fileOps{file: f}
			byFile[f] = fo
		}
		return fo
	}
	for _, p := range plan.Patches {
		if f := resToFile[p.Resource]; f != "" {
			get(f).patches = append(get(f).patches, p)
		}
	}
	for _, np := range plan.NestedPatches {
		if f := resToFile[np.Resource]; f != "" {
			get(f).nested = append(get(f).nested, np)
		}
	}
	if len(plan.Appends) > 0 && appendTarget != "" {
		get(appendTarget).appends = plan.Appends
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sortStrings(files)
	out := make([]fileOps, 0, len(files))
	for _, f := range files {
		out = append(out, *byFile[f])
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// managedBuckets returns the live bucket names backing managed HCL refs.
func managedBuckets(refs []dg_hcl.ResourceRef) []string {
	var out []string
	for _, r := range refs {
		if r.Type == "aws_s3_bucket" {
			out = append(out, dg_hcl.BucketName(r))
		}
	}
	return out
}

func printPlan(p diff.Plan) {
	for _, np := range p.NestedPatches {
		fmt.Printf("would set %s.%s.%s = %q\n", np.Resource, np.Nested, np.Attr, np.Value.AsString())
	}
	for _, pt := range p.Patches {
		fmt.Printf("would set %s.%s = %q\n", pt.Resource, pt.Attr, pt.Value.AsString())
	}
	for _, a := range p.Appends {
		fmt.Printf("would append resource %q %q\n", a.Type, a.Name)
	}
	for _, im := range p.Imports {
		fmt.Printf("unmanaged: %s\n", im)
	}
	for _, n := range p.Notes {
		fmt.Printf("note: %s\n", n)
	}
}

// parseDir parses every .tf file in dir.
func parseDir(dir string) ([]dg_hcl.ResourceRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading dir %s: %w", dir, err)
	}
	var refs []dg_hcl.ResourceRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		rs, err := dg_hcl.ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		refs = append(refs, rs...)
	}
	return refs, nil
}

func orUnset(s string) string {
	if s == "" {
		return "Unset"
	}
	return s
}
