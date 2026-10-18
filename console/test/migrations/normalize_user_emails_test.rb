# frozen_string_literal: true

require "test_helper"
require Rails.root.join("db/migrate/20261018090000_normalize_user_emails")

# Emails as users typed them before User normalized them, trimmed and put in
# lower case. Of users that would then share an email, one keeps it, so it
# can still be signed in with: the one already in lower case, or else the
# oldest. The others stay as typed, and the migration names them.
class NormalizeUserEmailsTest < ActiveSupport::TestCase
  test "an email is trimmed and put in lower case, and signs in" do
    grace = user_typed(" Grace@Example.COM ")

    migrate

    assert_equal "grace@example.com", grace.reload.email
    assert_equal grace, User.authenticate_by(email: "GRACE@example.com", password: "password123")
  end

  test "of users who would share an email, the one already in lower case keeps it" do
    bob = user_typed("bob@example.com")
    other = user_typed("BOB@example.com")

    said = migrate

    assert_equal [ "bob@example.com", "BOB@example.com" ], [ bob.reload.email, other.reload.email ]
    assert_equal bob, User.authenticate_by(email: "Bob@Example.com", password: "password123")
    assert_match "user #{other.id} (BOB@example.com)", said
  end

  test "when none of them is in lower case, the oldest gets the email and the rest stay as typed" do
    oldest = user_typed("Bob@Example.com")
    other = user_typed("BOB@example.com")

    said = migrate

    assert_equal [ "bob@example.com", "BOB@example.com" ], [ oldest.reload.email, other.reload.email ]
    assert_equal oldest, User.authenticate_by(email: "bob@example.com", password: "password123")
    assert_not User.new(name: "Third", email: "bob@example.com", password: "password123").valid?,
      "the email is taken, so no third user is made"
    assert_match "user #{other.id} (BOB@example.com)", said
  end

  private

  # A user whose email is stored exactly as given, as before User normalized it.
  def user_typed(email)
    user = create(:user, email: "tmp-#{SecureRandom.hex(4)}@example.com", password: "password123")
    User.where(id: user.id).update_all([ "email = ?", email ])
    user
  end

  # What the migration says.
  def migrate
    out = StringIO.new
    original, $stdout = $stdout, out
    NormalizeUserEmails.new.up
    out.string
  ensure
    $stdout = original
  end
end
