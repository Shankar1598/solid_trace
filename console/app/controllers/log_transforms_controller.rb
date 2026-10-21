# frozen_string_literal: true

# A Project's Log transform page: the program in an editor that EventStore's
# check underlines as it's typed, its running counts, and its last versions.
# Any member reads it, and checks and previews a draft; only the
# Organization's admins save and restore, through LogTransformSave.
class LogTransformsController < ApplicationController
  include EventStoreProxy

  layout "dashboard"

  # How many of the newest versions the page lists.
  VERSIONS_SHOWN = 20

  SAVED = "Log transform saved. It applies to Logs processed from now on; stored Logs don't change."
  UNCHANGED = "That program is already the current version, so nothing was saved."

  before_action :set_current_org
  before_action :set_project

  def show
    render inertia: "LogTransform/Show", props: page_props
  end

  # POST with program: EventStore's check of a draft, {errors, warnings},
  # each with byte offsets into the program, for the editor's underlines.
  def check
    render json: EventStore.check_log_transform(project_id: @project.id, program: params[:program].to_s)
  end

  # POST with program, and logs pasted as JSON objects: EventStore's preview
  # of a draft on those Logs, or on the recent arrivals without them. Each
  # Log before and after, dropped or the error, with the kept, dropped and
  # failed counts; or the check's errors when the draft doesn't compile.
  # Nothing is stored.
  def preview
    render json: EventStore.preview_log_transform(project_id: @project.id, program: params[:program].to_s, logs: pasted_logs)
  end

  # PATCH with program: saves it as the newest version, for admins.
  def update
    save(params[:program].to_s)
  end

  # POST with version_id: saves that version's program again as the newest.
  def restore
    save(@project.log_transform_versions.find(params[:version_id]).program)
  end

  private

  # Saves program through LogTransformSave. A program that doesn't compile
  # shows the page again with VRL's errors under program: rendered, not
  # redirected, as a few of them overflow the session's cookie.
  def save(program)
    result = LogTransformSave.call(project: @project, user: current_user, program: program)
    if result.done?
      redirect_to page_path, notice: result.outcome == :saved ? SAVED : UNCHANGED
    else
      messages = result.errors.pluck("text")
      render inertia: "LogTransform/Show", props: page_props.merge(errors: { program: messages })
    end
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error(e.message)
    redirect_to page_path, alert: "EventStore is unavailable, so the program couldn't be checked. Try again shortly."
  end

  # The logs of a preview's JSON body as they were pasted: read from the body
  # itself, as Rails' params drop the nulls in arrays. Nil without them.
  def pasted_logs
    body = JSON.parse(request.raw_post) if request.raw_post.present?
    body["logs"] if body.is_a?(Hash)
  rescue JSON::ParserError
    nil
  end

  def page_path
    project_log_transform_path(@project, org_slug: @current_org.slug)
  end

  def page_props
    {
      project: ProjectSerializer.new(@project).as_json,
      program: @project.log_transform&.program.to_s,
      versions: @project.log_transform_versions.newest_first.includes(:user).limit(VERSIONS_SHOWN).map { |v| version_json(v) },
      stats: stats,
      can_manage: policy(@project).update?,
    }
  end

  # The running counts, or nil when EventStore can't give them: the page
  # still shows the program.
  def stats
    EventStore.log_transform_stats(project_id: @project.id)
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error(e.message)
    nil
  end

  def version_json(version)
    { id: version.id, program: version.program, saved_by: version.user&.name, saved_at: version.created_at.utc.iso8601 }
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end
end
