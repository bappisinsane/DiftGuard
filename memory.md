# Project Memory & Decisions Log

## Core Details
* **Project Name:** DriftGuard Engine
* **Objective:** Fast, deterministic IaC drift detection and remediation using Go and native HCL AST parsing.
* **Target Stack:** Go 1.22+, `hashicorp/hcl/v2`, AWS SDK for Go v2, Cobra CLI.

## Key Architectural Decisions (ADRs)
* **ADR-001:** Selected Go over Rust or TypeScript due to HashiCorp's native Go ecosystem support (`hashicorp/hcl/v2` and `hclwrite`), enabling zero-loss HCL manipulation.
* **ADR-002:** Standardized on `hclwrite` for code generation rather than string templates to preserve user comments, indentation, and formatting during automated patching.
* **ADR-003:** Applied Ponytail skill constraints—completely eliminating machine learning dependencies in favor of deterministic, high-speed structural graph diffing.
* **ADR-004:** hclwrite block lookup requires `FirstMatchingBlock("resource", []string{type, name})` — the Terraform resource type and name are *labels*; `"aws_s3_bucket"` is never the block type. Documented after the Phase 2 emitter lookup initially failed on this.
* **ADR-005:** Live S3 ACL is translated to a canned-ACL name only on exact grant-pattern match (`private`, `public-read`, `public-read-write`); custom grant sets yield `""` and the diff engine skips the field instead of guessing. Unknown ≠ drifted.
* **ADR-006:** Remediation semantics: patch findings adopt live values into HCL only when HCL states the setting explicitly (silent never adopted); unmanaged account buckets become `terraform import` commands; public-policy drift is a note, never auto-rewritten. Live state is matched by effective bucket name (`bucket` attribute, else label).
* **ADR-007:** `driftguard check` CI contract: stdout is pure JSON (`{"drift": bool, "findings": [...]}` with `findings: []` never `null`); errors go to stderr with exit 1; `--strict` exits 2 on drift. Scan keeps its legacy exit-2 for backward compatibility. Findings carry `file`/`line` (from `BlockRange`) for GitHub Actions annotations. Scan and check share one pipeline (`scanAndCheck` + `diff.S3Findings`) so human and CI output can never disagree.
* **ADR-008:** Daemon semantics: alert on *change* only (reflect.DeepEqual against the previous finding set — startup drift alerts, resolution alerts, steady state stays silent); transient per-tick errors log and retry, only SIGINT/SIGTERM (`signal.NotifyContext`) stops the loop; webhook delivery is the `chat.postMessage`-compatible `{"text": ...}` payload (satisfies Slack incoming webhooks and Teams connectors), with a 10s HTTP timeout; empty `--webhook-url` is log-only mode. No persistence of last state: a daemon restart re-alerts current drift, which is the desired fail-loud behavior.