# frozen_string_literal: true

class ProjectsController < ApplicationController
  layout "dashboard"
  before_action :set_organization
  before_action :set_project, only: [ :show, :update, :destroy ]

  def index
    @projects = @current_org.projects.order(created_at: :desc)

    render inertia: "Projects/Index", props: {
      projects: @projects.map { |p| ProjectSerializer.new(p).as_json },
    }
  end

  def show
    render inertia: "Projects/Show", props: {
      project: ProjectSerializer.new(@project, include_keys: true, ingest_url: public_ingest_url).as_json,
      default_log_retention_days: InstanceSetting.current.default_log_retention_days,
      can_manage_project: policy(@project).update?,
      can_delete_project: policy(@project).destroy?,
      can_manage_keys: policy(ProjectKey.new(project: @project)).create?,
    }
  end

  def new
    render inertia: "Projects/New"
  end

  def create
    @project = @current_org.projects.build_with_key(project_params)
    if @project.save
      redirect_to projects_path(org_slug: @current_org.slug), notice: "Project created successfully"
    else
      redirect_to new_project_path(org_slug: @current_org.slug), inertia: { errors: @project.errors.to_hash }
    end
  end

  def update
    authorize @project

    if @project.update(project_params)
      redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Project updated successfully"
    else
      redirect_to project_path(@project, org_slug: @current_org.slug), inertia: { errors: @project.errors.to_hash }
    end
  end

  # Deletes the Project with its Issues, keys, Monitors and Dashboard charts.
  # EventStore deletes its Logs and Events in its next daily cleanups, once the
  # Project's row is gone.
  def destroy
    authorize @project

    @project.destroy!
    redirect_to projects_path(org_slug: @current_org.slug), notice: "Project deleted."
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:slug])
  end

  def project_params
    params.require(:project).permit(:name, :platform, :log_retention_days)
  end
end
