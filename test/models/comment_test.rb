require "test_helper"

class CommentTest < ActiveSupport::TestCase
  test "should be valid with valid attributes" do
    comment = build(:comment)
    assert comment.valid?
  end

  test "should belong to issue" do
    comment = build(:comment, issue: nil)
    assert_not comment.valid?
    assert_includes comment.errors[:issue], "must exist"
  end

  test "should belong to user" do
    comment = build(:comment, user: nil)
    assert_not comment.valid?
    assert_includes comment.errors[:user], "must exist"
  end

  test "should have rich text content" do
    comment = create(:comment, content: "Hello <b>World</b>")
    assert comment.content.body.to_s.include?("Hello <b>World</b>")
  end
end
