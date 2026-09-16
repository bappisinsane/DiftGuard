# DiftGuard Engine

Deterministic Infrastructure-as-Code (IaC) drift detection and remediation engine.

DriftGuard compares live cloud API states (AWS, GCP, Azure) against Terraform/OpenTofu HCL AST representations, identifies state discrepancies, and outputs precise HCL code patches and state import scripts—without reliance on machine learning or non-deterministic heuristics.

## Key Capabilities

* **Cloud-Native Ingestion:** Concurrent fetching of live cloud resources using official Cloud SDKs.
* **HCL AST Parsing:** Deep analysis of `.tf` files using `hashicorp/hcl/v2`.
* **Deterministic Diffing:** Exact attribute deltas (added, modified, deleted) mapped directly to Terraform resource schemas.
* **Automated Remediation:** Generation of valid HCL patches and CLI scripts (`terraform import`, `terraform state mv`) to synchronize state.
* **CI/CD Integration:** Strict mode check (`driftguard check --strict`) to prevent unapproved infrastructure changes.

## Quick Start

```bash
# Build the engine
go build -o driftguard ./cmd/driftguard

# Run a drift check
./driftguard check --path ./infra --provider aws
```

## Architecture

DriftGuard follows a strictly deterministic pipeline:
1. **Ingest:** Cloud SDK -> API Response -> Resource Map.
2. **Parse:** `.tf` Files -> HCL AST -> AST Map.
3. **Diff:** Resource Map ⬌ AST Map = Attribute Delta Graph.
4. **Patch:** Attribute Delta Graph -> HCL Patch/Import Script.

## Non-Functional Requirements

* **Performance:** Diffing 1,000 resources in < 3s.
* **Efficiency:** < 128MB RAM peak during graph diffing.
* **Safety:** Zero-AI approach ensures 100% reproducible outcomes.
