require "test_helper"

class CommentsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @organization = create(:organization)
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project)
    @user = create(:user)
    @organization.users << @user

    sign_in @user
  end

  test "should create comment" do
    assert_difference("Comment.count") do
      post issue_comments_url(@issue, org_slug: @organization.slug), params: { comment: { content: "Test comment" } }
    end

    assert_redirected_to issue_url(@issue, org_slug: @organization.slug)
    assert_equal "Test comment", Comment.last.content.to_plain_text.strip
    assert_equal @user, Comment.last.user
  end
end
