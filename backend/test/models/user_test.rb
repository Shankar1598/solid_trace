require "test_helper"

class UserTest < ActiveSupport::TestCase
  test "should create user with factory" do
    user = create(:user)
    assert user.valid?
  end
end
