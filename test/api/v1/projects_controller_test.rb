require "test_helper"

module Api
  module V1
    class ProjectsControllerTest < ActionDispatch::IntegrationTest
      setup do
        @user = create(:user)
        @organization = create(:organization)
        @organization.users << @user
        @project = create(:project, organization: @organization)

        payload = { user_id: @user.id }
        @token = JWT.encode(payload, Rails.application.secret_key_base)
        @headers = { "Authorization" => "Bearer #{@token}" }
      end

      test "should get index" do
        get api_v1_projects_url(org_slug: @organization.slug), headers: @headers, as: :json
        assert_response :success

        json_response = JSON.parse(response.body)
        assert_not_empty json_response
        assert_equal @project.name, json_response.first["name"]
      end

      test "should show project" do
        get api_v1_project_url(org_slug: @organization.slug, project_slug: @project.slug), headers: @headers, as: :json
        assert_response :success

        json_response = JSON.parse(response.body)
        assert_equal @project.name, json_response["name"]
      end

      test "should update project" do
        patch api_v1_project_url(org_slug: @organization.slug, project_slug: @project.slug),
              params: { project: { name: "Updated Name" } }, headers: @headers, as: :json
        assert_response :success

        json_response = JSON.parse(response.body)
        assert_equal "Updated Name", json_response["name"]
        assert_equal "Updated Name", @project.reload.name
      end

      test "should return 404 for invalid organization" do
        get api_v1_projects_url(org_slug: "invalid-org"), headers: @headers, as: :json
        assert_response :not_found
      end

      test "should return 404 for invalid project" do
        get api_v1_project_url(org_slug: @organization.slug, project_slug: "invalid-project"), headers: @headers, as: :json
        assert_response :not_found
      end
    end
  end
end
