# frozen_string_literal: true

require "test_helper"

class PasswordsMailerTest < ActionMailer::TestCase
  test "reset sends the user a link that opens the form for a new password" do
    user = create(:user, email: "grace@example.com")

    mail = PasswordsMailer.reset(user)

    assert_equal [ "grace@example.com" ], mail.to
    assert_equal "Reset your SolidTrace password", mail.subject
    [ mail.html_part, mail.text_part ].each do |part|
      link = part.body.to_s[%r{http://example\.com/passwords/[^/"\s]+/edit}]
      assert link, "#{part.content_type} has the link"
      token = link[%r{/passwords/([^/]+)/edit}, 1]
      assert_equal user, User.find_by_password_reset_token(token)
      assert_match "15 minutes", part.body.to_s
    end
  end
end
