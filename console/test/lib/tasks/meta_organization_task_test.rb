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
