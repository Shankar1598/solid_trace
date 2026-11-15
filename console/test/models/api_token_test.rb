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

  # A limit never points to nothing: the token goes with its Project or
  # its Organization.
  test "a token goes with the Project of its limit and with its Organization" do
    organization = create(:organization)
    create(:organization_user, organization: organization, user: @user)
    project = create(:project, organization: organization)
    limited, = ApiToken.issue!(user: @user, name: "Issue Run", scopes: [ "mcp:read" ], organization_id: organization.id,
                               project_id: project.id)
    scoped, = ApiToken.issue!(user: @user, name: "Run", scopes: [ "factory" ], organization_id: organization.id)

    project.destroy!
    assert_not ApiToken.exists?(limited.id)
    assert ApiToken.exists?(scoped.id)

    OrganizationUser.where(organization: organization).delete_all
    organization.destroy!
    assert_not ApiToken.exists?(scoped.id)
  end

  # Child tokens: issue_child! always copies the parent's user and
  # Organization, so these go through issue! with parent: directly.
  test "a child token acts only as its parent's user, in its parent's Organization" do
    acme = create(:organization)
    beta = create(:organization)
    other = create(:user)
    create(:organization_user, organization: acme, user: @user, role: :admin)
    create(:organization_user, organization: beta, user: @user, role: :admin)
    create(:organization_user, organization: acme, user: other, role: :admin)
    parent, = dispatcher_token(acme)

    [ { user: other, organization_id: acme.id }, { user: @user, organization_id: beta.id } ].each do |fields|
      error = assert_raises(ActiveRecord::RecordInvalid) do
        ApiToken.issue!(name: "Child", scopes: [ "run_events:write" ], expires_at: 1.hour.from_now, parent: parent,
                        **fields)
      end
      assert_match(/parent's user, in its parent's Organization/, error.record.errors[:parent].join, fields.inspect)
    end
  end

  test "issue_child! deletes its parent's expired children, and no other token" do
    acme = create(:organization)
    create(:organization_user, organization: acme, user: @user, role: :admin)
    parent, = dispatcher_token(acme)
    other_parent, = dispatcher_token(acme)
    expired = child_of(parent)
    live = child_of(parent)
    other_expired = child_of(other_parent)
    [ expired, other_expired ].each { |token| token.update_column(:expires_at, 1.minute.ago) }

    made = child_of(parent)

    assert_not ApiToken.exists?(expired.id)
    assert ApiToken.exists?(live.id)
    assert ApiToken.exists?(other_expired.id)
    assert_equal [ live.id, made.id ].sort, parent.children.pluck(:id).sort
  end

  private

  def dispatcher_token(organization)
    ApiToken.issue!(user: @user, name: "Dispatcher", scopes: Oauth::Scopes::DISPATCHER,
                    organization_id: organization.id, expires_at: 30.days.from_now)
  end

  def child_of(parent)
    ApiToken.issue_child!(parent: parent, name: "Execution", scopes: [ "run_events:write" ],
                          expires_at: 1.hour.from_now).first
  end
end
