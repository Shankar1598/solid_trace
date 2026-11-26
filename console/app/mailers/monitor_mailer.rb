# frozen_string_literal: true

# Mail about a Monitor: firing, resolved, or Logs that matched. Or mail
# about an Organization's Monitors that do not run, or run again. It takes the
# Notifiers::Message as a Hash, so it can wait in the mail queue.
class MonitorMailer < ApplicationMailer
  def notify(message, recipients)
    @message = Notifiers::Message.new(**message.symbolize_keys)

    mail(to: recipients, subject: "[SolidTrace] #{@message.title}")
  end
end
