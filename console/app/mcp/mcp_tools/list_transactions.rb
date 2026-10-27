# frozen_string_literal: true

module McpTools
  # A Project's Transactions by name, as its Performance page lists them.
  class ListTransactions < Base
    # The range argument of the Performance tools, whose default is a day.
    PERFORMANCE_RANGE = {
      type: "string",
      description: "How far back from now to read: a whole number of m, h or d, such as 1h, 24h or 7d. " \
                   "24h when neither range nor from and to is given.",
    }.freeze
    ENVIRONMENT = { type: "string", description: "The environment to read, such as production. Every one when left out." }.freeze

    description <<~TEXT.squish
      Lists a Project's Transactions, the requests and jobs its Sentry SDKs traced, one row per Transaction name,
      the most time consuming first, over a range, and links to its Performance page.
      Each row has sampled, how many requests were stored; throughput_per_minute, an estimate that counts each
      sampled request as 1 / its sample rate; p50_us, p95_us and p99_us of the sampled durations in microseconds;
      error_rate, the weighted share of requests whose status was sent and isn't ok;
      and time_consumed_us, the weighted sum of their durations, an estimate too.
      previous has the same numbers for the period of the same length before the range, null when none was sampled,
      and change each number's change from it: a share of the previous number (0.5 is 50% more, null from 0),
      and the error rate's difference.
      Transactions named by their raw URL are one row, <unparameterised>, labelled Unparameterised.
      environments lists the range's environments. get_transaction breaks one name down.
    TEXT
    read_only!
    takes_project
    argument :range, PERFORMANCE_RANGE
    argument :from, FROM
    argument :to, TO
    argument :environment, ENVIRONMENT

    def run
      view = TransactionsView.new(project: project, range: performance_range, environment: arguments[:environment])
      answer = view.to_h
      raise ToolError, "#{view.event_store[:error]}. Try again shortly." unless view.event_store[:available]

      json(answer, link: links.performance(project, range: performance_range, environment: view.environment))
    end

    private

    def performance_range
      @performance_range ||= TransactionsView.range(range: arguments[:range], from: arguments[:from], to: arguments[:to])
    end
  end
end
