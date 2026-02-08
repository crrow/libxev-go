# Redis MVP Benchmark Report

Generated at: 2026-02-08T07:57:07Z UTC\n\nRequests per scenario: 2000\n\nConcurrency: 30\n\n## Scenarios

- ping_only: 100% PING
- read_heavy: 70% GET + 30% SET
- write_heavy: 80% SET + 20% GET

## Gates

- throughput ratio >= 0.70\n- p99 ratio <= 1.50\n\n## Comparison

scenario | mvp rps | redis rps | throughput ratio | mvp p99 ms | redis p99 ms | p99 ratio | pass
---|---:|---:|---:|---:|---:|---:|---
ping_only | 21374.9 | 26835.2 | 0.797 | 1.750 | 10.669 | 0.164 | true\nread_heavy | 21046.5 | 31102.7 | 0.677 | 1.746 | 1.397 | 1.250 | false\nwrite_heavy | 21087.0 | 30723.2 | 0.686 | 1.718 | 1.329 | 1.293 | false\n
## Target Details

### libxev-go-mvp (127.0.0.1:6390)\n\nscenario | throughput rps | p50 ms | p95 ms | p99 ms | errors
---|---:|---:|---:|---:|---:
ping_only | 21374.9 | 1.384 | 1.596 | 1.750 | 0\nread_heavy | 21046.5 | 1.379 | 1.665 | 1.746 | 0\nwrite_heavy | 21087.0 | 1.405 | 1.638 | 1.718 | 0\n
### redis-server (127.0.0.1:6391)\n\nscenario | throughput rps | p50 ms | p95 ms | p99 ms | errors
---|---:|---:|---:|---:|---:
ping_only | 26835.2 | 0.941 | 1.232 | 10.669 | 0\nread_heavy | 31102.7 | 0.935 | 1.248 | 1.397 | 0\nwrite_heavy | 30723.2 | 0.956 | 1.214 | 1.329 | 0\n
