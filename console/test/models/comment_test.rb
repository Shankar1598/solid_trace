# frozen_string_literal: true

require "test_helper"

class CommentTest < ActiveSupport::TestCase
  test "should be valid with valid attributes" do
    comment = create(:comment)
    assert comment.valid?
  end

  test "should belong to issue" do
    comment = build(:comment, issue: nil)
    assert_not comment.valid?
    assert_includes comment.errors[:issue], "must exist"
  end

  test "should belong to organization_user" do
    comment = build(:comment, organization_user: nil)
    assert_not comment.valid?
    assert_includes comment.errors[:organization_user], "must exist"
  end

  test "a Factory comment has a run_id and no member" do
    comment = build(:comment, issue: create(:issue), organization_user: nil, run_id: "run-1")
    assert comment.valid?
    assert comment.factory?
  end

  test "a comment with a member and a run_id is invalid" do
    comment = build(:comment, issue: create(:issue), run_id: "run-1")
    assert_not comment.valid?
    assert_includes comment.errors[:run_id], "is only for a Factory comment, which has no member as its author"
  end

  test "a comment with no member and no run_id is invalid" do
    comment = build(:comment, issue: create(:issue), organization_user: nil, run_id: nil)
    assert_not comment.valid?
    assert_includes comment.errors[:organization_user], "must exist"
  end

  test "should have rich text content" do
    comment = create(:comment, content: "Hello <b>World</b>")
    assert comment.content.body.to_s.include?("Hello <b>World</b>")
  end
end
