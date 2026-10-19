# frozen_string_literal: true

# A Project's keys, which only the Organization's admins change
# (ProjectKeyPolicy).
class ProjectKeysController < ApplicationController
  before_action :set_current_org
  before_action :set_project

  def create
    key = @project.project_keys.build
    authorize key
    key.save!
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key created successfully"
  end

  def rotate
    key = @project.project_keys.find(params[:id])
    authorize key
    key.rotate!
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key rotated successfully"
  end

  def destroy
    key = @project.project_keys.find(params[:id])
    authorize key
    key.destroy
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key deleted successfully"
  end

  private

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end
end
