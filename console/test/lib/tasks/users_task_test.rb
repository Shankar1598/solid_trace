# frozen_string_literal: true

require "test_helper"
require "rake"

# The operator resets the Two-factor of a user who has lost every factor and
# every Recovery code: no Console page or MCP tool can.
class UsersTaskTest < ActiveSupport::TestCase
  setup do
    Rails.application.load_tasks unless Rake::Task.task_defined?("users:reset_two_factor")
  end

  test "the operator resets a user's Two-factor by email, in any case, and ends their Sessions" do
    ada = create(:user, email: "ada@example.com")
    ada.update!(authenticator_secret: ROTP::Base32.random)
    ada.gaining_two_factor(keep: nil) { ada.confirm_authenticator_app(ROTP::TOTP.new(ada.authenticator_secret).now) }
    ada.sessions.create!(user_agent: "Laptop", ip_address: "127.0.0.1")
    grace = create(:user, email: "grace@example.com")
    grace.sessions.create!(user_agent: "Phone", ip_address: "127.0.0.1")

    out = run_task("users:reset_two_factor", "EMAIL" => " Ada@Example.com ")

    ada.reload
    assert_not ada.two_factor?
    assert_equal 0, ada.recovery_codes.count
    assert_empty ada.sessions
    assert_equal 1, grace.sessions.count
    assert_equal "Two-factor is off for ada@example.com, and their Sessions have ended: they sign in with their password alone.\n", out
  end

  test "a user without Two-factor is reset all the same" do
    create(:user, email: "ada@example.com")

    assert_includes run_task("users:reset_two_factor", "EMAIL" => "ada@example.com"), "Two-factor is off for ada@example.com"
  end

  test "an unknown or missing email is rejected" do
    error = assert_raises(SystemExit) { run_task("users:reset_two_factor", "EMAIL" => "nobody@example.com") }
    assert_equal "No user has the email nobody@example.com.", error.message
    error = assert_raises(SystemExit) { run_task("users:reset_two_factor", "EMAIL" => "") }
    assert_equal "Give EMAIL=<a user's email>.", error.message
  end

  private

  def run_task(name, env = {})
    previous = env.keys.index_with { |key| ENV[key] }
    env.each { |key, value| ENV[key] = value }
    task = Rake::Task[name]
    task.reenable
    out, _err = capture_io { task.invoke }
    out
  ensure
    previous.each { |key, value| ENV[key] = value }
  end
end
