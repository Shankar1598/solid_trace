# frozen_string_literal: true

require "test_helper"

# A Project's keys: only the Organization's admins make, rotate and delete
# them (ProjectKeyPolicy).
class ProjectKeysControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization)
    @key = @project.project_keys.create!
  end

  test "an admin makes a key" do
    sign_in_as(@admin)

    assert_difference -> { @project.project_keys.count }, 1 do
      post project_keys_url(@project, org_slug: @organization.slug)
    end
    assert_redirected_to project_url(@project, org_slug: @organization.slug)
  end

  test "an admin rotates a key: new values, so the old ones no longer send" do
    sign_in_as(@admin)
    old_public, old_secret = @key.public_key, @key.secret_key

    patch rotate_project_key_url(@project, @key, org_slug: @organization.slug)

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    @key.reload
    assert_match(/\A\h{32}\z/, @key.public_key)
    assert_not_equal old_public, @key.public_key
    assert_not_equal old_secret, @key.secret_key
  end

  test "an admin deletes a key" do
    sign_in_as(@admin)

    delete project_key_url(@project, @key, org_slug: @organization.slug)

    assert_redirected_to project_url(@project, org_slug: @organization.slug)
    assert_not ProjectKey.exists?(@key.id)
  end

  test "a member can't make, rotate or delete a key" do
    sign_in_as(@member)
    public_key = @key.public_key

    assert_no_difference -> { ProjectKey.count } do
      post project_keys_url(@project, org_slug: @organization.slug)
      assert_equal "You are not authorized to perform this action.", flash[:alert]

      patch rotate_project_key_url(@project, @key, org_slug: @organization.slug)
      assert_equal "You are not authorized to perform this action.", flash[:alert]

      delete project_key_url(@project, @key, org_slug: @organization.slug)
      assert_equal "You are not authorized to perform this action.", flash[:alert]
    end
    assert_equal public_key, @key.reload.public_key
  end

  test "another Organization's Project's keys are not found" do
    sign_in_as(@admin)
    theirs = create(:project)
    key = theirs.project_keys.create!

    patch rotate_project_key_url(theirs, key, org_slug: @organization.slug)

    assert_response :not_found
  end
end
