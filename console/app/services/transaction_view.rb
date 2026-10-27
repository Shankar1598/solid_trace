# frozen_string_literal: true

# One Transaction as a user sees it on its page: its numbers over a range,
# its p50 and p95 by release, where its time goes by Span op and by group
# (op and description with literals replaced), the groups a request repeats
# with their median and maximum repeats, and its slowest traces. Made here
# once, so the page and the MCP server's get_transaction can't drift.
#
# Shares and averages are weighted by sample rate, as throughput is; the
# percentiles are of the sampled requests. When EventStore can't answer,
# the lists are empty and event_store says why.
class TransactionView
  include EventStoreView

  attr_reader :project, :name, :range, :environment

  # range is a McpTools::TimeRange, as TransactionsView.range reads it;
  # environment nil reads every one.
  def initialize(project:, name:, range:, environment: nil)
    @project = project
    @name = name.to_s
    @range = range
    @environment = environment.presence
  end

  def to_h
    found = answer || {}
    {
      project: project_ref(project), name: name, label: TransactionsView.label(name),
      range: TransactionsView.range_h(range), environment: environment, event_store: event_store,
      summary: answer && TransactionsView.numbers(answer),
      bucket_seconds: found["bucket_seconds"],
      releases: Array(found["releases"]), ops: Array(found["ops"]), groups: Array(found["groups"]),
      repeats: Array(found["repeats"]), slowest: Array(found["slowest"]),
    }
  end

  private

  def answer
    event_store_answer("Transaction") do
      EventStore.transaction!(project_id: project.id, name: name, from: range.from, to: range.to,
        environment: environment, read_timeout: TransactionsView::TIMEOUT_SECONDS)
    end
  end
end
