# frozen_string_literal: true

# The email settings as the Email settings page and the MCP server show them.
# The password is write-only: only whether one is saved, and readable.
class EmailSettingsSerializer
  def initialize(settings)
    @settings = settings
  end

  def as_json(*)
    {
      smtp_address: @settings.smtp_address,
      smtp_port: @settings.smtp_port,
      smtp_username: @settings.smtp_username,
      mail_from: @settings.mail_from,
      password_saved: @settings.smtp_password_saved?,
      password_readable: @settings.smtp_password_readable?,
      # Why Console can't send email, or nil when it can.
      problem: @settings.email_problem,
    }
  end
end
