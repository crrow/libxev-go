# Redis MVP Benchmarks

This directory stores reproducible Redis MVP benchmark artifacts.

## Scenarios

- `ping_only`: 100% `PING`
- `read_heavy`: 70% `GET` + 30% `SET`
- `write_heavy`: 80% `SET` + 20% `GET`

## Workflow

```bash
just bench-compare 2000 30
just bench-report
```

Artifacts are written under `benchmarks/reports/`:

- `latest.json`: machine-readable benchmark report
- `latest.md`: latest human-readable summary
- timestamped `benchmark-*.json` and `report-*.md`

## Baseline

`just bench-compare` runs against:

- MVP server: `cmd/redis-server` implementation (`libxev-go-mvp`)
- Reference server: `redis-server` binary from local environment

If `redis-server` is not installed, benchmark compare exits with an explicit error.
