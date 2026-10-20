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

  # One person, one user: an email is kept trimmed and in lower case, and
  # found in any case.
  test "an email is kept trimmed and in lower case" do
    user = create(:user, email: "  Grace.Hopper@Example.COM ")

    assert_equal "grace.hopper@example.com", user.reload.email
    assert_equal user, User.find_by(email: "GRACE.hopper@example.com")
  end

  test "an email another user has in another case is taken" do
    create(:user, email: "test@example.com")

    user = User.new(name: "Other", email: "Test@Example.com", password: "password")

    assert_not user.valid?
    assert_includes user.errors[:email], "has already been taken"
  end

  test "authenticates with password" do
    user = create(:user, password: "secret")

    assert user.authenticate("secret")
    assert_not user.authenticate("wrong")
  end

  test "an Invited user made with a password stays Invited until a password is set, however it's set" do
    user = create(:user, invited: true)
    assert user.reload.invited?

    user.update!(name: "Grace")
    assert user.reload.invited?

    user.update!(password: "new-password", password_confirmation: "new-password")
    assert_not user.reload.invited?
  end
end
