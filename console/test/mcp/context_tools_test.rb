# frozen_string_literal: true

require "test_helper"

# whoami and list_projects: who the agent acts as, and where.
class McpContextToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
  end

  test "whoami answers the user and their role in each of their Organizations, linking to User settings" do
    beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: beta, user: @user, role: :member)
    left = create(:organization, name: "Left", slug: "left")
    create(:organization_user, organization: left, user: @user).discard!

    answer = call_tool(:whoami, user: @user)

    assert_not answer.error?
    assert_equal({ "user" => { "name" => "Ada", "email" => "ada@example.com" },
                   "organizations" => [ { "slug" => "acme", "name" => "Acme", "role" => "admin" },
                                        { "slug" => "beta", "name" => "Beta", "role" => "member" } ] }, answer.json)
    assert_equal "http://console.test/user_settings", answer.link
  end

  test "list_projects answers the Organization's Projects by name, linking to its Projects page" do
    create(:project, organization: @acme, name: "Web", slug: "web", log_retention_days: 7, log_file_order: [])
    create(:project, organization: @acme, name: "Api", slug: "api")
    create(:project, organization: create(:organization, slug: "theirs"), name: "Secret", slug: "secret")

    answer = call_tool(:list_projects, user: @user)

    assert_not answer.error?
    projects = answer.json["projects"]
    assert_equal "acme", answer.json["organization"]
    assert_equal [ "api", "web" ], projects.map { |project| project["slug"] }
    assert_equal [ 30, 7 ], projects.map { |project| project["log_retention_days"] }
    assert_equal [ [ "service", "file" ], [] ], projects.map { |project| project["log_file_order"] }
    assert_equal "http://console.test/acme/projects", answer.link
  end

  test "list_projects in one of several Organizations needs its slug" do
    beta = create(:organization, slug: "beta")
    create(:organization_user, organization: beta, user: @user)
    create(:project, organization: beta, name: "Shop", slug: "shop")

    assert call_tool(:list_projects, user: @user).error?
    assert_equal [ "shop" ], call_tool(:list_projects, user: @user, organization: "beta").json["projects"].map { |p| p["slug"] }
  end
end
