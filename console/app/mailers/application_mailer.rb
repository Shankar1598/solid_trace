# frozen_string_literal: true

# Every message reads the sender and, over SMTP (production), the server from
# the instance settings as it's sent, so a change on the Email settings page
# applies to the next one in every process (ADR 0015). Development keeps
# letter_opener and test keeps :test.
class ApplicationMailer < ActionMailer::Base
  default from: -> { InstanceSetting.current.mail_from || "from@example.com" },
    delivery_method_options: -> { InstanceSetting.current.smtp_options if delivery_method == :smtp }
  layout "mailer"

  # Whether mail can be sent: in production, once an instance admin has set up
  # email; always in development and test.
  def self.configured?
    !Rails.env.production? || InstanceSetting.current.email_set_up?
  end
end
