# frozen_string_literal: true

require "test_helper"

# The install's email settings: the SMTP server and the sender, with the
# password encrypted with keys derived from SECRET_KEY_BASE (ADR 0015).
class InstanceSettingTest < ActiveSupport::TestCase
  test "the SMTP password is kept encrypted, and reads back as typed" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "alerts@example.com")

    stored = InstanceSetting.connection.select_value("SELECT smtp_password FROM instance_settings")
    assert stored.present?
    assert_not_includes stored, "s3cret-pass"
    assert_equal "s3cret-pass", InstanceSetting.current.smtp_password
  end
end
