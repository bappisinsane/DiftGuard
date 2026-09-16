# Implementation Roadmap & Sprint Phases

## Phase 1: HCL AST Parsing & Live State Mapping (Sprint 1–2)
- [ ] Initialize Go module (`driftguard`) with Cobra CLI framework.
- [ ] Implement `pkg/hcl/parser.go` using `hashicorp/hcl/v2` to extract resource blocks into structured Go objects.
- [ ] Implement `pkg/cloud/aws/s3.go` using `aws-sdk-go-v2` to fetch S3 bucket configurations.
- [ ] Write unit tests verifying HCL file parsing and live AWS state extraction.

## Phase 2: Graph Diffing Engine & HCL Patch Emitter (Sprint 3–4)
- [x] Build `pkg/diff/engine.go` to compute structural deltas between HCL AST targets and live state objects.
- [x] Implement `pkg/hcl/emitter.go` using `hclwrite` to programmatically append or alter HCL blocks without breaking file formatting.
- [x] Implement `driftguard scan` command displaying formatted terminal drift tables (exits 2 on drift).
- [x] Build unit tests proving zero loss of existing code comments during AST patch generation (full-file golden equality).

## Phase 3: Multi-Provider Expansion & State Import Generation (Sprint 5–6)
- [ ] Add support for AWS EC2, VPC, Security Groups, and IAM resources.
- [x] Implement `driftguard remediate` command to generate both HCL patch files and `terraform import` scripts (dry-run default; `--write` applies via hclwrite and writes `driftguard-import.tf.sh`).
- [ ] Add GCP provider core resources (Compute Instances, Storage Buckets).

## Phase 4: CI/CD Gatekeeper & Daemon Mode (Sprint 7–8)
- [x] Implement `driftguard check` command returning strict exit codes for CI/CD integration (GitHub Actions, GitLab CI).
- [x] Build lightweight background daemon mode with webhook alerting (Slack, Teams, PagerDuty).
- [ ] Build multi-platform release binaries (macOS, Linux arm64/amd64, Windows) using GoReleaser.