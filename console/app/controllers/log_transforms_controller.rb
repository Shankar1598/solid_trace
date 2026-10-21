# frozen_string_literal: true

# A Project's Log transform page: the program in an editor that EventStore's
# check underlines as it's typed, its running counts, and its last versions.
# Any member reads it and checks a draft; only the Organization's admins
# save and restore, through LogTransformSave.
class LogTransformsController < ApplicationController
  layout "dashboard"

  # How many of the newest versions the page lists.
  VERSIONS_SHOWN = 20

  before_action :set_current_org
  before_action :set_project

  def show
    render inertia: "LogTransform/Show", props: page_props
  end

  private

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
