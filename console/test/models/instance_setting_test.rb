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

  test "the text-match budget is 4096 MiB until changed, a whole number of MiB from 0, and changes at once" do
    assert_equal 4096, InstanceSetting.current.logs_text_match_budget_mb
    [ -1, "1.5", "lots", nil ].each do |mb|
      settings = InstanceSetting.new(logs_text_match_budget_mb: mb)
      assert_not settings.valid?, "budget #{mb.inspect}"
      assert settings.errors[:logs_text_match_budget_mb].any?, "budget #{mb.inspect}"
    end
    assert InstanceSetting.new(logs_text_match_budget_mb: 0).valid?

    assert InstanceSetting.current.update_editable({ logs_text_match_budget_mb: "10240" })
    assert_equal 10_240, InstanceSetting.current.logs_text_match_budget_mb
  end

  test "the Log read threads are 0, ClickHouse's default, until changed, a whole number from 0 to 64, and change at once" do
    assert_equal 0, InstanceSetting.current.logs_read_threads
    [ -1, 65, "1.5", "lots", nil ].each do |threads|
      settings = InstanceSetting.new(logs_read_threads: threads)
      assert_not settings.valid?, "threads #{threads.inspect}"
      assert settings.errors[:logs_read_threads].any?, "threads #{threads.inspect}"
    end
    [ 0, 64 ].each { |threads| assert InstanceSetting.new(logs_read_threads: threads).valid?, "threads #{threads}" }

    assert InstanceSetting.current.update_editable({ logs_read_threads: "8" })
    assert_equal 8, InstanceSetting.current.logs_read_threads
  end

  test "the hours on local disk are 24 until changed, a whole number from 0 to 720, and change at once" do
    assert_equal 24, InstanceSetting.current.local_cache_hours
    [ -1, 721, "1.5", "lots", nil ].each do |hours|
      settings = InstanceSetting.new(local_cache_hours: hours)
      assert_not settings.valid?, "hours #{hours.inspect}"
      assert settings.errors[:local_cache_hours].any?, "hours #{hours.inspect}"
    end
    [ 0, 720 ].each { |hours| assert InstanceSetting.new(local_cache_hours: hours).valid?, "hours #{hours}" }

    assert InstanceSetting.current.update_editable({ local_cache_hours: "6" })
    assert_equal 6, InstanceSetting.current.local_cache_hours
  end

  test "Console's and ingest's URLs are blank until saved, drop a trailing slash, and blank saves as none" do
    assert_nil InstanceSetting.current.console_url
    assert_nil InstanceSetting.current.ingest_url

    assert InstanceSetting.current.update_editable({ console_url: " http://console.example.com:3000/ ",
      ingest_url: "https://ingest.example.com:4443/sentry/" })
    settings = InstanceSetting.current
    assert_equal [ "http://console.example.com:3000", "https://ingest.example.com:4443/sentry" ],
      [ settings.console_url, settings.ingest_url ]

    assert settings.update_editable({ console_url: "", ingest_url: "  " })
    assert_equal [ nil, nil ], InstanceSetting.current.slice(:console_url, :ingest_url).values
  end

  test "Console's URL is a scheme and a host, with a port but no path; ingest's may have a path" do
    [ "console.example.com", "ftp://console.example.com", "http://", "http://console.example.com/app",
      "http://user:pass@console.example.com", "http://console.example.com?x=1", "http://console.example.com#top",
      "http://exa mple.com" ].each do |url|
      settings = InstanceSetting.new(console_url: url)
      assert_not settings.valid?, url
      assert_equal [ "must be http:// or https:// and a host, with a port if needed and no path, like " \
                     "https://console.example.com" ], settings.errors[:console_url], url
    end
    [ "http://localhost:3000", "https://solid-trace.example.ts.net", "http://[::1]:3000" ].each do |url|
      assert InstanceSetting.new(console_url: url).valid?, url
    end

    [ "ingest.example.com", "ftp://ingest.example.com", "https://ingest.example.com?x=1" ].each do |url|
      settings = InstanceSetting.new(ingest_url: url)
      assert_not settings.valid?, url
      assert_equal [ "must be http:// or https:// and a host, with a port and a path if needed, like " \
                     "https://ingest.example.com:4443" ], settings.errors[:ingest_url], url
    end
    [ "https://35.214.179.92:4443", "https://example.com/ingest" ].each do |url|
      assert InstanceSetting.new(ingest_url: url).valid?, url
    end
  end

  test "the page and MCP refuse a Console URL whose scheme isn't the HTTPS mode Console started in, and a shell doesn't" do
    PublicUrls.stub(:https?, false) do
      settings = InstanceSetting.current
      assert_not settings.update_editable({ console_url: "https://console.example.com", log_all_reads: true })
      assert_equal [ "must start with http://, as Console started without HTTPS. HTTPS changes only with " \
                     "CONSOLE_URL's scheme in deploy/.env and a restart" ], settings.errors[:console_url]
      assert_nil InstanceSetting.current.console_url
      assert_not InstanceSetting.current.log_all_reads?

      assert InstanceSetting.current.update_editable({ console_url: "http://console.example.com" })
    end

    PublicUrls.stub(:https?, true) do
      settings = InstanceSetting.current
      assert_not settings.update_editable({ console_url: "http://console.example.com:8080" })
      assert_equal [ "must start with https://, as Console started with HTTPS. HTTPS changes only with " \
                     "CONSOLE_URL's scheme in deploy/.env and a restart" ], settings.errors[:console_url]

      # An unchanged one isn't checked again, so the other settings still save.
      assert InstanceSetting.current.update_editable({ log_all_reads: true })
      assert InstanceSetting.current.update_editable({ console_url: "https://console.example.com" })
      # bin/rails instance_settings:set saves any scheme, for recovery.
      assert InstanceSetting.current.update(console_url: "http://console.example.com")
    end
  end
end
