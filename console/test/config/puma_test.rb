# frozen_string_literal: true

require "test_helper"
require "puma/configuration"

# A Dashboard runs up to MAX_IN_FLIGHT charts at once, and each run holds a
# Puma thread while it waits on EventStore (up to about 35 s). Production must
# have threads to spare beyond one Dashboard, or a single viewer stalls every
# other Console request.
class PumaTest < ActiveSupport::TestCase
  FETCH_QUEUE = Rails.root.join("app/frontend/lib/dashboards/fetchQueue.ts")

  test "production Puma has more threads than one Dashboard runs at once" do
    max_in_flight = FETCH_QUEUE.read[/export const MAX_IN_FLIGHT = (\d+)/, 1].to_i
    assert_operator max_in_flight, :>, 0

    assert_operator production_threads, :>=, 2 * max_in_flight
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
