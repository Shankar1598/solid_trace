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

  test "validates name and slug" do
    org = Organization.new(name: "", slug: "")
    assert_not org.valid?
    assert_includes org.errors[:name], "can't be blank"
    assert_includes org.errors[:slug], "can't be blank"

    org.slug = "invalid slug"
    assert_not org.valid?
    assert_includes org.errors[:slug], "only allows lowercase letters, numbers, and hyphens"

    create(:organization, slug: "existing")
    org.slug = "existing"
    assert_not org.valid?
    assert_includes org.errors[:slug], "has already been taken"
  end
end
