# Technical Architecture — DriftGuard

## 1. System Overview & Tech Stack
* **Language/Runtime:** Go 1.22+ (Chosen for native concurrency, small binary size, and direct integration with HashiCorp HCL libraries).
* **AST Parser:** `github.com/hashicorp/hcl/v2` and `github.com/hashicorp/hcl/v2/hclwrite`.
* **Cloud Integration:** Official AWS SDK for Go v2 (`aws-sdk-go-v2`), GCP Client Libraries, Azure SDK for Go.
* **CLI Framework:** `github.com/spf13/cobra`.
* **Database/Cache (Daemon Mode):** SQLite / BoltDB (local embedded storage for historical drift tracking).

## 2. Data Conversion Pipeline Architecture