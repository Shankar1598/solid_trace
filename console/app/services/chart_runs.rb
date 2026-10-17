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

  # Puma threads no chart run may take.
  SPARE_THREADS = 3

  @lock = Mutex.new
  @running = 0

  class << self
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
