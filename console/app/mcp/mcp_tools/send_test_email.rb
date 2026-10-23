# frozen_string_literal: true

module McpTools
  # Sends a test email with the install's email settings, as Settings → Email
  # does. For instance admins.
  class SendTestEmail < Base
    include EmailSettingsTool

    description "Sends a test email at once with the saved email settings, to check the SMTP server and the sender. " \
                "Answers the SMTP server's error when it fails. Only an instance admin can."
    writes!(open_world: true)
    argument :to, { type: "string", description: "Who to send it to. Your own email when left out." }

    def run
      require_instance_admin!("send a test email")
      to = arguments[:to].to_s.strip.presence || user.email
      failure = EmailSettingsMailer.send_test(to)
      raise ToolError, "The test email wasn't sent: #{failure}" if failure

      organization = user.organizations_by_name.first
      json({ sent: true, to: to }, link: (links.email_settings(organization) if organization))
    end
  end
end
