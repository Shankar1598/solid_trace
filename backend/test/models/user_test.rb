require "test_helper"

class UserTest < ActiveSupport::TestCase
  test "should validate email format" do
    user = build(:user, email: "invalid_email")
    assert_not user.valid?
    assert_includes user.errors[:email], "is invalid"

    user.email = "valid@example.com"
    assert user.valid?
  end
end
