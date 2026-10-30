# frozen_string_literal: true

require "test_helper"

# whoami and list_projects: who the agent acts as, and where.
class McpContextToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    # EventStore has no volume of any Project, unless a test says otherwise.
    stub_request(:post, "#{EventStore.base_url}/volumes").to_return(status: 200, body: { projects: [] }.to_json)
  end

  test "whoami answers the user and their role in each of their Organizations, linking to User settings" do
    beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: beta, user: @user, role: :member)
    left = create(:organization, name: "Left", slug: "left")
    create(:organization_user, organization: left, user: @user).discard!

    answer = call_tool(:whoami, user: @user)

    assert_not answer.error?
    assert_equal({ "user" => { "name" => "Ada", "email" => "ada@example.com", "instance_admin" => false },
                   "organizations" => [ { "slug" => "acme", "name" => "Acme", "role" => "admin" },
                                        { "slug" => "beta", "name" => "Beta", "role" => "member" } ] }, answer.json)
    assert_equal "http://console.test/user_settings", answer.link
  end

  test "list_projects answers the Organization's Projects by name, linking to its Projects page" do
    create(:project, organization: @acme, name: "Web", slug: "web", description: "The storefront", log_retention_days: 7,
      trace_retention_days: 3)
    create(:project, organization: @acme, name: "Api", slug: "api")
    create(:project, organization: create(:organization, slug: "theirs"), name: "Secret", slug: "secret")

    answer = call_tool(:list_projects, user: @user)

    assert_not answer.error?
    projects = answer.json["projects"]
    assert_equal "acme", answer.json["organization"]
    assert_equal [ "api", "web" ], projects.map { |project| project["slug"] }
    assert_equal [ nil, "The storefront" ], projects.map { |project| project["description"] }
    assert_equal [ 30, 7 ], projects.map { |project| project["log_retention_days"] }
    assert_equal [ 14, 3 ], projects.map { |project| project["trace_retention_days"] }
    assert projects.none? { |project| project.key?("log_file_order") }, "every Project's Logs are sorted by Service and file"
    assert_equal "http://console.test/acme/projects", answer.link
  end

  test "list_projects answers each Project's volume over the last 24 hours, zeros for one with no Logs" do
    web = create(:project, organization: @acme, name: "Web", slug: "web")
    api = create(:project, organization: @acme, name: "Api", slug: "api")
    asked = stub_request(:post, "#{EventStore.base_url}/volumes")
      .with(body: { project_ids: [ api.id, web.id ] }.to_json)
      .to_return(status: 200, body: { projects: [
        { project_id: api.id, logs_per_hour: 0, bytes_per_hour: 0, files: 0 },
        { project_id: web.id, logs_per_hour: 3_700_000.4, bytes_per_hour: 1_572_500_170.6, files: 12 },
      ] }.to_json)

    answer = call_tool(:list_projects, user: @user)

    assert_not answer.error?
    assert_requested asked, times: 1
    volumes = answer.json["projects"].map { |project| project.slice("slug", "logs_per_hour", "bytes_per_hour", "files") }
    assert_equal [ { "slug" => "api", "logs_per_hour" => 0, "bytes_per_hour" => 0, "files" => 0 },
                   { "slug" => "web", "logs_per_hour" => 3_700_000, "bytes_per_hour" => 1_572_500_171, "files" => 12 } ], volumes
    assert_nil answer.json["note"]
  end

  test "list_projects in one of several Organizations needs its slug" do
    beta = create(:organization, slug: "beta")
    create(:organization_user, organization: beta, user: @user)
    create(:project, organization: beta, name: "Shop", slug: "shop")

    assert call_tool(:list_projects, user: @user).error?
    assert_equal [ "shop" ], call_tool(:list_projects, user: @user, organization: "beta").json["projects"].map { |p| p["slug"] }
  end
end
