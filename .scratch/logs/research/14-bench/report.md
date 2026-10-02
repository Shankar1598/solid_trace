Engine `v1.5.6` on remote-dev-shankar-v7, 2 CPU, 12246192 kB RAM, linux/amd64. DuckDB memory_limit 2GB, threads 2.

Engine settings: map[disable_parquet_prefetching:false memory_limit:1.8 GiB prefetch_all_parquet_files:false storage_block_prefetch:REMOTE_ONLY threads:2 variant_minimum_shredding_size:30000]

### Writes

| Layout | Generator alone (Logs/s) | Phase | Target MB/s | Logs/s | MB/s | Flush p50 / p99 / max ms | Max backlog (Logs) | Kept up | Peak RSS MB | CPU % | DuckDB mem MB | Queries during (p50 / max ms, errors) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| json | 159628 | unthrottled | max | 26898 | 15.30 | 61 / 2818 / 2987 | 0 |  | 950 | 124 | 156 |  |
| json |  | target | 1.9 | 3329 | 1.89 | 33 / 208 / 208 | 170 | yes | 677 | 9 | 174 |  |
| json |  | target_with_queries | 1.9 | 3309 | 1.88 | 59 / 480 / 480 | 397 | yes | 550 | 172 | 188 | 67 (42 / 836, 0) |
| json |  | burst_5x | 9.5 | 14179 | 8.06 | 284 / 2635 / 2635 | 29971 | **no** | 765 | 84 | 217 |  |
| map | 189950 | unthrottled | max | 17326 | 9.86 | 155 / 2179 / 4093 | 0 |  | 616 | 136 | 88 |  |
| map |  | target | 1.9 | 3322 | 1.89 | 94 / 3736 / 3736 | 9375 | **no** | 620 | 65 | 89 |  |
| map |  | target_with_queries | 1.9 | 3281 | 1.86 | 229 / 615 / 615 | 336 | yes | 287 | 168 | 102 | 184 (23 / 382, 0) |
| map |  | burst_5x | 9.5 | 16221 | 9.23 | 499 / 2866 / 2866 | 33662 | **no** | 887 | 85 | 128 |  |

### Size on disk

| Layout | Rows | Generated MB | Final checkpoint ms | .duckdb MB | B/Log | Parquet MB (row group size) |
|---|---|---|---|---|---|---|
| json | 1230924 | 700 | 1 | 272.6 | 221 |  |
| map | 870926 | 495 | 2965 | 157.0 | 180 |  |

### Hot table queries (ms)

| Query | json cold / warm | map cold / warm |
|---|---|---|
| errors_last_15m | 39.7 / 14.3 | 45.0 / 20.1 |
| count_by_service_per_minute | 56.6 / 39.1 | 42.2 / 26.3 |
| common_attr_eq | 117 / 135 | 113 / 66.5 |
| common_attr_per_minute | 832 / 567 | 337 / 299 |
| rare_attr_eq | 980 / 583 | 468 / 267 |
| mixed_numeric_range | 118 / 91.3 | 56.5 / 24.3 |
| message_substring | 36.0 / 20.0 | 49.9 / 27.9 |
| group_by_attr_top10 | 1109 / 803 | 376 / 278 |
| fetch_by_id | 8.7 / 1.7 | 23.2 / 4.7 |

### Hot table, cold, storage_block_prefetch = ALWAYS_PREFETCH (ms)

| Query | json cold | map cold |
|---|---|---|
| errors_last_15m | 26.6 | 42.2 |
| count_by_service_per_minute | 47.2 | 61.4 |
| common_attr_eq | 174 | 159 |
| common_attr_per_minute | 839 | 364 |
| rare_attr_eq | 1199 | 502 |
| mixed_numeric_range | 227 | 52.6 |
| message_substring | 31.7 | 45.9 |
| group_by_attr_top10 | 1139 | 378 |
| fetch_by_id | 64.9 | 53.7 |

### Parquet export

