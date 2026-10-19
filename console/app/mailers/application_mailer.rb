# frozen_string_literal: true

class ApplicationMailer < ActionMailer::Base
  default from: ENV["MAIL_FROM"].presence || "from@example.com"
  layout "mailer"

  # Whether mail can be sent: in production, once SMTP is set up
  # (config/environments/production.rb); always in development and test.
  def self.configured?
    !Rails.env.production? || ENV["SMTP_ADDRESS"].present?
  end
end
