# frozen_string_literal: true

# A Project's Transactions as a user sees them on Performance: one row per
# Transaction name over a range, in one environment or every one, with its
# throughput, p50, p95 and p99, error rate and time consumed, and each
# number's change from the period of the same length before. Made here once,
# so the page and the MCP server's list_transactions can't drift.
#
# Throughput and time consumed are estimates: EventStore counts each sampled
# request as 1 / its sample rate. Transactions named by their raw URL are one
# row, Unparameterised. When EventStore can't answer, there are no rows and
# event_store says why.
class TransactionsView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # The range when none is given.
  DEFAULT_SPAN = "24h"
  # The name EventStore stores Transactions named by their raw URL under.
  UNPARAMETERISED = "<unparameterised>"
  # The numbers each row has, and whose change from the previous period it
  # shows.
  NUMBERS = %w[throughput_per_minute p50_us p95_us p99_us error_rate time_consumed_us].freeze

  # The range a page's URL or a tool's arguments name: a span back from now,
  # 24h without one, or from and to. Raises McpTools::ToolError, saying why,
  # for one it can't read.
  def self.range(range: nil, from: nil, to: nil)
    range = DEFAULT_SPAN if range.blank? && from.blank? && to.blank?
    McpTools::TimeRange.parse(range: range, from: from, to: to)
  end

  # How a Transaction name reads: Unparameterised for the raw URLs'.
  def self.label(name)
    name == UNPARAMETERISED ? "Unparameterised" : name
  end

  # A period's numbers as EventStore answers them, with throughput a minute.
  def self.numbers(answer)
    answer.slice("sampled", "p50_us", "p95_us", "p99_us", "error_rate", "time_consumed_us")
      .merge("throughput_per_minute" => answer["throughput"].to_f * 60)
  end

  def self.range_h(range)
    { span: range.span, from: range.from.utc.iso8601(3), to: range.to.utc.iso8601(3) }
  end

  attr_reader :project, :range, :environment

  # range is a McpTools::TimeRange, as TransactionsView.range reads it;
  # environment nil reads every one.
  def initialize(project:, range:, environment: nil)
    @project = project
    @range = range
    @environment = environment.presence
  end

  def to_h
    {
      project: project_ref(project), range: self.class.range_h(range), environment: environment,
      event_store: event_store, environments: Array(answer&.dig("environments")), transactions: transactions,
    }
  end

  # A row per Transaction name, the most time consuming first.
  def transactions
    Array(answer&.dig("transactions")).map do |t|
      current = self.class.numbers(t)
      previous = t["previous"] && self.class.numbers(t["previous"])
      { name: t["name"], label: self.class.label(t["name"]), **current.symbolize_keys, previous: previous, change: change(current, previous) }
    end
  end

  private

  # Each number's change from the previous period's: a share of it, nil from
  # 0, and the error rate's difference in points.
  def change(current, previous)
    return nil unless previous

    NUMBERS.to_h do |number|
      now, before = current[number].to_f, previous[number].to_f
      [ number, number == "error_rate" ? now - before : (before.zero? ? nil : (now - before) / before) ]
    end
  end

  def answer
    event_store_answer("Transactions") do
      EventStore.transactions!(project_id: project.id, from: range.from, to: range.to, environment: environment,
        read_timeout: TIMEOUT_SECONDS)
    end
  end
end
