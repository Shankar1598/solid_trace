Engine `v1.5.6` on remote-dev-shankar-v7, 2 CPU, 12246192 kB RAM, linux/amd64. DuckDB memory_limit 2GB, threads 2. An hour is 100000 Logs (57 MB generated); ingest 1× target (1.9 MB/s); N = 20 promoted keys; checkpoint after delete: true.

| Hour | Rows exported | Files | Top keys ms | Copy ms | Delete ms | Checkpoint ms | Table rows | .duckdb MB | WAL MB | Blocks (free) | Flush p50 / p99 / max ms during export | Max backlog during | Flush p50 / p99 / max ms between |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 100000 | 3 | 214 | 1885 | 30 | 432 | 104800 | 33.6 | 0.0 | 128 (73) | 172 / 172 / 172 | 0 | 101 / 1178 / 1178 |
| 1 | 100000 | 3 | 150 | 1389 | 14 | 1216 | 104003 | 39.6 | 0.0 | 151 (100) | 254 / 254 / 254 | 0 | 144 / 2719 / 2719 |
| 2 | 100000 | 3 | 159 | 1754 | 11 | 1479 | 106403 | 26.2 | 0.0 | 100 (48) | 289 / 289 / 289 | 4 | 100 / 964 / 964 |
