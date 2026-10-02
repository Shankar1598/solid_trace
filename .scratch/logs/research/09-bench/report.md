Engine `v2.0.0-alpha43385` on remote-dev-shankar-v7, 2 CPU, 12246192 kB RAM, linux/amd64. DuckDB memory_limit 2GB, threads 2.

Engine settings: map[async_threads:8 disable_parquet_prefetching:false memory_limit:1.8 GiB prefetch_all_parquet_files:false read_ahead_depth:-1 storage_block_prefetch:REMOTE_ONLY threads:2 variant_minimum_shredding_size:30000]

### Writes

| Layout | Generator alone (Logs/s) | Phase | Target MB/s | Logs/s | MB/s | Flush p50 / p99 / max ms | Max backlog (Logs) | Kept up | Peak RSS MB | CPU % | DuckDB mem MB | Queries during (p50 / max ms, errors) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| json | 277944 | unthrottled | max | 32786 | 18.60 | 58 / 2140 / 2228 | 0 |  | 873 | 118 | 178 |  |
| json |  | target | 1.9 | 3345 | 1.89 | 35 / 148 / 148 | 179 | yes | 872 | 7 | 193 |  |
| json |  | target_with_queries | 1.9 | 3303 | 1.88 | 85 / 276 / 276 | 170 | yes | 761 | 178 | 207 | 110 (32 / 595, 0) |
| json |  | burst_5x | 9.5 | 16528 | 9.38 | 183 / 1994 / 1994 | 18805 | **no** | 748 | 54 | 255 |  |
| map | 245804 | unthrottled | max | 20305 | 11.52 | 154 / 2107 / 2135 | 0 |  | 719 | 122 | 72 |  |
| map |  | target | 1.9 | 3323 | 1.89 | 100 / 200 / 200 | 168 | yes | 551 | 16 | 91 |  |
| map |  | target_with_queries | 1.9 | 3293 | 1.87 | 230 / 1106 / 1106 | 605 | **no** | 302 | 177 | 91 | 342 (25 / 198, 0) |
| map |  | burst_5x | 9.5 | 14955 | 8.49 | 505 / 2329 / 2329 | 24623 | **no** | 689 | 85 | 112 |  |
| variant_noshred | 230594 | unthrottled | max | 25903 | 14.69 | 126 / 1024 / 1103 | 0 |  | 870 | 134 | 573 |  |
| variant_noshred |  | target | 1.9 | 3318 | 1.89 | 88 / 238 / 238 | 174 | yes | 861 | 16 | 624 |  |
| variant_noshred |  | target_with_queries | 1.9 | 3296 | 1.87 | 189 / 598 / 598 | 175 | yes | 812 | 176 | 661 | 158 (25 / 445, 0) |
| variant_noshred |  | burst_5x | 9.5 | 13863 | 7.86 | 547 / 2973 / 2973 | 35714 | **no** | 1431 | 87 | 804 |  |

### Size on disk

| Layout | Rows | Generated MB | Final checkpoint ms | .duckdb MB | B/Log | Parquet MB (row group size) |
|---|---|---|---|---|---|---|
| json | 1351632 | 767 | 162 | 277.4 | 205 | 144.6 (122880) |
| map | 981640 | 557 | 1 | 136.9 | 139 | 98.7 (122880) |
| variant_noshred | 1098155 | 623 | 1 | 258.5 | 235 | 122.1 (122880) |

### VARIANT shredding in the hot table (after the final checkpoint)

| Layout | Row groups | Shredded | Rows in unshredded row groups | Unshredded row-group sizes |
|---|---|---|---|---|
| variant_noshred | 10 | 0 | 1098155 | 120000, 120000, 120000, 120000, 122880, 122880, 122880, 122880, … |

### Hot table queries (ms)

| Query | json cold / warm | map cold / warm | variant_noshred cold / warm |
|---|---|---|---|
| errors_last_15m | 25.4 / 9.1 | 39.3 / 16.7 | 31.3 / 17.9 |
| count_by_service_per_minute | 58.5 / 36.0 | 41.5 / 28.6 | 45.9 / 31.5 |
| common_attr_eq | 74.9 / 37.1 | 62.4 / 33.3 | 114 / 55.4 |
| rare_attr_eq | 1088 / 533 | 294 / 105 | 921 / 359 |
| mixed_numeric_range | 77.9 / 45.2 | 40.1 / 17.8 | 168 / 116 |
| message_substring | 24.1 / 14.9 | 41.6 / 25.3 | 41.5 / 28.2 |
| group_by_attr_top10 | 878 / 652 | 250 / 145 | 712 / 356 |
| fetch_by_id | 15.0 / 4.9 | 18.1 / 6.0 | 21.5 / 4.2 |

