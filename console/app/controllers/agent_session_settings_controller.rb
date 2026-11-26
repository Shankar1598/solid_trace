# frozen_string_literal: true

# Organization settings → Agent sessions: the turn-on of Agent sessions,
# which makes the Organization's Agent Project, its captured repositories,
# through AgentSessionsSettings, and its Evidence Retention. Each member
# reads the page; only the Organization's Admins turn on and save, and
# anyone else's write is 403. Someone outside the Organization gets 404
# (set_current_org).
class AgentSessionSettingsController < ApplicationController
  layout "dashboard"

  # How many of the newest confirms the page lists.
  CONFIRMATIONS_SHOWN = 20

  before_action :set_current_org
  before_action :require_admin, only: [ :create, :update, :evidence_retention ]
  before_action :require_agent_project, only: [ :update, :evidence_retention ]

  def show
    render inertia: "Settings/AgentSessions", props: page_props
  end

  # The turn-on: makes the Agent Project with the captured repositories.
  def create
    answer AgentSessionsSettings.turn_on(organization: @current_org, user: current_user, entries: entries, confirmed: params[:confirmed]),
      "Agent sessions turned on"
  end

  # A save of the captured repositories.
  def update
    answer AgentSessionsSettings.save(project: @current_org.agent_project, user: current_user, entries: entries,
      confirmed: params[:confirmed]), "Captured repositories saved"
  end

  def evidence_retention
    answer AgentSessionsSettings.save_evidence_retention(project: @current_org.agent_project, days: params[:evidence_retention_days]),
      "Evidence Retention saved"
  end

  private

  # A refused write shows the page again with its errors: rendered, not
  # redirected, as the errors of many entries overflow the session's cookie.
  def answer(result, notice)
    if result.done?
      redirect_to page_path, notice: notice
    else
      render inertia: "Settings/AgentSessions", props: page_props.merge(errors: result.errors)
    end
  end

  # The entries as the form sends them: an array, where [""] is an empty
  # list, as a form can't send an empty array.
  def entries
    Array(params[:captured_repositories]).map(&:to_s).reject(&:blank?)
  end

  def require_admin
    head :forbidden unless admin?
  end

  def require_agent_project
    redirect_to(page_path, alert: "Turn on Agent sessions first.") unless @current_org.agent_project
  end

  def admin?
    @current_org.organization_users.find_by(user: current_user)&.admin? || false
  end

  def page_path
    agent_session_settings_path(org_slug: @current_org.slug)
  end

  def page_props
    project = @current_org.agent_project
    {
      agent_sessions: {
        on: project.present?,
        project: project && ProjectSerializer.new(project).as_json,
        captured_repositories: project&.captured_repositories || [],
        evidence_retention_days: project&.evidence_retention_days,
        confirmations: project ? confirmations(project) : [],
      },
      notice_text: AgentSessionsSettings::NOTICE,
      can_manage: admin?,
    }
  end

  def confirmations(project)
    project.capture_confirmations.newest_first.includes(:user).limit(CONFIRMATIONS_SHOWN).map do |confirmation|
      { user: confirmation.user&.name, at: confirmation.created_at.utc.iso8601, entries: confirmation.entries }
    end
  end
end
