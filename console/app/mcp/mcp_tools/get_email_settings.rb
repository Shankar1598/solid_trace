# frozen_string_literal: true

module McpTools
  # The install's email settings, for instance admins.
  class GetEmailSettings < Base
    include EmailSettingsTool

    description "The SMTP server and sender SolidTrace sends every Organization's email with: host, port, username " \
                "and sender, whether a password is saved and readable (it's never shown), and whether email is set " \
                "up or what's missing (problem). They're the install's, so they take no Organization. " \
                "Only an instance admin can; whoami says whether you are one."
    read_only!

    def run
      require_instance_admin!("read the email settings")
      settings_answer
    end
  end
end
