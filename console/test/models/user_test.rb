# frozen_string_literal: true

require "test_helper"

class UserTest < ActiveSupport::TestCase
  test "requires name and email" do
    user = User.new(password: "password")

    assert_not user.valid?
    assert_includes user.errors[:name], "can't be blank"
    assert_includes user.errors[:email], "can't be blank"
  end

  test "validates email format" do
    user = User.new(name: "Test", email: "invalid", password: "password")

    assert_not user.valid?
    assert_includes user.errors[:email], "is invalid"
  end

  test "validates email uniqueness" do
    create(:user, email: "test@example.com")

    user = User.new(name: "Other", email: "test@example.com", password: "password")

    assert_not user.valid?
    assert_includes user.errors[:email], "has already been taken"
  end

  test "authenticates with password" do
    user = create(:user, password: "secret")

    assert user.authenticate("secret")
    assert_not user.authenticate("wrong")
  end
end
