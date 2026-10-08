# Validation report

Validation performed in the build environment with the installed Go toolchain:

- `gofmt -w ./...` — passed
- `go test ./...` — passed
- `go test -race ./...` — passed
- `go vet ./...` — passed
- `go build ./...` — passed

The project has no third-party Go module dependencies. Docker configuration is provided but a Docker image build was not executed in this environment.

These checks establish compilation and test-suite success for the included code, not production readiness. Review `docs/DESIGN.md` for known limitations, especially single-process directory ownership, manifest-less file lifecycle, in-memory SSTable loading, and synchronous compaction.

## Independent review (2026-10-08)

An independent review verified the implementation beyond the packaged tests and fixed four issues:

1. **Module path renamed** from `github.com/example/stonebridge` to `github.com/MinuuLakshmi17/stonebridge` (`go.mod` and the `cmd/stonebridge` import).
2. **Dead sparse index wired in.** `table.go` built a sparse key index on every SSTable but `get()` ignored it, doing a full binary search instead. `get()` now binary-searches the sampled keys and confines the record search to the correct 16-record window. Correctness argument: SSTable keys are unique and sorted, so the target can only lie in the window starting at the greatest sample key <= k.
3. **Crash-recovery test added** (`internal/lsm/crash_recovery_test.go`): 100 writes, flush, 50 more writes, reopen *without Close* (simulated crash) — all 150 keys verified, then overwrite + delete + compact verified (updated value wins, tombstoned key gone, exactly 1 table remains). Passes, including under `-race`.
4. **LICENSE added** (MIT, repository owner). The scaffold shipped without one.

Verification performed: `gofmt -l` clean, `go vet ./...` clean, `go test ./... -count=1` all pass, `go test -race` clean. Live HTTP smoke test (put → get → delete → get/404 → stats) all correct.

Benchmark on this machine (`go run ./cmd/bench`, 50k puts of 100-byte values, WAL fsync off): **53,547 writes/s**, **1,599,018 reads/s**, **1,497,191 missing-key lookups/s**. Reproducible via the bundled tool.

No algorithmic defects were found in the WAL framing, flush ordering, read precedence, tombstone masking, or compaction merge. Known limitations are documented in the README's scope section.
