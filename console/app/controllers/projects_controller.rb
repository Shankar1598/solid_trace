# frozen_string_literal: true

class ProjectsController < ApplicationController
  layout "dashboard"
  before_action :set_organization
  before_action :set_project, only: [ :show, :update ]

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
    }
  end

  def new
    render inertia: "Projects/New"
  end

  def create
    @project = @current_org.projects.new(project_params)
    # Its first key, so SDKs and shippers can send to it at once.
    @project.project_keys.build
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
