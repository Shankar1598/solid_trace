# frozen_string_literal: true

require "test_helper"

class ProjectsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @user = create(:user)
    @organization.users << @user
    @project = create(:project, organization: @organization)

    sign_in_as(@user)
  end

  test "index returns success" do
    get projects_url(org_slug: @organization.slug)

    assert_response :success
  end

  test "every page shares the Organization's Projects for the command palette" do
    create(:project, organization: @organization, name: "Backoffice")
    create(:project, organization: create(:organization), name: "Elsewhere")

    get projects_url(org_slug: @organization.slug), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    projects = JSON.parse(response.body)["props"]["org_projects"]
    assert_equal [ "Backoffice", @project.name ].sort, projects.map { |p| p["name"] }
    assert_includes projects, { "name" => @project.name, "slug" => @project.slug }
  end

  test "show returns success" do
    get project_url(@project, org_slug: @organization.slug)

    assert_response :success
  end

  test "the Project page's DSN sends to ingest as the Logs snippets do: PUBLIC_INGEST_URL, or this host at port 4000" do
    key = @project.project_keys.create!

    assert_equal "http://#{key.public_key}@www.example.com:4000/#{@project.id}", project_page["project"]["keys"].sole["dsn"]

    ENV["PUBLIC_INGEST_URL"] = "https://ingest.example.com/sentry/"
    assert_equal "https://#{key.public_key}@ingest.example.com/sentry/#{@project.id}", project_page["project"]["keys"].sole["dsn"]
  ensure
    ENV.delete("PUBLIC_INGEST_URL")
  end

  test "create project" do
    assert_difference -> { @organization.projects.count }, 1 do
      post projects_url(org_slug: @organization.slug), params: {
        project: { name: "New Project" },
      }
    end

    assert_redirected_to projects_url(org_slug: @organization.slug)
  end

  test "a new Project gets its first key, so it can be sent Logs at once" do
    post projects_url(org_slug: @organization.slug), params: { project: { name: "Checkout" } }

    keys = @organization.projects.find_by!(name: "Checkout").project_keys
    assert_equal 1, keys.size
    assert_match(/\A\h{32}\z/, keys.first.public_key)
    assert_match(/\A\h{64}\z/, keys.first.secret_key)
  end

  test "Generate New Key gives the Project another key of its own" do
    post project_keys_url(@project, org_slug: @organization.slug)

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_match(/\A\h{32}\z/, @project.project_keys.sole.public_key)
  end

  test "update project" do
    sign_in_as(@admin)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { name: "Updated Project" },
    }

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_equal "Updated Project", @project.reload.name
  end

  test "an admin sets a Project's Log Retention" do
    sign_in_as(@admin)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { log_retention_days: "14" },
    }

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_equal 14, project_page["project"]["log_retention_days"]
  end

  test "a Project's Log Retention must be a positive whole number of days" do
    sign_in_as(@admin)
    @project.update!(log_retention_days: 7)

    [ "0", "-3", "1.5", "two" ].each do |days|
      patch project_url(@project, org_slug: @organization.slug), params: {
        project: { log_retention_days: days },
      }

      assert_redirected_to project_url(@project, org_slug: @organization.slug)
      page = project_page
      assert page["errors"]["log_retention_days"].present?, "#{days.inspect} should be rejected"
      assert_equal 7, page["project"]["log_retention_days"]
    end
  end

  test "clearing a Project's Log Retention falls back to the instance default" do
    sign_in_as(@admin)
    @project.update!(log_retention_days: 7)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { log_retention_days: "" },
    }

    page = project_page
    assert_nil page["project"]["log_retention_days"]
    assert_equal 30, page["default_log_retention_days"]
  end

  test "the Project page lets only admins edit Project settings" do
    assert_equal false, project_page["can_manage_project"]

    sign_in_as(@admin)
    assert_equal true, project_page["can_manage_project"]
  end

  test "a member can't change a Project's Log Retention" do
    @project.update!(log_retention_days: 7)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { log_retention_days: "90" },
    }

    assert_response :redirect
    assert_equal "You are not authorized to perform this action.", flash[:alert]
    assert_equal 7, project_page["project"]["log_retention_days"]
  end

  test "denies access to other org" do
    other_org = create(:organization)

    get projects_url(org_slug: other_org.slug)

    assert_response :not_found
  end

  private

  def project_page
    get project_url(@project, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    JSON.parse(response.body)["props"]
  end
end
