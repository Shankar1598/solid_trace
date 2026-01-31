# frozen_string_literal: true

require "test_helper"

class ProjectsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
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
    patch project_url(@project, org_slug: @organization.slug), params: {
      project: { name: "Updated Project" }
    }

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_equal "Updated Project", @project.reload.name
  end

  test "denies access to other org" do
    other_org = create(:organization)

    get projects_url(org_slug: other_org.slug)

    assert_response :not_found
  end
end
