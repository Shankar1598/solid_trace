# frozen_string_literal: true

# Mail about a Monitor: firing, resolved, or Logs that matched. It takes the
# Notifiers::Message as a Hash, so it can wait in the mail queue.
class MonitorMailer < ApplicationMailer
  def notify(message, recipients)
    @message = Notifiers::Message.new(**message.symbolize_keys)

    mail(to: recipients, subject: "[SolidTrace] #{@message.title}")
  end
end
