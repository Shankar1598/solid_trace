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

  test "the link is to Console's URL as it is when the email is built: the setting, then CONSOLE_URL" do
    user = create(:user)

    ENV["CONSOLE_URL"] = "http://10.0.0.5:3000"
    assert_match %r{\Ahttp://10\.0\.0\.5:3000/passwords/[^/]+/edit\z}, PasswordsMailer.reset(user).text_part.body.to_s[%r{\S+/edit}]

    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")
    assert_match %r{\Ahttps://solid-trace\.example\.ts\.net/passwords/[^/]+/edit\z},
      PasswordsMailer.reset(user).text_part.body.to_s[%r{\S+/edit}]
  ensure
    ENV.delete("CONSOLE_URL")
  end
end
