# Redis Benchmarking

Redis MVP benchmark workflow is designed for deterministic local validation.

## Commands

```bash
just bench-compare 2000 30
just bench-report
```

## Scenario Matrix

- `ping_only`: 100% `PING`
- `read_heavy`: 70% `GET` + 30% `SET`
- `write_heavy`: 80% `SET` + 20% `GET`

## Report Artifacts

Reports are written into `benchmarks/reports/`:

- `latest.json` for machine-readable fields
- `latest.md` for review
- timestamped immutable snapshots

## Acceptance Gate (MVP)

- throughput ratio (`libxev-go-mvp` / `redis-server`) >= `0.70`
- p99 latency ratio (`libxev-go-mvp` / `redis-server`) <= `1.50`

These values are recorded per scenario in the report comparison table.