| Layout | Row group size | Export ms | Files | Row groups | Leaf columns | MB | Peak RSS MB, async on / off |
|---|---|---|---|---|---|---|---|

### Results match the JSON baseline

Over Logs 0..50000 of Project 1. ✓ = identical rows (and count/id checksum); Parquet is compared with the same layout's hot table.

| Query | json hot | json Parquet | map hot | map Parquet |
|---|---|---|---|---|
| errors_last_15m | ✓ | – | ✓ | – |
| count_by_service_per_minute | ✓ | – | ✓ | – |
| common_attr_eq | ✓ | – | ✓ | – |
| common_attr_per_minute | ✓ | – | ✓ | – |
| rare_attr_eq | ✓ | – | ✓ | – |
| mixed_numeric_range | ✓ | – | ✓ | – |
| message_substring | ✓ | – | ✓ | – |
| group_by_attr_top10 | ✓ | – | ✓ | – |
| fetch_by_id | ✓ | – | ✓ | – |

### Projection to a full day (276480000 Logs), not measured

Warm latency of the queries that scan the whole time range, scaled linearly by rows. This is a projection: it ignores memory pressure, cache misses and the larger row counts per file a day brings.

| Query | json hot s | json Parquet s | map hot s | map Parquet s |
|---|---|---|---|---|
| count_by_service_per_minute | 8.8 | – | 8.3 | – |
| common_attr_eq | 30.4 | – | 21.1 | – |
| common_attr_per_minute | 127.3 | – | 95.0 | – |
| rare_attr_eq | 130.9 | – | 84.9 | – |
| mixed_numeric_range | 20.5 | – | 7.7 | – |
| message_substring | 4.5 | – | 8.8 | – |
| group_by_attr_top10 | 180.4 | – | 88.2 | – |

### Per-Project hourly Parquet, top N keys promoted

One zstd file per Project per hour, 30720-row row groups, sorted by time. Queries read Project 1's files (861393 Logs).

| Layout | N | Files | MB | B/Log | Top keys ms | Copy ms |
|---|---|---|---|---|---|---|
| json | 0 | 3 | 132.2 | 107 | 0 | 5831 |
| map | 0 | 3 | 89.0 | 102 | 0 | 7243 |
| map | 10 | 3 | 78.3 | 90 | 1349 | 8798 |
| map | 20 | 3 | 75.5 | 87 | 1172 | 10312 |
| map | 40 | 3 | 74.2 | 85 | 1288 | 12688 |

#### Queries (ms, cold / warm)

| Query | json N=0 map | map N=0 map | map N=10 coalesce | map N=10 per_file | map N=20 coalesce | map N=20 per_file | map N=40 coalesce | map N=40 per_file |
|---|---|---|---|---|---|---|---|---|
| errors_last_15m | 377 / 288 | 246 / 235 | 325 / 210 | 260 / 211 | 275 / 215 | 250 / 253 | 371 / 241 | 310 / 238 |
| count_by_service_per_minute | 67.4 / 45.6 | 55.6 / 74.0 | 75.7 / 34.8 | 53.3 / 37.8 | 54.3 / 68.5 | 52.6 / 36.2 | 55.9 / 36.5 | 60.6 / 40.3 |
| common_attr_eq | 1085 / 994 | 597 / 539 | 393 / 340 | 259 / 212 | 346 / 276 | 257 / 197 | 308 / 255 | 273 / 211 |
| common_attr_per_minute | 734 / 638 | 431 / 435 | 188 / 156 | 43.2 / 26.1 | 130 / 98.6 | 42.9 / 24.0 | 88.9 / 62.3 | 49.6 / 28.8 |
| rare_attr_eq | 1092 / 997 | 608 / 511 | 461 / 420 | 646 / 340 | 327 / 254 | 321 / 253 | 276 / 339 | 318 / 234 |
| mixed_numeric_range | 1152 / 929 | 483 / 402 | 316 / 252 | 340 / 328 | 414 / 247 | 313 / 356 | 306 / 314 | 339 / 268 |
| message_substring | 443 / 313 | 285 / 242 | 290 / 219 | 296 / 327 | 280 / 221 | 282 / 233 | 298 / 221 | 284 / 235 |
| group_by_attr_top10 | 900 / 944 | 426 / 455 | 194 / 255 | 41.4 / 25.6 | 158 / 100 | 42.8 / 26.6 | 100 / 68.1 | 44.9 / 25.9 |
| fetch_by_id | 42.9 / 24.4 | 73.9 / 56.5 | 60.6 / 54.0 | 55.2 / 55.2 | 45.5 / 50.0 | 166 / 62.2 | 78.2 / 65.4 | 71.0 / 54.1 |