### Hot table, cold, storage_block_prefetch = ALWAYS_PREFETCH (ms)

| Query | json cold | map cold | variant_noshred cold |
|---|---|---|---|
| errors_last_15m | 18.6 | 27.9 | 27.9 |
| count_by_service_per_minute | 45.8 | 38.1 | 60.4 |
| common_attr_eq | 51.7 | 57.2 | 362 |
| rare_attr_eq | 1425 | 311 | 1176 |
| mixed_numeric_range | 62.5 | 31.8 | 980 |
| message_substring | 20.8 | 37.5 | 37.1 |
| group_by_attr_top10 | 1088 | 203 | 844 |
| fetch_by_id | 56.2 | 28.1 | 63.4 |

### Parquet export

| Layout | Row group size | Export ms | Files | Row groups | Leaf columns | MB | Peak RSS MB, async on / off |
|---|---|---|---|---|---|---|---|
| json | 122880 | 5332 | 1 | 11 | 10 | 144.6 | 520 / 449 |
| map | 122880 | 5224 | 1 | 8 | 15 | 98.7 | 412 / 358 |
| variant_noshred | 122880 | 23980 | 1 | 9 | 89 | 122.1 | 474 / 476 |

### Parquet, row group 122880, async on (ms)

| Query | json cold / warm | map cold / warm | variant_noshred cold / warm |
|---|---|---|---|
| errors_last_15m | 483 / 465 | 360 / 329 | 1043 / 503 |
| count_by_service_per_minute | 71.4 / 61.5 | 53.8 / 44.3 | 77.9 / 55.1 |
| common_attr_eq | 1295 / 1044 | 866 / 827 | 3201 / 3119 |
| rare_attr_eq | 1401 / 1121 | 795 / 815 | 3689 / 3498 |
| mixed_numeric_range | 1319 / 1200 | 516 / 501 | 6416 / 9061 |
| message_substring | 540 / 532 | 394 / 337 | skipped |
| group_by_attr_top10 | 921 / 867 | 578 / 509 | skipped |
| fetch_by_id | 195 / 129 | 223 / 224 | skipped |

### Parquet, row group 122880, async off: read_ahead_depth = 0 (ms)

| Query | json cold / warm | map cold / warm | variant_noshred cold / warm |
|---|---|---|---|
| errors_last_15m | 472 / 433 | 357 / 291 | 1005 / 936 |
| count_by_service_per_minute | 99.8 / 60.6 | 53.2 / 49.5 | 105 / 103 |
| common_attr_eq | 1289 / 1121 | 933 / 764 | 4057 / 3193 |
| rare_attr_eq | 1174 / 1120 | 862 / 775 | 3780 / 4018 |
| mixed_numeric_range | 1556 / 1187 | 514 / 476 | 7533 / 6745 |
| message_substring | 600 / 484 | 461 / 328 | skipped |
| group_by_attr_top10 | 1104 / 870 | 567 / 583 | skipped |
| fetch_by_id | 165 / 90.4 | 256 / 182 | skipped |

### Results match the JSON baseline

Over Logs 0..50000 of Project 1. ✓ = identical rows (and count/id checksum); Parquet is compared with the same layout's hot table.

| Query | json hot | json Parquet | map hot | map Parquet | variant_noshred hot | variant_noshred Parquet |
|---|---|---|---|---|---|---|
| errors_last_15m | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| count_by_service_per_minute | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| common_attr_eq | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| rare_attr_eq | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| mixed_numeric_range | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| message_substring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| group_by_attr_top10 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| fetch_by_id | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

### Projection to a full day (276480000 Logs), not measured

Warm latency of the queries that scan the whole time range, scaled linearly by rows. This is a projection: it ignores memory pressure, cache misses and the larger row counts per file a day brings.

| Query | json hot s | json Parquet s | map hot s | map Parquet s | variant_noshred hot s | variant_noshred Parquet s |
|---|---|---|---|---|---|---|
| count_by_service_per_minute | 7.4 | 12.6 | 8.0 | 12.5 | 7.9 | 13.9 |
| common_attr_eq | 7.6 | 213.5 | 9.4 | 233.0 | 14.0 | 785.3 |
| rare_attr_eq | 109.0 | 229.3 | 29.4 | 229.5 | 90.3 | 880.7 |
| mixed_numeric_range | 9.2 | 245.4 | 5.0 | 141.2 | 29.3 | 2281.2 |
| message_substring | 3.0 | 108.7 | 7.1 | 95.0 | 7.1 | – |
| group_by_attr_top10 | 133.5 | 177.3 | 40.9 | 143.4 | 89.6 | – |
