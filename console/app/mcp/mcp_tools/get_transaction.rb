# frozen_string_literal: true

module McpTools
  # One Transaction's breakdown, as its page shows it.
  class GetTransaction < Base
    description <<~TEXT.squish
      Breaks one of a Project's Transactions down over a range, and links to its page:
      summary, its numbers as list_transactions gives them;
      releases, its p50_us and p95_us by release, over the range and at points bucket_seconds apart, to see which release made it slower;
      ops, its Spans' self time (a Span's duration less its children's) by op, as a share of its time and on average a request;
      groups, the same by group, an op and a description with literals replaced (normalised), with one description as sent;
      repeats, groups a request ran more than once, such as an N+1 query, with the median and most times a request
      that ran it did, and how many sampled requests did;
      slowest, its slowest sampled requests' trace ids, durations, statuses and releases.
      Shares and averages count each sampled request as 1 / its sample rate.
      list_transactions lists the names.
    TEXT
    read_only!
    takes_project
    argument :name, { type: "string", description: "The Transaction's name, as list_transactions gives it, such as OrdersController#show." }, required: true
    argument :range, ListTransactions::PERFORMANCE_RANGE
    argument :from, FROM
    argument :to, TO
    argument :environment, ListTransactions::ENVIRONMENT

    def run
      range = TransactionsView.range(range: arguments[:range], from: arguments[:from], to: arguments[:to])
      view = TransactionView.new(project: project, user: user, name: arguments[:name], range: range, environment: arguments[:environment])
      answer = view.to_h
      raise ToolError, "#{view.event_store[:error]}. Try again shortly." unless view.event_store[:available]

      json(answer, link: links.transaction(project, view.name, range: range, environment: view.environment))
    end
  end
end
