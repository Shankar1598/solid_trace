# frozen_string_literal: true

module McpTools
  # What the email settings tools share. The settings are the install's, for
  # every Organization, so they take no Organization, and only an instance
  # admin reads or changes them, as on Settings → Email (ADR 0015). The
  # password is never answered: only whether one is saved, and readable.
  module EmailSettingsTool
    private

    # Raises a tool error unless the user is an instance admin. doing says
    # what was refused, such as "send a test email".
    def require_instance_admin!(doing)
      raise ToolError, "Only an instance admin can #{doing}." unless user.instance_admin?
    end

    # The settings as Settings → Email shows them, linking to that page in the
    # user's first Organization, as it's under one.
    def settings_answer(settings = InstanceSetting.current)
      shown = EmailSettingsSerializer.new(settings).as_json
      organization = user.organizations_by_name.first
      json(shown.merge(set_up: shown[:problem].nil?), link: (links.email_settings(organization) if organization))
    end
  end
end
