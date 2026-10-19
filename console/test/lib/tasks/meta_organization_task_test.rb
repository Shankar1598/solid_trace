# frozen_string_literal: true

require "test_helper"
require "rake"

# db_prepare seeds the Meta Organization after db:prepare, with the project
# key deploy/install wrote to deploy/.env (ADR 0011).
class MetaOrganizationTaskTest < ActiveSupport::TestCase
  KEY = "0123456789abcdef0123456789abcdef"

  setup do
    Rails.application.load_tasks unless Rake::Task.task_defined?("meta_organization:seed")
  end

  test "on an empty database it makes the SolidTrace Organization and Project with the key" do
    seed

    organization = Organization.sole
    assert_equal "SolidTrace", organization.name
    project = organization.projects.sole
    assert_equal "SolidTrace", project.name
    assert_equal [ KEY ], project.project_keys.pluck(:public_key)
    assert_empty organization.users
  end

  test "on an install with users, the oldest user is its admin" do
    create(:user, email: "newer@example.com", created_at: 1.day.ago)
    oldest = create(:user, email: "oldest@example.com", created_at: 1.year.ago)

    seed

    membership = Organization.sole.organization_users.sole
    assert_equal oldest, membership.user
    assert membership.admin?
  end

  test "its Project keeps the instance's default Log Retention" do
    InstanceSetting.current.update!(default_log_retention_days: 45)

    seed

    assert_nil Project.sole.log_retention_days
  end

  test "run again, it makes nothing more" do
    seed
    seed

    assert_equal 1, Organization.count
    assert_equal 1, Project.count
  end

  test "a deleted Meta Organization is not made again" do
    seed
    Organization.sole.destroy!

    seed

    assert_empty Organization.all
  end

  test "a deleted Meta Project is not made again" do
    seed
    Project.sole.destroy!

    seed

    assert_empty Project.all
  end

  test "without the key, as in local development, it does nothing, and seeds once the key is set" do
    seed(nil)
    assert_empty Organization.all

    seed
    assert_equal [ "SolidTrace" ], Organization.pluck(:name)
  end

  private

  def seed(key = KEY)
    previous = ENV["META_PROJECT_KEY"]
    ENV["META_PROJECT_KEY"] = key
    task = Rake::Task["meta_organization:seed"]
    task.reenable
    capture_io { task.invoke }
  ensure
    ENV["META_PROJECT_KEY"] = previous
  end
end
