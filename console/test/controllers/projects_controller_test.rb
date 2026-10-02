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

  test "show returns success" do
    get project_url(@project, org_slug: @organization.slug)

    assert_response :success
  end

  test "create project" do
    assert_difference -> { @organization.projects.count }, 1 do
      post projects_url(org_slug: @organization.slug), params: {
        project: { name: "New Project" }
      }
    end

    assert_redirected_to projects_url(org_slug: @organization.slug)
  end

  test "update project" do
    sign_in_as(@admin)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { name: "Updated Project" }
    }

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_equal "Updated Project", @project.reload.name
  end

  test "an admin sets a Project's Log Retention" do
    sign_in_as(@admin)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { log_retention_days: "14" }
    }

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_equal 14, project_page["project"]["log_retention_days"]
  end

  test "a Project's Log Retention must be a positive whole number of days" do
    sign_in_as(@admin)
    @project.update!(log_retention_days: 7)

    [ "0", "-3", "1.5", "two" ].each do |days|
      patch project_url(@project, org_slug: @organization.slug), params: {
        project: { log_retention_days: days }
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
      project: { log_retention_days: "" }
    }

    page = project_page
    assert_nil page["project"]["log_retention_days"]
    assert_equal 30, page["default_log_retention_days"]
  end

  test "a member can't change a Project's Log Retention" do
    @project.update!(log_retention_days: 7)

    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { log_retention_days: "90" }
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
