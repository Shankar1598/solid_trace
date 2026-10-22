# frozen_string_literal: true

require "test_helper"

# Settings → Email: the install's SMTP server and sender, which apply to every
# Organization, so only an instance admin sees or changes them (ADR 0015). The
# password is write-only: the page only says whether one is saved.
class EmailSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  # An SMTP server that refuses to sign in.
  class RefusingSmtp
    def initialize(_settings); end

    def deliver!(_mail)
      raise Net::SMTPAuthenticationError, "535 5.7.8 Authentication credentials invalid"
    end
  end

  setup do
    @organization = create(:organization)
    @instance_admin = create(:user, email: "grace@example.com", instance_admin: true)
    @organization.organization_users.create!(user: @instance_admin, role: :admin)
    @org_admin = create(:user)
    @organization.organization_users.create!(user: @org_admin, role: :admin)
  end

  test "an instance admin sees the settings and whether a password is saved, never the password" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "SolidTrace <alerts@example.com>")
    sign_in_as(@instance_admin)

    get email_settings_url(org_slug: @organization.slug), headers: inertia

    assert_response :success
    assert_equal "Settings/Email", response.parsed_body["component"]
    assert_equal({ "smtp_address" => "smtp.example.com", "smtp_port" => 587, "smtp_username" => "apikey",
      "mail_from" => "SolidTrace <alerts@example.com>", "password_saved" => true, "password_readable" => true,
      "problem" => nil }, response.parsed_body.dig("props", "settings"))
    assert response.parsed_body.dig("props", "current_user", "instance_admin")
    assert_not_includes response.body, "s3cret-pass"
  end

  test "the page says what's missing" do
    sign_in_as(@instance_admin)

    get email_settings_url(org_slug: @organization.slug), headers: inertia

    assert_equal "No SMTP host: Console sends no email", response.parsed_body.dig("props", "settings", "problem")
  end

  test "an Organization admin who isn't an instance admin is refused, and changes nothing" do
    sign_in_as(@org_admin)

    get email_settings_url(org_slug: @organization.slug)
    assert_redirected_to root_url
    assert_equal "You are not authorized to perform this action.", flash[:alert]

    patch email_settings_url(org_slug: @organization.slug), params: { email_settings: { smtp_address: "evil.example.com" } }
    assert_redirected_to root_url
    assert_nil InstanceSetting.current.smtp_address

    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")
    assert_no_emails do
      post send_test_email_settings_url(org_slug: @organization.slug), params: { to: "someone@example.com" }
    end
    assert_redirected_to root_url
  end

  test "saving sets the settings, and a blank password keeps the saved one" do
    sign_in_as(@instance_admin)

    save(smtp_address: "smtp.example.com", smtp_port: "465", smtp_username: "apikey", smtp_password: "s3cret-pass",
      mail_from: "SolidTrace <alerts@example.com>")
    assert_redirected_to email_settings_url(org_slug: @organization.slug)
    assert_equal "Email settings saved", flash[:notice]

    save(smtp_address: "smtp.example.com", smtp_port: "465", smtp_username: "apikey", smtp_password: "",
      mail_from: "SolidTrace <alerts@example.com>")

    settings = InstanceSetting.current
    assert_equal [ "smtp.example.com", 465, "apikey", "s3cret-pass" ],
      [ settings.smtp_address, settings.smtp_port, settings.smtp_username, settings.smtp_password ]
  end

  test "clearing the password removes the saved one" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "alerts@example.com")
    sign_in_as(@instance_admin)

    save(smtp_address: "smtp.example.com", smtp_port: "587", smtp_username: "apikey", smtp_password: "",
      clear_password: "true", mail_from: "alerts@example.com")

    assert_not InstanceSetting.current.smtp_password_saved?
  end

  test "invalid settings come back with their errors on the form, and nothing is saved" do
    sign_in_as(@instance_admin)

    save(smtp_address: "smtp.example.com", smtp_port: "0", smtp_username: "", smtp_password: "", mail_from: "")

    assert_redirected_to email_settings_url(org_slug: @organization.slug)
    get email_settings_url(org_slug: @organization.slug), headers: inertia
    assert response.parsed_body.dig("props", "errors", "smtp_port").present?
    assert_equal [ "can't be blank" ], response.parsed_body.dig("props", "errors", "mail_from")
    assert_nil InstanceSetting.current.smtp_address
  end

  test "a test email goes to the address typed, or else to the instance admin" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")
    sign_in_as(@instance_admin)

    post send_test_email_settings_url(org_slug: @organization.slug), params: { to: "ops@example.com" }
    assert_redirected_to email_settings_url(org_slug: @organization.slug)
    assert_equal "Test email sent to ops@example.com", flash[:notice]

    post send_test_email_settings_url(org_slug: @organization.slug), params: { to: "" }
    assert_equal "Test email sent to grace@example.com", flash[:notice]

    assert_equal [ [ "ops@example.com" ], [ "grace@example.com" ] ], ActionMailer::Base.deliveries.map(&:to)
    assert_equal "SolidTrace test email", ActionMailer::Base.deliveries.last.subject
  end

  test "a test email the SMTP server refuses shows its error" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")
    sign_in_as(@instance_admin)

    with_delivery_method(RefusingSmtp) do
      post send_test_email_settings_url(org_slug: @organization.slug), params: { to: "ops@example.com" }
    end

    assert_redirected_to email_settings_url(org_slug: @organization.slug)
    assert_equal "The test email wasn't sent: 535 5.7.8 Authentication credentials invalid", flash[:alert]
  end

  test "without email set up, no test email is sent, and the page says why" do
    sign_in_as(@instance_admin)

    assert_no_emails do
      post send_test_email_settings_url(org_slug: @organization.slug), params: { to: "ops@example.com" }
    end

    assert_equal "The test email wasn't sent: No SMTP host: Console sends no email", flash[:alert]
  end

  private

  def save(**settings)
    patch email_settings_url(org_slug: @organization.slug), params: { email_settings: settings }
  end

  def with_delivery_method(method)
    previous = ActionMailer::Base.delivery_method
    ActionMailer::Base.delivery_method = method
    yield
  ensure
    ActionMailer::Base.delivery_method = previous
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
