# 06: Compared with other tools

Spec: [../spec.md](../spec.md)

**What to build:** A **Compared with other tools** section on the Overview shows what the range's volume would cost at Datadog's, Better Stack's and Axiom's list prices, plus GCP's internet egress for sending the bytes on the wire to such a tool. A 7-day or Today range is scaled to 30 days and labelled so. The section says these are list prices before discounts, an estimate, and gives the prices' date and a link to each pricing page. There's no saving line: the install's own cost is left out.

End to end:

- **Research the prices first, with sources.** Don't write them from memory. That's each tool's pricing model over GB ingested, Logs ingested and Retention days, and GCP's internet egress per GB for this volume from a GCP region. Record the sources and the date checked in this ticket's Answer.
- Put them in one small, dated table in Console's code, with each tool's formula, pricing URL and checked date. Nothing is fetched at runtime.
- The answer builder prices the range's GB received, Logs received and the Projects' Retention, scaled to 30 days, with egress on bytes on the wire. A Project without a Retention uses the instance default.
- `get_overview` answers the estimates too.

**Blocked by:** 01

**Status:** resolved

- [x] The prices, their sources and the date checked are recorded in this ticket's Answer
- [x] Given a range's volume and Retention, the builder answers each tool's estimate and the egress from the price table, scaled to 30 days (Rails test)
- [x] `get_overview` includes the estimates with the prices' date (MCP tool test)
- [x] 01's Playwright flow also sees the section with all three tools and the egress

## Answer

Built on `overview/06-compared-with-other-tools`, merged into `feature/overview` as `2559fe6` (2026-10-24). The price table is `Overview::OtherTools`.

Tests run on the merge:
- Rails: seven Overview controller and MCP files, 32 runs, 0 failures.
- `tsc` is clean, and Go `./handler` passed.
- The flow passed on the implementer's own stack.

Open:
- **Axiom's compression.** Axiom's storage assumes 10× compression; the code review fix should use the Organization's measured ratio instead.
- **Rounding.** Dollars round to cents, so tiny dev volumes show $0.00.

### Prices (copied from the implementer's research)

Checked on **2026-10-24**, each from the vendor's own pricing page (fetched that day; GCP's table read from the page's embedded per-region data). All USD, list prices before discounts, no annual commitment. In Console: `console/app/services/overview/other_tools.rb` (`Overview::OtherTools::PRICES`, `CHECKED_ON`).

## The volume each formula prices

The range's totals scaled to 30 days (`× 30 / range days`; Today is 1 day, 7 days is 7):

- **GB** = Logs' bytes received (decoded payload, `received.bytes`) / 10^9, per Project. The vendors bill decimal GB of what they ingest; that's the decoded bytes, not the compressed bytes on the wire.
- **Logs** = Logs received (`received.logs`), per Project.
- **R** = the Project's Retention in days; the instance default (`InstanceSetting.default_log_retention_days`) when it has none.
- **Wire GiB** = bytes on the wire (`received.bytes_on_wire`, the request bodies as sent, often gzip) / 2^30, for the egress.

Each tool prices each Project at its own Retention, summed; allowances and base fees apply once to the total.

## Datadog Log Management

