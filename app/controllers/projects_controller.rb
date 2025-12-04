class ProjectsController < ApplicationController
  layout "dashboard"
  before_action :set_organization
  before_action :set_project, only: [:show]

  def index
    @projects = @current_org.projects.order(created_at: :desc)
  end

  def show
    @project_keys = @project.project_keys.order(created_at: :desc)
  end

  def update
    if @project.update(project_params)
      redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Project updated successfully"
    else
      render :show, status: :unprocessable_entity
    end
  end

  def new
    @project = @current_org.projects.new
  end

  def create
    @project = @current_org.projects.new(project_params)
    if @project.save
      redirect_to projects_path(org_slug: @current_org.slug), notice: "Project created successfully"
    else
      render :new, status: :unprocessable_entity
    end
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_project
    @project = @current_org.projects.find(params[:id])
  end

  def project_params
    params.require(:project).permit(:name, :platform)
  end
end
