require "test_helper"

class IssuesControllerTest < ActionDispatch::IntegrationTest
  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project) # This will be issue #1

    sign_in_as(@user)
  end

  test "should show issue using number param" do
    get issue_url(@issue, org_slug: @organization.slug)
    assert_response :success
  end

  test "should resolve issue using number param" do
    post resolve_issue_url(@issue, org_slug: @organization.slug)
    assert_redirected_to issue_url(@issue, org_slug: @organization.slug)
    assert_equal 1, @issue.reload.status
  end

  test "should unresolve issue using number param" do
    @issue.update!(status: 1)
    post unresolve_issue_url(@issue, org_slug: @organization.slug)
    assert_redirected_to issue_url(@issue, org_slug: @organization.slug)
    assert_equal 0, @issue.reload.status
  end
end
