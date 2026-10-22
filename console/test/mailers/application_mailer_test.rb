# frozen_string_literal: true

require "test_helper"

# Every message reads the SMTP server and the sender from the instance
# settings when it's sent, so a change on the Email settings page applies to
# the next one, with no restart (ADR 0015).
class ApplicationMailerTest < ActionMailer::TestCase
  test "over SMTP, a message goes through the instance settings' server, from their sender" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", smtp_port: 465, smtp_username: "apikey",
      smtp_password: "s3cret-pass", mail_from: "SolidTrace <alerts@example.com>")

    message = over_smtp { PasswordsMailer.reset(create(:user)).message }

    assert_equal "SolidTrace <alerts@example.com>", message[:from].to_s
    assert_kind_of Mail::SMTP, message.delivery_method
    settings = message.delivery_method.settings
    assert_equal [ "smtp.example.com", 465, "apikey", "s3cret-pass", :plain, true ],
      settings.values_at(:address, :port, :user_name, :password, :authentication, :tls)
  end

  test "a change to the settings applies to the next message" do
    InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")
    over_smtp { PasswordsMailer.reset(create(:user)).message }

    InstanceSetting.current.update!(smtp_address: "smtp.other.example", mail_from: "ops@other.example")
    message = over_smtp { PasswordsMailer.reset(create(:user)).message }

    assert_equal [ "ops@other.example" ], message.from
    assert_equal "smtp.other.example", message.delivery_method.settings[:address]
  end

  test "without a sender, a message is from the placeholder" do
    message = PasswordsMailer.reset(create(:user)).message

    assert_equal [ "from@example.com" ], message.from
  end

  test "in production, mail can be sent once email is set up" do
    in_production do
      assert_not ApplicationMailer.configured?

      InstanceSetting.current.update!(smtp_address: "smtp.example.com", mail_from: "alerts@example.com")

      assert ApplicationMailer.configured?
    end
  end

  test "in development and test, mail can always be sent" do
    assert ApplicationMailer.configured?
  end

  private

  def over_smtp
    previous = ActionMailer::Base.delivery_method
    ActionMailer::Base.delivery_method = :smtp
    yield
  ensure
    ActionMailer::Base.delivery_method = previous
  end

  def in_production(&block)
    Rails.stub(:env, ActiveSupport::EnvironmentInquirer.new("production"), &block)
  end
end
