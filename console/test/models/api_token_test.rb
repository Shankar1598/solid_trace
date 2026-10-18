# frozen_string_literal: true

require "test_helper"

class ApiTokenTest < ActiveSupport::TestCase
  setup do
    @user = create(:user)
  end

  test "issue! makes a token whose secret is st_ and at least 32 random bytes, and keeps only its digest" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop")

    assert_match(/\Ast_[A-Za-z0-9_-]{43,}\z/, secret)
    assert_equal Digest::SHA256.hexdigest(secret), token.reload.token_digest
    assert_not_includes ApiToken.column_names, "token"
    assert_equal "Laptop", token.name
    assert_nil token.last_used_at
  end

  test "each token gets its own secret" do
    _, one = ApiToken.issue!(user: @user, name: "One")
    _, two = ApiToken.issue!(user: @user, name: "Two")

    assert_not_equal one, two
  end

  test "find_by_secret finds a token by its secret's digest" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop")

    assert_equal token, ApiToken.find_by_secret(secret)
    assert_nil ApiToken.find_by_secret("#{secret}x")
    assert_nil ApiToken.find_by_secret(token.token_digest)
    assert_nil ApiToken.find_by_secret("")
    assert_nil ApiToken.find_by_secret(nil)
  end

  test "a revoked token isn't found" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop")
    token.destroy

    assert_nil ApiToken.find_by_secret(secret)
  end

  test "a token needs a name" do
    assert_raises(ActiveRecord::RecordInvalid) { ApiToken.issue!(user: @user, name: " ") }
    assert_equal 0, ApiToken.count
  end

  test "touch_last_used writes at most once a minute" do
    token, = ApiToken.issue!(user: @user, name: "Laptop")
    start = Time.zone.parse("2026-10-17 10:00:00")

    travel_to(start) { token.touch_last_used }
    assert_equal start, token.reload.last_used_at

    travel_to(start + 59.seconds) { token.reload.touch_last_used }
    assert_equal start, token.reload.last_used_at

    travel_to(start + 61.seconds) { token.reload.touch_last_used }
    assert_equal start + 61.seconds, token.reload.last_used_at
  end

  test "touch_last_used doesn't change updated_at" do
    token, = ApiToken.issue!(user: @user, name: "Laptop")

    assert_no_changes -> { token.reload.updated_at } do
      travel(5.minutes) { token.touch_last_used }
    end
  end

  test "a user's tokens go with the user" do
    ApiToken.issue!(user: @user, name: "Laptop")

    assert_difference -> { ApiToken.count }, -1 do
      @user.destroy
    end
  end
end
