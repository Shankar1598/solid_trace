# frozen_string_literal: true

class Overview
  # The Overview's Compared with other tools: what the range's volume would
  # cost a month at Datadog's, Better Stack's and Axiom's list prices, its
  # Spans at Datadog APM's, and GCP's internet egress for sending the bytes
  # on the wire to such a tool.
  #
  # The volume is the range's scaled to 30 days by the seconds the range has
  # had so far (Today at 06:00 is 6 hours × 30 days / 6 hours, not a whole
  # day's), or by its whole days before any has passed: each Project's GB and Logs
  # received at its Retention (the instance's when it has none), and the
  # bytes on the wire. The prices are PRICES, checked by hand on CHECKED_ON
  # from each pricing page and updated in a release when stale; nothing is
  # fetched. They're list prices before discounts, with no commitment; the
  # install's own cost is left out.
  class OtherTools
    CHECKED_ON = "2026-10-24"
    DAYS = 30

    GB = 10**9
    GIB = 2**30

    # Each tool's list prices, in USD a month. A tiered price is a list of
    # [up to this many units, price per unit]; the last tier has no limit.
    PRICES = {
      # Standard indexing, billed on-demand (no contract). Every Log is
      # indexed, at the shortest listed retention as long as the Project's;
      # past 30 days Datadog lists none, so 30 days' is the floor.
      datadog: {
        name: "Datadog", plan: "Log Management, Standard indexing, on-demand",
        url: "https://www.datadoghq.com/pricing/list/",
        ingest_per_gb: 0.10,
        index_per_million: { 3 => 1.59, 7 => 1.91, 15 => 2.55, 30 => 3.75 },
      },
      # Pay-as-you-go Telemetry in Europe. Retention is billed per GB held a
      # month: a month's GB kept R days is GB × R / 30 GB-months.
      better_stack: {
        name: "Better Stack", plan: "Logs, pay-as-you-go, Europe",
        url: "https://betterstack.com/pricing",
        ingest_per_gb: 0.10, retention_per_gb_month: 0.05,
      },
      # Axiom Cloud: the platform fee, data loading past the 1,000 GB
      # included, and compressed storage past the 100 GB free, at
      # ClickHouse's measured compression (raw bytes over stored), or 10×
      # when nothing is stored to measure. Queries are assumed to stay in
      # the included compute.
      axiom: {
        name: "Axiom", plan: "Axiom Cloud",
        url: "https://axiom.co/pricing",
        platform_fee: 25.0,
        loading_per_gb: [ [ 1_000, 0 ], [ 100_000, 0.12 ], [ 500_000, 0.10 ], [ 1_000_000, 0.085 ], [ 5_000_000, 0.07 ], [ nil, 0.06 ] ],
        compression: 10, free_storage_gb: 100, storage_per_gb_month: 0.03,
      },
    }.freeze

    # Datadog APM, on-demand (no contract), checked on the list page above
    # and the APM billing docs (allotments: "1 million Indexed Spans and 150
    # GB of Ingested Spans included per month with every APM host",
    # https://docs.datadoghq.com/account_management/billing/apm_tracing_profiler/).
    # SolidTrace doesn't know the hosts, so it takes one APM host per
    # Project that sent Spans, and pools the allotments across them as
    # Datadog does. Every Span is indexed, as SolidTrace keeps every Span it
    # receives, at the shortest listed retention as long as the Project's
    # trace Retention; past 30 days Datadog lists none, so 30 days' is the
    # floor. The ingested GB are the Transactions' bytes as received.
    APM = {
      key: "datadog_apm", name: "Datadog", plan: "APM, on-demand",
      url: "https://www.datadoghq.com/pricing/list/",
      per_host: 36.0, included_gb_per_host: 150, included_indexed_per_host: 1_000_000,
      ingest_per_gb: 0.10,
      index_per_million: { 7 => 1.91, 15 => 2.55, 30 => 3.75 },
    }.freeze

    # Premium Tier internet egress from a European region to Europe, per GiB.
    EGRESS = {
      name: "GCP internet egress", plan: "Premium Tier, from a European region",
      url: "https://cloud.google.com/vpc/network-pricing",
      per_gib: [ [ 1_024, 0.12 ], [ 10_240, 0.11 ], [ nil, 0.08 ] ],
    }.freeze

    # projects: [[received {logs, bytes}, Retention days]]; spans: [[spans
    # {received, received_bytes}, trace Retention days]]; days: the range's
    # whole days; seconds: those it has had so far; stored: the band's stored
    # {bytes, raw_bytes}, whose ratio Axiom's storage is compressed by.
    def initialize(projects:, bytes_on_wire:, days:, seconds:, stored: {}, spans: [])
      raw, compressed = stored["raw_bytes"].to_i, stored["bytes"].to_i
      @compression = compressed.positive? && raw.positive? ? { ratio: raw.to_f / compressed, measured: true } : nil
      @days, @seconds = days, seconds.to_i
      scale = @seconds.positive? ? DAYS * 86_400.0 / @seconds : DAYS.to_f / days
      @projects = projects.map do |received, retention_days|
        { gb: received["bytes"].to_i * scale / GB, logs: received["logs"].to_i * scale, retention_days: retention_days }
      end
      @bytes = projects.sum { |received, _| received["bytes"].to_i } * scale
      @bytes_on_wire = bytes_on_wire.to_i * scale
      @spans = spans.filter_map do |received, retention_days|
        next unless received["received"].to_i.positive?

        { spans: received["received"].to_i * scale, gb: received["received_bytes"].to_i * scale / GB, retention_days: retention_days }
      end
    end

    def to_h
      {
        checked_on: CHECKED_ON,
        volume: { days: @days, seconds: @seconds, scaled: @seconds != DAYS * 86_400, logs: @projects.sum { |p| p[:logs] }.round, bytes: @bytes.round,
                  bytes_on_wire: @bytes_on_wire.round },
        tools: [ datadog, better_stack, axiom ],
        egress: tool(EGRESS, tiered(@bytes_on_wire / GIB, EGRESS[:per_gib])),
        apm: apm,
      }
    end

    private

    def datadog
      prices = PRICES[:datadog]
      listed = prices[:index_per_million]
      cost = @projects.sum do |p|
        days = listed.keys.find { |d| d >= p[:retention_days] } || listed.keys.last
        p[:gb] * prices[:ingest_per_gb] + p[:logs] / 1_000_000 * listed.fetch(days)
      end
      tool(prices, cost).merge(beyond_list: @projects.any? { |p| p[:retention_days] > listed.keys.last })
    end

    # Datadog APM for the Spans, nil when none arrived.
    def apm
      return if @spans.empty?

      hosts = @spans.size
      listed = APM[:index_per_million]
      spans = @spans.sum { |p| p[:spans] }
      gb = @spans.sum { |p| p[:gb] }
      # The indexed allotment comes off each Project's Spans in proportion.
      billable = spans.positive? ? [ spans - hosts * APM[:included_indexed_per_host], 0 ].max / spans : 0
      indexed = @spans.sum do |p|
        days = listed.keys.find { |d| d >= p[:retention_days] } || listed.keys.last
        p[:spans] * billable / 1_000_000 * listed.fetch(days)
      end
      cost = hosts * APM[:per_host] + [ gb - hosts * APM[:included_gb_per_host], 0 ].max * APM[:ingest_per_gb] + indexed
      { key: APM[:key], name: APM[:name], plan: APM[:plan], url: APM[:url], monthly_usd: cost.round(2),
        per_host_usd: APM[:per_host], volume: { spans: spans.round, bytes: (gb * GB).round, hosts: hosts },
        beyond_list: @spans.any? { |p| p[:retention_days] > listed.keys.last } }
    end

    def better_stack
      prices = PRICES[:better_stack]
      tool(prices, @projects.sum do |p|
        p[:gb] * prices[:ingest_per_gb] + gb_months(p) * prices[:retention_per_gb_month]
      end)
    end

    def axiom
      prices = PRICES[:axiom]
      compression = @compression || { ratio: prices[:compression].to_f, measured: false }
      stored = @projects.sum { |p| gb_months(p) } / compression[:ratio]
      tool(prices, prices[:platform_fee] + tiered(@projects.sum { |p| p[:gb] }, prices[:loading_per_gb]) +
        [ stored - prices[:free_storage_gb], 0 ].max * prices[:storage_per_gb_month])
        .merge(compression: { ratio: compression[:ratio].round(1), measured: compression[:measured] })
    end

    # The GB a month a Project holds when it keeps a month's GB R days.
    def gb_months(project)
      project[:gb] * project[:retention_days] / DAYS
    end

    # What amount units cost over graduated tiers, each tier's units at its
    # own price.
    def tiered(amount, tiers)
      floor = 0
      tiers.sum do |limit, price|
        units = [ (limit || amount), amount ].min - floor
        floor = limit || amount
        units.positive? ? units * price : 0
      end
    end

    def tool(prices, cost)
      { key: PRICES.key(prices)&.to_s, name: prices[:name], plan: prices[:plan], url: prices[:url], monthly_usd: cost.round(2) }.compact
    end
  end
end
