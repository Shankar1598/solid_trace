# frozen_string_literal: true

class Overview
  # The Overview's Compared with other tools: what the range's volume would
  # cost a month at Datadog's, Better Stack's and Axiom's list prices, and
  # GCP's internet egress for sending the bytes on the wire to such a tool.
  #
  # The volume is the range's scaled to 30 days: each Project's GB and Logs
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
      # included, and compressed storage past the 100 GB free, assuming 10×
      # compression. Queries are assumed to stay in the included compute.
      axiom: {
        name: "Axiom", plan: "Axiom Cloud",
        url: "https://axiom.co/pricing",
        platform_fee: 25.0,
        loading_per_gb: [ [ 1_000, 0 ], [ 100_000, 0.12 ], [ 500_000, 0.10 ], [ 1_000_000, 0.085 ], [ 5_000_000, 0.07 ], [ nil, 0.06 ] ],
        compression: 10, free_storage_gb: 100, storage_per_gb_month: 0.03,
      },
    }.freeze

    # Premium Tier internet egress from a European region to Europe, per GiB.
    EGRESS = {
      name: "GCP internet egress", plan: "Premium Tier, from a European region",
      url: "https://cloud.google.com/vpc/network-pricing",
      per_gib: [ [ 1_024, 0.12 ], [ 10_240, 0.11 ], [ nil, 0.08 ] ],
    }.freeze

    # projects: [[received {logs, bytes}, Retention days]]; days: the range's.
    def initialize(projects:, bytes_on_wire:, days:)
      scale = DAYS.to_f / days
      @days = days
      @projects = projects.map do |received, retention_days|
        { gb: received["bytes"].to_i * scale / GB, logs: received["logs"].to_i * scale, retention_days: retention_days }
      end
      @bytes = projects.sum { |received, _| received["bytes"].to_i } * scale
      @bytes_on_wire = bytes_on_wire.to_i * scale
    end

    def to_h
      {
        checked_on: CHECKED_ON,
        volume: { days: @days, scaled: @days != DAYS, logs: @projects.sum { |p| p[:logs] }.round, bytes: @bytes.round,
                  bytes_on_wire: @bytes_on_wire.round },
        tools: [ datadog, better_stack, axiom ],
        egress: tool(EGRESS, tiered(@bytes_on_wire / GIB, EGRESS[:per_gib])),
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

    def better_stack
      prices = PRICES[:better_stack]
      tool(prices, @projects.sum do |p|
        p[:gb] * prices[:ingest_per_gb] + gb_months(p) * prices[:retention_per_gb_month]
      end)
    end

    def axiom
      prices = PRICES[:axiom]
      stored = @projects.sum { |p| gb_months(p) } / prices[:compression]
      tool(prices, prices[:platform_fee] + tiered(@projects.sum { |p| p[:gb] }, prices[:loading_per_gb]) +
        [ stored - prices[:free_storage_gb], 0 ].max * prices[:storage_per_gb_month])
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
