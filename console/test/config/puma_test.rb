# frozen_string_literal: true

require "test_helper"
require "puma/configuration"

# A Dashboard runs up to MAX_IN_FLIGHT charts at once, and an explorer run is
# a Query and a histogram at once. Each run holds a Puma thread until
# EventStore answers, even after the browser aborts it, so ChartRuns caps the
# runs under the thread count (see its test). Production must have room under
# that cap for one Dashboard's runs and one explorer run at once, or a single
# viewer sees Console as busy.
class PumaTest < ActiveSupport::TestCase
  FETCH_QUEUE = Rails.root.join("app/frontend/lib/dashboards/fetchQueue.ts")
  EXPLORER_RUNS_AT_ONCE = 2

  test "production Puma allows one Dashboard's and one explorer's runs at once, under the cap that keeps threads spare" do
    max_in_flight = FETCH_QUEUE.read[/export const MAX_IN_FLIGHT = (\d+)/, 1].to_i
    assert_operator max_in_flight, :>, 0

    assert_operator ChartRuns.limit(production_threads), :>=, max_in_flight + EXPLORER_RUNS_AT_ONCE
    assert_operator production_threads - ChartRuns.limit(production_threads), :>=, ChartRuns::SPARE_THREADS
  end

  test "each production database has a connection for every Puma thread" do
    ActiveRecord::Base.configurations.configs_for(env_name: "production").each do |config|
      assert_operator config.configuration_hash[:max_connections].to_i, :>=, production_threads, config.name
    end
  end

  private
    def production_threads
      previous = ENV.delete("RAILS_MAX_THREADS")
      config = Puma::Configuration.new({}, {}) { |user, _default| user.load Rails.root.join("config/puma.rb").to_s }
      config.clamp
      config.options[:max_threads].to_i
    ensure
      ENV["RAILS_MAX_THREADS"] = previous if previous
    end
end
