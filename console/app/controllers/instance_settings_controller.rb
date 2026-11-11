# frozen_string_literal: true

# Settings → Instance: the install's settings, for instance admins. SQL
# queries, the default Retention of Logs and traces, log-all-reads,
# log-MCP-reads, whether Console sends its own Issues and traces, the
# text-match budget, the Log read threads, the hours on local disk, and
# Console's and ingest's URLs apply at once, so they change here. Sign-up and
# everything that needs a restart, HTTPS among them, are shown read-only
# (InstanceSettingsAnswer builds what's shown).
class InstanceSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org
  before_action :require_instance_admin

  def show
    render inertia: "Settings/Instance", props: {
      settings: InstanceSettingsAnswer.new(request_url: request.base_url).as_json,
    }
  end

  # Only the settings that apply at once change; anything else, Sign-up
  # included, is ignored. A Console URL on another host that strands
  # Passkeys comes back as the passkeys_stop_working error, with their counts,
  # for the page to confirm and send again with confirm_passkeys_stop_working.
  def update
    settings = InstanceSetting.current
    if settings.update_editable(params.fetch(:instance_settings, {}).permit(*InstanceSetting::EDITABLE),
        confirm_passkeys_stop_working: params[:confirm_passkeys_stop_working].to_s == "true")
      redirect_to instance_settings_path(org_slug: @current_org.slug), notice: "Instance settings saved"
    else
      errors = settings.errors.to_hash
      errors[:passkeys_stop_working] = errors.delete(:base) if settings.errors.of_kind?(:base, :passkeys_stop_working)
      redirect_to instance_settings_path(org_slug: @current_org.slug), inertia: { errors: errors }
    end
  end
end
