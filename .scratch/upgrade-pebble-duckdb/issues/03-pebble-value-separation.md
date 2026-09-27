# Pebble value separation: benchmark before enabling

Status: needs-triage

Blocked by: 01

After 01, stores are at `FormatValueSeparation` with the policy off. Values are raw event JSON of about 2.2 KB, which is the case value separation targets.
Before turning on `Experimental.ValueSeparationPolicy`, measure write amplification, `Get` latency (an extra blob read) and space. Also confirm that blob files work with shared-storage tiering (`CreateOnShared`).
Consider running this together with the memtable benchmarks in [prod-readiness 011](../../prod-readiness/issues/011-pebble-memtable-size.md).

## Comments
