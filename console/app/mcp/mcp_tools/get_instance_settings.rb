# frozen_string_literal: true

module McpTools
  # The install's settings, for instance admins.
  class GetInstanceSettings < Base
    include InstanceSettingsTool

    description "The install's settings, as Console's Settings → Instance page shows them. editable: the settings " \
                "that apply at once, which update_instance_settings changes (SQL queries, the default Retention in " \
                "days for Projects that set none, log-all-reads). sign_up: whether Sign-up is open; it changes only " \
                "from a shell on the server, with the command given. restart_to_change: groups of settings Console " \
                "and EventStore read at start, each row naming its env var or event_store.yml key and whether it's " \
                "the default; a secret's value is only whether it's set. They're the install's, so they take no " \
                "Organization. Only an instance admin can; whoami says whether you are one."
    read_only!

    def run
      require_instance_admin!("read the instance settings")
      settings_answer
    end
  end
end