#### Same, threads = 1 (ms, cold / warm)

| Query | json N=0 map | map N=0 map | map N=40 per_file |
|---|---|---|---|
| errors_last_15m | 480 / 354 | 357 / 248 | 380 / 306 |
| count_by_service_per_minute | 105 / 76.4 | 77.4 / 44.1 | 92.9 / 52.2 |
| common_attr_eq | 1379 / 1211 | 853 / 713 | 451 / 271 |
| common_attr_per_minute | 956 / 845 | 523 / 508 | 74.0 / 36.6 |
| rare_attr_eq | 1298 / 1082 | 872 / 742 | 455 / 330 |
| mixed_numeric_range | 1624 / 1373 | 577 / 423 | 333 / 259 |
| message_substring | 645 / 498 | 417 / 305 | 386 / 315 |
| group_by_attr_top10 | 1185 / 1128 | 607 / 458 | 71.5 / 31.9 |
| fetch_by_id | 44.8 / 81.8 | 59.9 / 42.8 | 56.6 / 46.3 |

#### Results match the same layout's hot table

| Query | json N=0 map | map N=0 map | map N=10 coalesce | map N=10 per_file | map N=20 coalesce | map N=20 per_file | map N=40 coalesce | map N=40 per_file |
|---|---|---|---|---|---|---|---|---|
| errors_last_15m | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| count_by_service_per_minute | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| common_attr_eq | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| common_attr_per_minute | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| rare_attr_eq | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| mixed_numeric_range | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| message_substring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| group_by_attr_top10 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| fetch_by_id | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

#### Projection to 4 vCPU, not measured

Warm 2-thread latency scaled linearly by rows, then divided by the 1→2-thread speedup measured for the same query (from the column's own threads = 1 run, else the same layout's N=0 run), assuming 2→4 threads scales as 1→2 did. **Full scans** are scaled to a day of a Project that receives the instance's whole volume (276480000 Logs); **latest 100** queries to its newest hour file (11520000 Logs), an upper bound that assumes the whole hour is scanned.

| Query | Scaled to | json N=0 map s | map N=0 map s | map N=10 coalesce s | map N=10 per_file s | map N=20 coalesce s | map N=20 per_file s | map N=40 coalesce s | map N=40 per_file s |
|---|---|---|---|---|---|---|---|---|---|
| count_by_service_per_minute | day | 8.7 | 56.3 | 26.5 | 28.8 | 52.2 | 27.6 | 27.8 | 14.1 |
| common_attr_eq | hour | 10.9 | 7.7 | 4.9 | 3.0 | 3.9 | 2.8 | 3.6 | 3.1 |
| common_attr_per_minute | day | 154.5 | 169.3 | 60.6 | 10.2 | 38.4 | 9.4 | 24.2 | 10.2 |
| rare_attr_eq | hour | 12.3 | 6.7 | 5.5 | 4.4 | 3.3 | 3.3 | 4.4 | 3.1 |
| mixed_numeric_range | hour | 8.4 | 7.2 | 4.5 | 5.9 | 4.4 | 6.4 | 5.6 | 5.3 |
| message_substring | hour | 2.6 | 3.6 | 3.3 | 4.9 | 3.3 | 3.5 | 3.3 | 3.3 |
| group_by_attr_top10 | day | 253.3 | 205.2 | 114.9 | 11.5 | 45.2 | 12.0 | 30.7 | 9.6 |
