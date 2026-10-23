# frozen_string_literal: true

module McpTools
  # Changes the install's email settings, as Settings → Email does. For
  # instance admins.
  class UpdateEmailSettings < Base
    include EmailSettingsTool

    description "Changes the SMTP server and sender SolidTrace sends every Organization's email with. Only the " \
                "settings given change. A password given here passes through your context, so the user may prefer " \
                "to type it on Console's Email settings page instead. Answers the settings as get_email_settings " \
                "does. Only an instance admin can."
    writes!
    argument :smtp_address, { type: "string", description: "The SMTP host, such as smtp.example.com. Empty for none: no email is sent." }
    argument :smtp_port, { type: "integer", description: "The SMTP port: 465 uses TLS, any other STARTTLS when the server offers it." }
    argument :smtp_username, { type: "string", description: "The username to sign in with. Empty for none, which clears the password too." }
    argument :smtp_password, { type: "string", description: "A new SMTP password. Left out or empty, the saved one stays." }
    argument :clear_password, { type: "boolean", description: "true clears the saved password." }
    argument :mail_from, { type: "string", description: "The sender, with or without a name, such as SolidTrace <alerts@example.com>." }

    def run
      require_instance_admin!("change the email settings")
      settings = InstanceSetting.current
      unless settings.update_email(arguments.except(:clear_password), clear_password: arguments[:clear_password] == true)
        raise ToolError, "Not saved: #{settings.errors.full_messages.join('; ')}."
      end

      settings_answer(settings)
    end
  end
end
