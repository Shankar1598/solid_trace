# frozen_string_literal: true

require "test_helper"

# Solid Queue reads config/recurring.yml as Solid Queue's supervisor does:
# when the file has a block for the environment it takes only that block,
# so every task must be listed under each environment that runs it.
class RecurringTest < ActiveSupport::TestCase
  FILE = Rails.root.join("config/recurring.yml")
  TASKS = %i[event_store_message_processor monitor_scheduler maintenance_notification_cleanup].freeze

  test "development and production both run every task, each on a schedule" do
    %w[development production].each do |env|
      tasks = tasks_for(env)
      TASKS.each do |task|
        assert tasks.dig(task, :schedule).present?, "#{task} has no schedule in #{env}"
      end
    end
  end

  test "only production clears finished Solid Queue jobs" do
    assert tasks_for("production").key?(:clear_solid_queue_finished_jobs)
    assert_not tasks_for("development").key?(:clear_solid_queue_finished_jobs)
  end

  private

  def tasks_for(env)
    SolidQueue::Configuration.new(recurring_schedule_file: FILE).send(:config_from, FILE, env: env)
  end
end
