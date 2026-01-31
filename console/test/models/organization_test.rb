# frozen_string_literal: true

require "test_helper"

class OrganizationTest < ActiveSupport::TestCase
  test "has projects" do
    organization = create(:organization)
    project = create(:project, organization: organization)

    assert_includes organization.projects, project
  end

  test "has users through organization_users" do
    organization = create(:organization)
    user = create(:user)

    organization.users << user

    assert_includes organization.users, user
  end
end
