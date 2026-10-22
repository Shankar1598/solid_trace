# frozen_string_literal: true

require "test_helper"
require Rails.root.join("db/migrate/20261022180200_make_oldest_user_instance_admin")

# On an install that already has users, the oldest becomes the instance admin,
# as the person who set it up signed up first.
class MakeOldestUserInstanceAdminTest < ActiveSupport::TestCase
  test "the oldest user becomes the instance admin, and no one else" do
    newer = create(:user, created_at: 1.day.ago)
    oldest = create(:user, created_at: 1.year.ago)
    same_time = create(:user, created_at: 1.year.ago)

    MakeOldestUserInstanceAdmin.new.up

    assert oldest.reload.instance_admin?
    assert_not same_time.reload.instance_admin?, "of users made at once, the lower id is older"
    assert_not newer.reload.instance_admin?
  end

  test "an install without users gets no instance admin until someone signs up" do
    assert_nothing_raised { MakeOldestUserInstanceAdmin.new.up }

    assert_not User.exists?(instance_admin: true)
  end
end
