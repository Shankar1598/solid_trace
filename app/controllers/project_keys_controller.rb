# frozen_string_literal: true

class ProjectKeysController < ApplicationController
  before_action :set_organization
  before_action :set_project

  def create
    @project.project_keys.create!
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key created successfully"
  end

  def rotate
    key = @project.project_keys.find(params[:id])
    key.rotate!
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key rotated successfully"
  end

  def destroy
    key = @project.project_keys.find(params[:id])
    key.destroy
    redirect_to project_path(@project, org_slug: @current_org.slug), notice: "Key deleted successfully"
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end
end
