# frozen_string_literal: true

require "test_helper"

# The email settings tools: the install's SMTP server and sender, which only
# an instance admin reads or changes, as on Settings → Email (ADR 0015). They
# take no Organization, and never answer the password.
class McpEmailSettingsToolsTest < ActiveSupport::TestCase
  include McpTestHelper
  include ActionMailer::TestHelper

  # An SMTP server that refuses to sign in.
  class RefusingSmtp
    def initialize(_settings); end

    def deliver!(_mail)
      raise Net::SMTPAuthenticationError, "535 5.7.8 Authentication credentials invalid"
    end
  end

  setup do
    ActionMailer::Base.deliveries.clear
    @acme = create(:organization, name: "Acme", slug: "acme")
    @instance_admin = create(:user, name: "Grace", email: "grace@example.com", instance_admin: true)
    create(:organization_user, organization: @acme, user: @instance_admin, role: :admin)
    @org_admin = create(:user, name: "Ada", email: "ada@example.com")
    create(:organization_user, organization: @acme, user: @org_admin, role: :admin)
  end

  test "get_email_settings answers the settings and whether a password is saved, never the password" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "SolidTrace <alerts@example.com>")

    answer = call_tool(:get_email_settings, user: @instance_admin)

    assert_not answer.error?, answer.text
    assert_equal({ "smtp_address" => "smtp.example.com", "smtp_port" => 587, "smtp_username" => "apikey",
      "mail_from" => "SolidTrace <alerts@example.com>", "password_saved" => true, "password_readable" => true,
      "set_up" => true, "problem" => nil }, answer.json)
    assert_not_includes answer.text, "s3cret-pass"
    assert_equal "http://console.test/acme/settings/email", answer.link
  end

  test "get_email_settings says what's missing" do
    answer = call_tool(:get_email_settings, user: @instance_admin)

    assert_equal [ false, "No SMTP host: Console sends no email" ], answer.json.values_at("set_up", "problem")
  end

  test "update_email_settings changes only the settings given, and answers them without the password" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "alerts@example.com")

    answer = call_tool(:update_email_settings, user: @instance_admin, smtp_port: 465, mail_from: "Ops <ops@example.com>")

    assert_not answer.error?, answer.text
    settings = InstanceSetting.current
    assert_equal [ "smtp.example.com", 465, "apikey", "s3cret-pass", "Ops <ops@example.com>" ],
      [ settings.smtp_address, settings.smtp_port, settings.smtp_username, settings.smtp_password, settings.mail_from ]
    assert_equal [ 465, "Ops <ops@example.com>", true ], answer.json.values_at("smtp_port", "mail_from", "password_saved")
    assert_not_includes answer.text, "s3cret-pass"
    assert_equal "http://console.test/acme/settings/email", answer.link
  end

  test "update_email_settings sets a password, and clear_password clears it" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey", mail_from: "alerts@example.com")

    answer = call_tool(:update_email_settings, user: @instance_admin, smtp_password: "n3w-pass")
    assert_not_includes answer.text, "n3w-pass"
    assert_equal "n3w-pass", InstanceSetting.current.smtp_password

    answer = call_tool(:update_email_settings, user: @instance_admin, clear_password: true)
    assert_equal false, answer.json["password_saved"]
    assert_not InstanceSetting.current.smtp_password_saved?
  end

  test "update_email_settings answers what's wrong, and saves nothing" do
    answer = call_tool(:update_email_settings, user: @instance_admin, smtp_address: "smtp.example.com", smtp_port: 0)

    assert answer.error?
    assert_match "Not saved: ", answer.text
    assert_match "Smtp port", answer.text
    assert_match "Mail from can't be blank", answer.text
    assert_nil InstanceSetting.current.smtp_address
  end

  test "send_test_email sends to the address given, or else the caller" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")

    answer = call_tool(:send_test_email, user: @instance_admin, to: "ops@example.com")
    assert_not answer.error?, answer.text
    assert_equal({ "sent" => true, "to" => "ops@example.com" }, answer.json)

    call_tool(:send_test_email, user: @instance_admin)
    assert_equal [ [ "ops@example.com" ], [ "grace@example.com" ] ], ActionMailer::Base.deliveries.map(&:to)
  end

  test "send_test_email answers the SMTP server's error" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")

    answer = with_delivery_method(RefusingSmtp) { call_tool(:send_test_email, user: @instance_admin) }

    assert answer.error?
    assert_equal "The test email wasn't sent: 535 5.7.8 Authentication credentials invalid", answer.text
  end

  test "an Organization admin who isn't an instance admin is refused by each tool, and changes nothing" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")

    {
      get_email_settings: [ {}, "read the email settings" ],
      update_email_settings: [ { smtp_address: "evil.example.com" }, "change the email settings" ],
      send_test_email: [ { to: "ada@example.com" }, "send a test email" ],
    }.each do |tool, (arguments, doing)|
      answer = call_tool(tool, user: @org_admin, **arguments)
      assert answer.error?, tool
      assert_equal "Only an instance admin can #{doing}.", answer.text
    end
    assert_equal "smtp.example.com", InstanceSetting.current.smtp_address
    assert_no_emails
  end

  test "whoami says whether the caller is an instance admin" do
    assert_equal true, call_tool(:whoami, user: @instance_admin).json.dig("user", "instance_admin")
    assert_equal false, call_tool(:whoami, user: @org_admin).json.dig("user", "instance_admin")
  end

  private

  def with_delivery_method(method)
    previous = ActionMailer::Base.delivery_method
    ActionMailer::Base.delivery_method = method
    yield
  ensure
    ActionMailer::Base.delivery_method = previous
  end
end
