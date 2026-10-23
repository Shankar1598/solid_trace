# frozen_string_literal: true

module McpTools
  # What the instance settings tools share. The settings are the install's,
  # for every Organization, so they take no Organization, and only an
  # instance admin reads or changes them, as on Settings → Instance.
  module InstanceSettingsTool
    private

    # The settings as Settings → Instance shows them (InstanceSettingsAnswer),
    # linking to that page in the user's first Organization, as it's under
    # one.
    def settings_answer(settings = InstanceSetting.current)
      organization = user.organizations_by_name.first
      json(InstanceSettingsAnswer.new(settings, console_url: console_url).as_json,
        link: (links.instance_settings(organization) if organization))
    end
  end
end
