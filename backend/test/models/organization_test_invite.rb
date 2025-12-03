require "test_helper"

class OrganizationTest < ActiveSupport::TestCase
  include ActionMailer::TestHelper
  test "invite_user adds existing user and sends email" do
    organization = create(:organization)
    user = create(:user) # Assuming user two is not in organization one

    assert_difference -> { organization.users.count }, 1 do
      assert_emails 1 do
        organization.invite_user(user.email)
      end
    end

    assert organization.users.include?(user)
  end

  test "invite_user creates new user and sends email" do
    organization = create(:organization)
    email = "newuser@example.com"

    assert_difference -> { User.count }, 1 do
      assert_difference -> { organization.users.count }, 1 do
        assert_emails 1 do
          organization.invite_user(email)
        end
      end
    end

    new_user = User.find_by(email: email)
    assert_not_nil new_user
    assert organization.users.include?(new_user)
  end

  test "invite_user does not add user if already member but sends email" do
    organization = create(:organization)
    user = create(:user) # Assuming user one is already in organization one
    organization.users << user

    assert_no_difference -> { organization.users.count } do
      assert_emails 1 do
        organization.invite_user(user.email)
      end
    end
  end
end
