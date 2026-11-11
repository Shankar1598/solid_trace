# frozen_string_literal: true

require "test_helper"
require Rails.root.join("db/migrate/20261111120000_add_expires_at_to_sessions")

# Sessions signed in before Sessions expired keep working for 7 days from
# the upgrade, so it doesn't sign everyone out at once.
class AddExpiresAtToSessionsTest < ActiveSupport::TestCase
  test "a Session from before gets an expiry 7 days from the migration, and every Session needs one" do
    user = create(:user)
    migration = AddExpiresAtToSessions.new
    ActiveRecord::Migration.suppress_messages { migration.migrate(:down) }
    Session.reset_column_information
    connection.execute("INSERT INTO sessions (user_id, created_at, updated_at) VALUES (#{user.id}, '2026-01-01', '2026-01-01')")

    freeze_time
    ActiveRecord::Migration.suppress_messages { migration.migrate(:up) }
    Session.reset_column_information

    assert_equal 7.days.from_now, user.sessions.sole.expires_at
    assert_raises(ActiveRecord::NotNullViolation) { Session.create!(user: user) }
  ensure
    Session.reset_column_information
  end

  private

  def connection
    ActiveRecord::Base.connection
  end
end
