# frozen_string_literal: true

require "test_helper"
require "rake"

# The operator grants and removes instance admin: there is no UI for it yet.
class InstanceAdminsTaskTest < ActiveSupport::TestCase
  setup do
    Rails.application.load_tasks unless Rake::Task.task_defined?("instance_admins:grant")
  end

  test "the operator lists the instance admins" do
    create(:user, email: "grace@example.com", instance_admin: true)
    create(:user, email: "ada@example.com")

    out = run_task("instance_admins:list")

    assert_includes out, "grace@example.com"
    assert_not_includes out, "ada@example.com"
  end

  test "without instance admins, the list says so" do
    assert_includes run_task("instance_admins:list"), "No instance admins"
  end

  test "the operator grants instance admin by email, in any case" do
    ada = create(:user, email: "ada@example.com")

    out = run_task("instance_admins:grant", "EMAIL" => " Ada@Example.com ")

    assert ada.reload.instance_admin?
    assert_includes out, "ada@example.com"
  end

  test "the operator revokes instance admin" do
    grace = create(:user, email: "grace@example.com", instance_admin: true)

    out = run_task("instance_admins:revoke", "EMAIL" => "grace@example.com")

    assert_not grace.reload.instance_admin?
    assert_includes out, "No instance admins"
  end

  test "an unknown or missing email is rejected and changes nothing" do
    create(:user, email: "ada@example.com")

    error = assert_raises(SystemExit) { run_task("instance_admins:grant", "EMAIL" => "nobody@example.com") }
    assert_equal "No user has the email nobody@example.com.", error.message
    assert_raises(SystemExit) { run_task("instance_admins:revoke", "EMAIL" => "") }
    assert_not User.exists?(instance_admin: true)
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
