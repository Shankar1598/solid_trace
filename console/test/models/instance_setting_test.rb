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

  test "a password encrypted under another SECRET_KEY_BASE is saved but unreadable, so email isn't set up" do
    other_key = ActiveRecord::Encryption::DerivedSecretKeyProvider.new("another secret")
    ActiveRecord::Encryption.with_encryption_context(key_provider: other_key) do
      InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
        smtp_password: "s3cret-pass", mail_from: "alerts@example.com")
    end

    settings = InstanceSetting.current
    assert settings.smtp_password_saved?
    assert_not settings.smtp_password_readable?
    assert_not settings.email_set_up?
    assert_nil settings.smtp_options
    assert_equal "The SMTP password can't be read, as Console's secret changed: enter it again", settings.email_problem
  end

  test "email is set up with a host and a sender, and a readable password when there's a username" do
    settings = InstanceSetting.current
    assert_equal "No SMTP host: Console sends no email", settings.email_problem

    settings.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")
    assert settings.email_set_up?
    assert_not settings.smtp_password_saved?

    settings.update!(smtp_username: "apikey")
    assert_equal "No SMTP password for the username", settings.email_problem

    settings.update!(smtp_password: "s3cret-pass")
    assert settings.reload.email_set_up?
    assert settings.smtp_password_readable?
  end

  test "on port 587 with a username, Console signs in with PLAIN over STARTTLS" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "alerts@example.com")

    assert_equal({ address: "smtp.example.com", port: 587, user_name: "apikey", password: "s3cret-pass",
      authentication: :plain, enable_starttls_auto: true, open_timeout: 10, read_timeout: 10 },
      InstanceSetting.current.smtp_options)
  end

  test "on port 465 without a username, Console uses implicit TLS and doesn't sign in" do
    InstanceSetting.current.update!(smtp_address: "relay.internal", smtp_port: 465, mail_from: "alerts@example.com")

    assert_equal({ address: "relay.internal", port: 465, tls: true, open_timeout: 10, read_timeout: 10 },
      InstanceSetting.current.smtp_options)
  end

  test "on port 465 with a username, Console signs in with PLAIN over implicit TLS" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_port: 465, smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "alerts@example.com")

    assert_equal({ address: "smtp.example.com", port: 465, user_name: "apikey", password: "s3cret-pass",
      authentication: :plain, tls: true, open_timeout: 10, read_timeout: 10 },
      InstanceSetting.current.smtp_options)
  end

  test "on port 587 without a username, Console uses STARTTLS and doesn't sign in" do
    InstanceSetting.current.update!(smtp_address: "relay.internal", mail_from: "alerts@example.com")

    assert_equal({ address: "relay.internal", port: 587, enable_starttls_auto: true, open_timeout: 10, read_timeout: 10 },
      InstanceSetting.current.smtp_options)
  end

  test "the port is a whole number from 1 to 65535" do
    [ 0, 65_536, "25.5", "smtp", nil ].each do |port|
      settings = InstanceSetting.new(smtp_port: port)
      assert_not settings.valid?, "port #{port.inspect}"
      assert settings.errors[:smtp_port].any?, "port #{port.inspect}"
    end
    assert InstanceSetting.new(smtp_port: 25).valid?
  end

  test "a host needs a sender" do
    settings = InstanceSetting.current
    settings.smtp_address = "smtp.example.com"

    assert_not settings.valid?
    assert_includes settings.errors[:mail_from], "can't be blank"
  end

  test "the sender is one email address, with or without a name" do
    [ "alerts@example.com", "SolidTrace <alerts@example.com>", "\"Solid, Trace\" <alerts@example.com>" ].each do |from|
      assert InstanceSetting.new(mail_from: from).valid?, from
    end
    [ "alerts", "SolidTrace alerts@example.com", "a@example.com, b@example.com", "@example.com" ].each do |from|
      settings = InstanceSetting.new(mail_from: from)
      assert_not settings.valid?, from
      assert_includes settings.errors[:mail_from], "isn't an email address, like SolidTrace <alerts@example.com>"
    end
  end

  test "settings are trimmed, and blank ones are none" do
    settings = InstanceSetting.current
    settings.update!(smtp_address: " smtp.example.com ", smtp_username: " ", mail_from: " alerts@example.com ")

    assert_equal [ "smtp.example.com", nil, "alerts@example.com" ],
      [ settings.smtp_address, settings.smtp_username, settings.mail_from ]
  end

  test "clearing the username clears the password" do
    settings = InstanceSetting.current
    settings.update!(smtp_address: "smtp.example.com", smtp_username: "apikey", smtp_password: "s3cret-pass",
      mail_from: "alerts@example.com")

    settings.update!(smtp_username: "")

    assert_not InstanceSetting.current.smtp_password_saved?
  end
end
