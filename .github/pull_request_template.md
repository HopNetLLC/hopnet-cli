## Summary

<!-- 1–3 sentences: what changed and why -->

## Phase

<!-- Reference the phase from ~/.claude/plans/replicated-squishing-dolphin.md, e.g. "Phase 8 — CLI command surface" -->

## Checklist

- [ ] Changes scoped to a single phase or a narrowly-defined slice of one.
- [ ] Tests added for new logic.
- [ ] No secrets logged in default-verbosity output.
- [ ] No `Co-Authored-By` trailer in commits.
- [ ] No unrelated diffs.
- [ ] `CGO_ENABLED=0` build still passes.

## Verification

```bash
go vet ./...
go test ./... -race -count=1
go build -o /tmp/hopnet ./cmd/hopnet
/tmp/hopnet version
```
