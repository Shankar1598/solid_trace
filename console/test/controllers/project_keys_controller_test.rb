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

  test "an admin makes the Agent Project's one key, which its page shows with what it is for; a second is refused" do
    agent_project = create(:project, organization: @organization, name: "Agent sessions", agent: true)
    sign_in_as(@admin)
    assert_equal true, page_of(agent_project)["can_manage_keys"]
    assert_equal ProjectKey::AGENT_PROJECT, page_of(agent_project)["agent_key_text"]

    post project_keys_url(agent_project, org_slug: @organization.slug)
    assert_equal "Key created successfully", flash[:notice]
    key = agent_project.project_keys.sole

    assert_no_difference -> { ProjectKey.count } do
      post project_keys_url(agent_project, org_slug: @organization.slug)
    end
    assert_redirected_to project_url(agent_project, org_slug: @organization.slug)
    assert_equal "#{ProjectKey::ONE_AGENT_KEY}.", flash[:alert]

    props = page_of(agent_project)
    shown = props["project"]["keys"].sole
    assert_equal key.public_key, shown["public_key"]
    assert_nil shown["dsn"], "the key sends no Events, so it has no DSN"
    assert_equal ProjectKey::AGENT_PROJECT, props["agent_key_text"]
  end

  test "an admin rotates and deletes the Agent Project's key, and then makes a new one" do
    agent_project = create(:project, organization: @organization, name: "Agent sessions", agent: true)
    key = agent_project.project_keys.create!
    old_public = key.public_key
    sign_in_as(@admin)

    patch rotate_project_key_url(agent_project, key, org_slug: @organization.slug)
    assert_not_equal old_public, key.reload.public_key

    delete project_key_url(agent_project, key, org_slug: @organization.slug)
    assert_not ProjectKey.exists?(key.id)

    assert_difference -> { agent_project.project_keys.count }, 1 do
      post project_keys_url(agent_project, org_slug: @organization.slug)
    end
  end

  test "a member can't make the Agent Project's key" do
    agent_project = create(:project, organization: @organization, name: "Agent sessions", agent: true)
    sign_in_as(@member)

    assert_no_difference -> { ProjectKey.count } do
      post project_keys_url(agent_project, org_slug: @organization.slug)
    end
    assert_equal "You are not authorized to perform this action.", flash[:alert]
    assert_equal false, page_of(agent_project)["can_manage_keys"]
  end

  test "an ordinary Project's page says nothing of the Agent Project's key" do
    sign_in_as(@admin)

    assert_nil page_of(@project)["agent_key_text"]
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

  private

  def page_of(project)
    get project_url(project, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    JSON.parse(response.body)["props"]
  end
end
