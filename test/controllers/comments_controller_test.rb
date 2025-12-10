# frozen_string_literal: true

require "test_helper"

class CommentsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project)
    @user = create(:user)
    @organization.users << @user

    sign_in_as @user
  end

  test "should create comment" do
    assert_difference("Comment.count") do
      post issue_comments_url(@issue, org_slug: @organization.slug), params: { comment: { content: "Test comment" } }, as: :turbo_stream
    end

    assert_response :success
    assert_equal "Test comment", Comment.last.content.to_plain_text.strip
    assert_equal @user, Comment.last.user
  end

  test "should fail to create invalid comment" do
    assert_no_difference("Comment.count") do
      post issue_comments_url(@issue, org_slug: @organization.slug), params: { comment: { content: "" } }, as: :turbo_stream
    end

    assert_response :success
    assert_match(/Error creating comment/, response.body)
  end
end
