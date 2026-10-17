# frozen_string_literal: true

require "test_helper"

class ChartRunsTest < ActiveSupport::TestCase
  test "leaves spare Puma threads free of chart runs, and runs one on a Console of few threads" do
    assert_equal 7, ChartRuns.limit(10)
    assert_equal 1, ChartRuns.limit(4)
    assert_equal 1, ChartRuns.limit(1)
  end

  test "refuses a run past the limit, and frees a slot as each run ends" do
    holding_every_chart_run do
      assert_raises(ChartRuns::Busy) { ChartRuns.hold { flunk "ran past the limit" } }
    end

    assert_equal :ran, ChartRuns.hold { :ran }
  end

  test "frees the slot of a run that fails" do
    ChartRuns.limit.times do
      assert_raises(EventStore::Unavailable) { ChartRuns.hold { raise EventStore::Unavailable, "down" } }
    end

    holding_every_chart_run { pass }
  end

  test "counts the runs of every thread" do
    started = Queue.new
    finish = Queue.new
    threads = Array.new(ChartRuns.limit) do
      Thread.new { ChartRuns.hold { started << true; finish.pop } }
    end
    ChartRuns.limit.times { started.pop }

    assert_raises(ChartRuns::Busy) { ChartRuns.hold { flunk "ran past the limit" } }
  ensure
    threads&.each { finish << true }
    threads&.each(&:join)
  end

  private
    def holding_every_chart_run(left = ChartRuns.limit, &block)
      left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
    end
end
