# frozen_string_literal: true

# Settings → Instance: the install's settings, for instance admins. SQL
# queries, the default Retention of Logs and traces, log-all-reads,
# log-MCP-reads, whether Console sends its own Issues and traces, the
# text-match budget, the Log read threads, the hours on local disk,
# Console's and ingest's URLs, the Dispatcher's URL, and the extra Redaction
# patterns apply at once, so they change here. Sign-up and
# everything that needs a restart, HTTPS among them, are shown read-only
# (InstanceSettingsAnswer builds what's shown).
class InstanceSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org
  before_action :require_instance_admin, only: :show
  # A save by anyone else is 403, with nothing changed.
  before_action -> { head :forbidden unless current_user.instance_admin? }, only: :update

  # With a Dispatcher URL, the page loads the Dispatcher's status after it
  # shows, so a Dispatcher that does not answer never slows the page.
  def show
    settings = InstanceSetting.current
    render inertia: "Settings/Instance", props: {
      settings: InstanceSettingsAnswer.new(settings, request_url: request.base_url).as_json,
      dispatcher: Dispatcher.on?(settings: settings) ? InertiaRails.defer { dispatcher_status(settings) } : nil,
    }
  end

  # Only the settings that apply at once change; anything else, Sign-up
  # included, is ignored. A Console URL on another host that strands
  # Passkeys comes back as the passkeys_stop_working error, with their counts,
  # for the page to confirm and send again with confirm_passkeys_stop_working.
  def update
    settings = InstanceSetting.current
    changes = params.fetch(:instance_settings, {})
      .permit(*(InstanceSetting::EDITABLE - [ :redaction_patterns ]), redaction_patterns: [])
    if settings.update_editable(changes,
        confirm_passkeys_stop_working: params[:confirm_passkeys_stop_working].to_s == "true")
      redirect_to instance_settings_path(org_slug: @current_org.slug), notice: "Instance settings saved"
    else
      errors = settings.errors.to_hash
      errors[:passkeys_stop_working] = errors.delete(:base) if settings.errors.of_kind?(:base, :passkeys_stop_working)
      redirect_to instance_settings_path(org_slug: @current_org.slug), inertia: { errors: errors }
    end
  end

  private

  # The Dispatcher's status, called as the signed-in user in this page's
  # Organization: { version:, token_active:, token_error: }, or { error: }
  # when the Dispatcher does not answer as it should.
  def dispatcher_status(settings)
    status = Dispatcher.new(user: current_user, organization: @current_org, settings: settings).status
    solidtrace = status["solidtrace"].is_a?(Hash) ? status["solidtrace"] : {}
    { version: status["version"].presence, token_active: solidtrace["token_active"] == true,
      token_error: solidtrace["error"].presence }.compact
  rescue Dispatcher::Unavailable, Dispatcher::Refused => e
    { error: e.message }
  end
end
