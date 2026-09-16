# Engineering Rules & Standards — DriftGuard Engine

## 1. Ponytail Skill Decision Ladder (Strict Enforcement)
Before writing or modifying any code, apply these three checks in order:
1. **Does this code need to exist?** Reject unnecessary abstractions, speculative framework wrappers, or premature generalizers.
2. **Does the standard library or existing stack provide this?** Rely on native Go standard library packages (`net/http`, `sync`, `encoding/json`, `context`) and official `hashicorp/hcl/v2` APIs before introducing third-party dependencies.
3. **Can it be written in fewer lines without sacrificing correctness?** Keep Go code idioms explicit, concurrent, and lean. Avoid over-engineered interface layers or unnecessary struct hierarchies.

## 2. Determinism & Safety Rules
* **Zero AI / Zero External Heuristics:** All diffing logic must be strictly rule-based, deterministic, and unit-tested against actual cloud resource schemas.
* **Safe AST Manipulation:** Never alter HCL files using raw string formatting or regex. All code generation and patch emissions MUST use `hclwrite` syntax trees to preserve existing comments, indentation, and code structure.
* **Zero External Data Exfiltration:** The core engine must execute completely locally or within internal network boundaries. No telemetry or external metrics calls permitted.

## 3. Go Coding Standards
* **Concurrency Safety:** Always use structured concurrency (`errgroup.Group` or channel worker pools with bounded capacity) when querying Cloud APIs to enforce controlled rate limits and context deadlines.
* **Error Handling:** Wrap all errors with domain context (`fmt.Errorf("parsing hcl block %s: %w", blockName, err)`). Never discard errors or use unhandled panics in production logic.
* **Type Safety:** Leverage Go 1.22+ strict typing and generics where appropriate. Avoid unsafe pointer casting or empty interface (`any`) anti-patterns unless interacting directly with dynamically unmarshaled HCL attribute values.