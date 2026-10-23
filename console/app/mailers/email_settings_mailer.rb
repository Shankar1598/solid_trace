# frozen_string_literal: true

require "net/smtp"

# The test email an instance admin sends from the Email settings page or the
# MCP server, to check the SMTP server and the sender.
class EmailSettingsMailer < ApplicationMailer
  # What sending can fail with: the SMTP server's refusals, a host that can't
  # be reached or doesn't answer in time, and TLS failures.
  FAILURES = [ Net::SMTPError, Timeout::Error, SocketError, SystemCallError, IOError, OpenSSL::SSL::SSLError ].freeze

  def test_email(to)
    mail(to: to, subject: "SolidTrace test email")
  end

  # Sends a test email to `to` now, not through a job, so the SMTP server's
  # error comes back to whoever asked. Returns why it wasn't sent, or nil.
  def self.send_test(to)
    problem = InstanceSetting.current.email_problem
    return problem if problem
    return "#{to} isn't an email address" unless to.to_s.match?(URI::MailTo::EMAIL_REGEXP)

    test_email(to).deliver_now
    nil
  rescue *FAILURES => e
    e.message
  end
end
