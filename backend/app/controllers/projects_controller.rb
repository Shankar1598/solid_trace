class ProjectsController < ApplicationController
  def index
    @projects = current_organization.projects.order(created_at: :desc)
  end

  def new
    @project = Project.new
  end

  def create
    @project = current_organization.projects.build(project_params)

    if @project.save
      redirect_to projects_path(org_slug: current_organization.slug), notice: "Project created successfully"
    else
      render :new, status: :unprocessable_entity
    end
  end

  def show
    @project = Project.find(params[:id])
    @issues = @project.issues.order(created_at: :desc)
  end

  private

  def project_params
    params.require(:project).permit(:name)
  end
end
