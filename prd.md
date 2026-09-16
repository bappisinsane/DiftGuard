# Product Requirements Document (PRD) — DriftGuard Engine

## 1. Executive Summary
DriftGuard is a deterministic Infrastructure-as-Code (IaC) drift detection and remediation engine written in Go. It compares live cloud API states (AWS, GCP, Azure) against Terraform/OpenTofu HCL AST representations, identifies state discrepancies, and outputs precise HCL code patches and state import scripts without machine learning or non-deterministic inference.

## 2. Target Audience
* **DevOps & Site Reliability Engineers (SREs):** Prevent accidental resource destruction during `terraform apply`.
* **Platform Engineers:** Maintain strict synchronization between Git repositories and live cloud state.
* **Cloud Security Engineers:** Instantly flag unauthorized manual changes (e.g., open security groups, public S3 buckets) in cloud consoles.

## 3. Core Features & Capabilities
* **P0: Cloud API & HCL AST Ingestion**
  * Concurrently fetch live cloud resource configurations via official Cloud SDKs.
  * Parse local/remote `.tf` files into `hcl.File` AST nodes using `hashicorp/hcl/v2`.
* **P0: Graph Structural Diffing Engine**
  * Map live cloud attributes to Terraform resource schema fields (`aws_s3_bucket`, `aws_security_group`, etc.).
  * Compute exact attribute deltas (added, modified, deleted attributes).
* **P1: Deterministic Remediator & AST Patch Emitter**
  * Generate valid HCL code blocks inserting missing or updated attributes into `.tf` files.
  * Output executable CLI scripts (`terraform import <resource_id>`, `terraform state mv`).
* **P1: Scheduled Daemon & Event Notifications**
  * Run as a lightweight background service emitting webhooks to Slack, Microsoft Teams, and PagerDuty when drift occurs.
* **P2: CI/CD Pipeline Gatekeeper**
  * Provide a CLI check command (`driftguard check --strict`) that exits with non-zero status if live state deviates from master Git branch HCL.

## 4. Non-Functional Requirements (NFRs)
* **Performance:** Scan and diff 1,000 cloud resources against HCL AST in $< 3.0\text{ seconds}$.
* **Memory Efficiency:** Peak RAM consumption during graph diffing must remain under $128\text{ MB}$.
* **Determinism & Safety:** 100% reproducible diff outputs; zero reliance on AI models or non-deterministic heuristics.