- Source: https://www.datadoghq.com/pricing/list/ (also https://www.datadoghq.com/pricing/?product=log-management)
- Plan: **Standard indexing, billed on-demand** (the page's three columns are Billed Annually / Billed Month-to-month / Billed On-demand). On-demand is what a small team pays with no contract; month-to-month still needs a committed volume.
- Ingestion: "Logs - Ingestion, Per ingested logs (1GB), per month": $0.10 (all three columns).
- Indexing, "Per 1M indexed logs (N-day retention), per month":

  | Retention | Annual | Month-to-month | On-demand (used) |
  |---|---|---|---|
  | 3 days | $1.06 | $1.27 | **$1.59** |
  | 7 days | $1.27 | $1.52 | **$1.91** |
  | 15 days | $1.70 | $2.04 | **$2.55** |
  | 30 days | $2.50 | $3.00 | **$3.75** |

- Formula: Σ over Projects of `GB × 0.10 + Logs / 10^6 × index(R)`, where `index(R)` is the shortest listed retention ≥ R.
- Assumptions: every Log is indexed (no exclusion filters, no Flex Logs). Datadog lists nothing above 30 days ("Contact Us"), so R > 30 is priced at the 30-day rate and the estimate says it's at least that (`beyond_list: true`).

## Better Stack Logs

- Source: https://betterstack.com/pricing (and its Markdown copy https://betterstack.com/pricing.md)
- Plan: **pay-as-you-go Telemetry, Europe region**. Ingestion "$0.10 per GB", Retention "$0.05 per GB per month" (US: $0.15 / $0.08; Singapore $0.35 / $0.18). Team members with Telemetry access are $0/month; the $34 Responder seat is for incident management, not needed for Logs. Europe is the region nearest an install on GCP in Europe, and it's the cheapest. The bundles (Nano $30/mo for 40 GB each of Logs, traces and metrics with 30-day retention, up to Tera $500/mo for 700 GB) cost more than pay-as-you-go for Logs alone: 40 GB at 30 days is $4 + $2 = $6 pay-as-you-go.
- Formula: Σ over Projects of `GB × 0.10 + GB × R / 30 × 0.05`.
- Assumptions: retention is billed on GB held, so a month's GB kept R days averages `GB × R / 30` GB-months at steady state. The page doesn't say whether retained GB is compressed; Better Stack's metrics billing doc measures "the uncompressed size ... as stored", so ingested GB is used. The free 3 GB (kept 3 days) isn't subtracted.

## Axiom

- Source: https://axiom.co/pricing (allowances also on https://axiom.co/docs/reference/limits)
- Plan: **Axiom Cloud**, "$25/month platform fee + usage. No minimum commitment." The Personal plan ($0) caps loading at 500 GB/month and retention at 30 days and is for personal projects, so a team uses Axiom Cloud.
- Data loading, per GB per month (graduated): 0–999 GB included; 1,000–99,999 GB $0.12; 100,000–499,999 $0.10; 500,000–999,999 $0.085; 1,000,000–4,999,999 $0.07; 5,000,000+ $0.06.
- Storage, compressed, per GB per month: Frequent Access $0.03; first 100 GB free (Infrequent Access $0.0163 after 30 days unread and Archive $0.00522 after 90 aren't used).
- Query compute: 100 GB-hours/month included, then $0.20 per GB-hour.
- Formula: `25 + loading(ΣGB) + 0.03 × max(0, Σ(GB × R / 30) / 10 − 100)`.
- Assumptions: storage is compressed bytes; Axiom publishes no ratio for this (its blog claims 25–50×, its architecture doc "25-50% reduction" at ingest), so a 10× ratio is assumed. Queries stay inside the 100 included GB-hours (they depend on use, not volume). All data stays in Frequent Access.

## GCP internet egress

- Source: https://cloud.google.com/vpc/network-pricing ("Premium Tier pricing", "Network (data transfer out)", region selector set to Netherlands (europe-west4); Belgium (europe-west1) and Frankfurt (europe-west3) show the same table).
- Tier: **Premium Tier** (GCP's default) from a European region, to Europe (Datadog EU1, Better Stack Europe and Axiom's EU region are there), per GiB per month per account, graduated:
  - 0–1,024 GiB: $0.12
  - 1,024–10,240 GiB: $0.11
  - 10,240 GiB and above: $0.08 (to North America $0.085; the first two tiers are the same)
- European source regions have no free first GiB (the table for us-central1 does).
- Formula: `graduated(Wire GiB)` over the tiers above.
- Assumptions: the bytes on the wire are what the shipper sends today, so a tool's own agent sending the same Logs is assumed to compress about as well. Egress is for one tool's worth of sending.

## Not in the estimate

The install's own cost (the user's choice, 2026-10-24), discounts and commitments, taxes, Datadog Flex Logs and archives, Axiom's query compute beyond the allowance.
