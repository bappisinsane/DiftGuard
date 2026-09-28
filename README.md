# DriftGuard — Deterministic IaC Drift Detection & Remediation Engine

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg?style=flat-square)](https://go.dev/)
[![Terraform](https://img.shields.io/badge/Terraform-OpenTofu-7B42BC.svg?style=flat-square)](https://terraform.io/)
[![HCL](https://img.shields.io/badge/HCL-v2-0052FF.svg?style=flat-square)](https://github.com/hashicorp/hcl)
[![License](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](LICENSE)

A deterministic Infrastructure-as-Code (IaC) drift detection and remediation engine. DriftGuard compares live cloud API states (AWS, GCP, Azure) against Terraform/OpenTofu HCL AST representations, identifies state discrepancies, and outputs precise HCL code patches and state import scripts — without any machine learning or non-deterministic heuristics.

**Zero-AI. 100% reproducible. Built for SREs, platform engineers, and cloud security teams who need to know their live infrastructure matches their code.**

---

## Table of Contents

- [The Problem](#the-problem)
- [How It Works](#how-it-works)
- [Features](#features)
- [Quick Start](#quick-start)
- [Commands](#commands)
- [Architecture](#architecture)
- [Providers](#providers)
- [CI/CD Integration](#cicd-integration)
- [Daemon Mode](#daemon-mode)
- [Performance](#performance)
- [Project Structure](#project-structure)
- [Roadmap](#roadmap)
- [License](#license)

---

## The Problem

Your Terraform state says one thing. Your cloud console says another. Someone opened a security group manually. An S3 bucket got made public. A region got toggled. `terraform plan` catches some of it — but only if you run it, and only against what's in state. DriftGuard runs continuously, compares live APIs against your HCL source of truth, and tells you exactly what drifted, down to the attribute level.

---

## How It Works

DriftGuard follows a strictly deterministic four-stage pipeline:

```
Cloud APIs (AWS / GCP / Azure)
        |
        v
  [Ingest] --> Live Resource Map
        |
        v
  [Parse]  --> .tf files -> HCL AST -> AST Map
        |
        v
  [Diff]   --> Resource Map ⬌ AST Map = Attribute Delta Graph
        |
        v
  [Patch]  --> HCL Patch Files + terraform import / state mv Scripts
```

1. **Ingest** — Concurrent fetching of live cloud resources via official Cloud SDKs (AWS SDK for Go v2, GCP client libraries, Azure SDK for Go).
2. **Parse** — Deep analysis of `.tf` / `.tf.json` files using `hashicorp/hcl/v2`, producing a structured AST map of every resource block, attribute, and dependency.
3. **Diff** — Structural comparison of the live resource map against the HCL AST map. Computes exact attribute deltas: added, modified, or deleted attributes, mapped directly to Terraform resource schema fields (`aws_s3_bucket`, `aws_security_group`, etc.).
4. **Patch** — Generates valid HCL code blocks to insert missing or updated attributes, plus executable CLI scripts (`terraform import <resource_id>`, `terraform state mv`) to synchronize state.

Because every step is deterministic — no AI, no heuristics, no fuzzy matching — outputs are 100% reproducible across runs.

---

## Features

- **Cloud-Native Ingestion** — Concurrent fetching of live cloud resources using official Cloud SDKs. Designed for multi-provider scan in a single pass.
- **HCL AST Parsing** — Full `.tf` file parsing via `hashicorp/hcl/v2` and programmatic HCL generation via `hclwrite`. Preserves code comments and formatting during patch emission.
- **Deterministic Graph Diffing** — Exact attribute-level deltas mapped to Terraform resource schemas. No probabilistic matching, no ML inference.
- **Automated Remediation** — Generates both HCL patch files (to bring `.tf` source in sync) and `terraform import` / `terraform state mv` scripts (to bring Terraform state in sync).
- **CI/CD Gatekeeper** — `driftguard check --strict` exits non-zero when live state deviates from the master Git branch's HCL — blocks unreviewed infrastructure changes.
- **Daemon Mode** — Lightweight background service with webhook alerts to Slack, Microsoft Teams, and PagerDuty when drift is detected.
- **Zero-AI Guarantee** — Every output is reproducible. Run it twice on the same infrastructure, get the same diff.

---

## Quick Start

### Build

```bash
go build -o driftguard ./cmd/driftguard
```

### Run a Drift Scan

```bash
# Scan a local infrastructure directory against AWS
./driftguard scan --path ./infra --provider aws

# Scan and output a structured drift report
./driftguard scan --path ./infra --provider aws --format json
```

### Remediate Drift

```bash
# Generate HCL patch + import script (dry-run, default)
./driftguard remediate --path ./infra --provider aws

# Actually write the patch files and import script
./driftguard remediate --path ./infra --provider aws --write
```

### CI/CD Strict Check

```bash
# Exit non-zero if any drift is detected (for CI pipelines)
./driftguard check --path ./infra --provider aws --strict
```

---

## Commands

| Command | Description | Exit Codes |
|---------|-------------|------------|
| `driftguard scan` | Scan live cloud state against HCL, display drift table | 0 = no drift, 2 = drift detected |
| `driftguard remediate` | Generate HCL patches + import scripts to fix drift | 0 = success, 2 = drift found & patched |
| `driftguard check` | Strict CI mode — exit non-zero on any drift | 0 = in sync, 1 = drift detected |
| `driftguard daemon` | Run as background service with webhook alerting | — |

---

## Architecture

### Tech Stack

| Layer | Technology |
|-------|------------|
| Language | Go 1.22+ (native concurrency, small static binaries) |
| HCL Parser | `github.com/hashicorp/hcl/v2` |
| HCL Writer | `github.com/hashicorp/hcl/v2/hclwrite` |
| CLI Framework | `github.com/spf13/cobra` |
| AWS SDK | `github.com/aws/aws-sdk-go-v2` |
| GCP SDK | Official GCP client libraries for Go |
| Azure SDK | Official Azure SDK for Go |
| Daemon Storage | SQLite / BoltDB (embedded, local) |

### Pipeline Detail

```
┌─────────────────────────────────────────────────────────┐
│ 1. INGEST — Cloud API Fetching                          │
│    - AWS SDK v2: S3, EC2, VPC, SG, IAM (concurrent)    │
│    - GCP: Storage, Compute                              │
│    - Azure: (planned)                                   │
│    Output: Live Resource Map (provider -> resource ->   │
│             attributes)                                  │
└─────────────────────────────────────────────────────────┘
                          |
                          v
┌─────────────────────────────────────────────────────────┐
│ 2. PARSE — HCL AST Extraction                           │
│    - Walk .tf files in --path                           │
│    - Parse with hcl/v2 into hcl.File AST nodes          │
│    - Extract resource blocks, attributes, meta-args     │
│    Output: AST Map (resource address -> attributes)     │
└─────────────────────────────────────────────────────────┘
                          |
                          v
┌─────────────────────────────────────────────────────────┐
│ 3. DIFF — Structural Delta Computation                  │
│    - Match live resources to HCL resource addresses     │
│    - Compute per-attribute deltas (added/mod/removed)   │
│    - Track which HCL blocks need patching               │
│    Output: Attribute Delta Graph                        │
└─────────────────────────────────────────────────────────┘
                          |
                          v
┌─────────────────────────────────────────────────────────┐
│ 4. PATCH — Remediant Emission                           │
│    - hclwrite: append/alter HCL blocks, preserve fmt    │
│    - Generate terraform import / state mv CLI script    │
│    Output: .tf patch files + driftguard-import.tf.sh    │
└─────────────────────────────────────────────────────────┘
```

---

## Providers

### AWS (Current)

- S3 buckets — bucket policies, ACLs, versioning, website config
- (Phase 3) EC2 instances, VPCs, Security Groups, IAM roles/policies

### GCP (Planned — Phase 3)

- Compute Instances
- Storage Buckets

### Azure (Planned)

- To be added in a future phase.

---

## CI/CD Integration

Use `driftguard check --strict` as a pipeline gate:

```yaml
# GitHub Actions example
- name: DriftGuard Check
  run: |
    go build -o driftguard ./cmd/driftguard
    ./driftguard check --path ./infra --provider aws --strict
```

Exit code `1` = drift detected — fail the pipeline. Exit code `0` = infrastructure matches code.

This prevents scenarios where a `terraform apply` would destroy or replace resources that drifted out of sync with the Git-managed HCL.

---

## Daemon Mode

Run DriftGuard as a lightweight background service:

```bash
./driftguard daemon --path ./infra --provider aws --interval 300 \
  --webhook-slack https://hooks.slack.com/services/... \
  --webhook-teams https://outlook.office.com/webhook/... \
  --webhook-pagerduty https://hooks.pagerduty.com/...
```

The daemon polls at the specified interval, diffs live state against HCL, and fires webhooks when drift is detected. Historical drift data is stored locally in SQLite / BoltDB for trend analysis.

---

## Performance

| Metric | Target |
|--------|--------|
| Scan + diff 1,000 resources | < 3 seconds |
| Peak RAM during graph diffing | < 128 MB |
| Binary size (Go static build) | ~10-15 MB |
| Determinism | 100% reproducible outputs |

---

## Project Structure

```
DiftGuard/
├── cmd/
│   └── driftguard/          # Cobra CLI entry point
├── pkg/
│   ├── hcl/
│   │   ├── parser.go        # HCL AST extraction (hcl/v2)
│   │   └── emitter.go       # HCL patch generation (hclwrite)
│   ├── diff/
│   │   └── engine.go        # Graph diffing engine
│   └── cloud/
│       └── aws/
│           ├── s3.go        # AWS S3 live state fetching
│           ├── ec2.go       # AWS EC2 (Phase 3)
│           ├── vpc.go       # AWS VPC (Phase 3)
│           ├── sg.go        # AWS Security Groups (Phase 3)
│           └── iam.go       # AWS IAM (Phase 3)
├── .freebuff/
├── go.mod
├── go.sum
├── main_prompt.json
├── architecture.md
├── prd.md
├── phases.md
├── rules.md
├── memory.md
└── README.md
```

---

## Roadmap

### Phase 1 — HCL AST Parsing & Live State Mapping (Sprint 1–2)
- [x] Go module + Cobra CLI skeleton
- [x] HCL parser (`pkg/hcl/parser.go`) using `hashicorp/hcl/v2`
- [x] AWS S3 live state fetcher (`pkg/cloud/aws/s3.go`)
- [x] Unit tests for HCL parsing and AWS S3 extraction

### Phase 2 — Graph Diffing Engine & HCL Patch Emitter (Sprint 3–4)
- [x] Graph diff engine (`pkg/diff/engine.go`)
- [x] HCL patch emitter (`pkg/hcl/emitter.go`) using `hclwrite`
- [x] `driftguard scan` command with formatted terminal output
- [x] Golden equality tests — zero loss of existing code comments

### Phase 3 — Multi-Provider Expansion & State Import Generation (Sprint 5–6)
- [x] `driftguard remediate` command — HCL patches + `terraform import` scripts (`--write` to apply)
- [ ] AWS EC2, VPC, Security Groups, IAM resources
- [ ] GCP Compute Instances, Storage Buckets

### Phase 4 — CI/CD Gatekeeper & Daemon Mode (Sprint 7–8)
- [x] `driftguard check` command with strict exit codes
- [x] Background daemon mode with Slack/Teams/PagerDuty webhooks
- [ ] Multi-platform release binaries via GoReleaser (macOS, Linux arm64/amd64, Windows)

---

## License

MIT License. See [LICENSE](LICENSE) for details.
