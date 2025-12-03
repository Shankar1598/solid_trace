require "test_helper"

class UserMailerTest < ActionMailer::TestCase
  test "invitation_email" do
    user = create(:user)
    organization = create(:organization)
    email = UserMailer.invitation_email(user, organization)

    assert_emails 1 do
      email.deliver_now
    end

    assert_equal ["from@example.com"], email.from
    assert_equal [user.email], email.to
    assert_equal "You have been invited to join #{organization.name} on Garnet", email.subject
    assert_match "You have been invited to join the organization \"#{organization.name}\"", email.body.encoded
  end
end
