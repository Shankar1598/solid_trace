# frozen_string_literal: true

# The runs this Console process is waiting on EventStore for: Dashboard
# charts' data, the chart editor's and a Monitor's preview, and the
# explorer's runs (its Query, histogram and SQL). A run holds its Puma thread
# until EventStore answers, up to EventStoreProxy::RUN_TIMEOUT_SECONDS and a
# little more, even after the browser has aborted it, since Puma doesn't stop
# a request whose browser went away. So a range change or Refresh sends a
# Dashboard's runs again while the old ones still wait, and each Builder edit
# in the explorer sends a Query and a histogram while the last ones still
# wait: neither the browser's limit of 4 at once nor the explorer's abort
# bounds the threads. This does: the runs are capped SPARE_THREADS under
# Puma's thread count, and a run past the cap is refused at once as Busy,
# leaving those threads for pages and sign-in however many Dashboards and
# explorers are open.
#
# The explorer shares the cap rather than having its own, so neither waits
# on a share the other isn't using: one Dashboard's runs and one explorer run
# fit under it together (test/config/puma_test.rb), and Busy means the
# threads really are taken.
class ChartRuns
  class Busy < StandardError; end

  # A chart's Query answered Logs, which can't be drawn, rather than a table.
  class NotATable < StandardError
    def initialize(message = "This Query answers Logs, not a table. End it with summarize, count or project to chart it.")
      super
    end
  end

  # Puma threads no chart run may take.
  SPARE_THREADS = 3

  @lock = Mutex.new
  @running = 0

  class << self
    # A chart's Query run on a Project from from to to, as one chart run that
    # EventStore stops after EventStoreProxy::RUN_TIMEOUT_SECONDS: EventStore's
    # answer, whose "table" is the table. A Dashboard's charts, the chart
    # editor's and a Monitor's preview and the MCP server's chart tools run
    # through it. parameters are the values of the Dashboard filters the
    # Query names. Raises NotATable for a Query that answers Logs, and Busy
    # past the limit.
    def run_chart(project, query, from:, to:, parameters: nil)
      answer = hold do
        EventStore.query_logs(project_id: project.id, query: query, from: from, to: to,
                              timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS, parameters: parameters)
      end
      raise NotATable unless answer.is_a?(Hash) && answer["table"].is_a?(Hash)

      answer
    end

    # The 100 most common values of a field in a Project's Logs from from to
    # to, most common first, as one chart run: [{value:, count:}], each value
    # as its text, a number as written and a bool as true or false. A Dashboard
    # filter's select of a field's values lists them.
    def field_values(project, field, from:, to:)
      name = bracketed(field)
      query = "#{bracketed(project.slug)} | where isnotnull(#{name}) | summarize count() by #{name} | sort by count_ desc | take 100"
      run_chart(project, query, from: from, to: to)["table"]["rows"].map do |value, count|
        { value: value.is_a?(String) ? value : value.to_json, count: count }
      end
    end

    # Runs the block as one chart run, or raises Busy when as many are
    # running as the limit allows.
    def hold
      raise Busy unless take

      begin
        yield
      ensure
        @lock.synchronize { @running -= 1 }
      end
    end

    # How many chart runs may wait on EventStore at once under a Puma of that
    # many threads. A Console of few threads still runs one.
    def limit(threads = puma_threads)
      [ threads - SPARE_THREADS, 1 ].max
    end

    private
      # A name as a Query writes it in brackets, such as ['o\'brien'].
      def bracketed(name)
        escaped = name.gsub("\\") { "\\\\" }.gsub("\n") { "\\n" }.gsub("\t") { "\\t" }.gsub("\r") { "\\r" }.gsub("'") { "\\'" }
        "['#{escaped}']"
      end

      def take
        @lock.synchronize do
          next false if @running >= limit

          @running += 1
        end
      end

      # The threads of the Puma serving this request, or, outside Puma (in
      # tests), the count config/puma.rb gives production.
      def puma_threads
        (defined?(Puma::Server) && Puma::Server.current&.max_threads) || ENV.fetch("RAILS_MAX_THREADS", 10).to_i
      end
  end
end
