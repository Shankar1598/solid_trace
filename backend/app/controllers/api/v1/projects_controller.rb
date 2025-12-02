module Api
  module V1
    class ProjectsController < ApplicationController
      before_action :set_organization
      before_action :set_project, only: [:show, :update]

      def index
        render json: @organization.projects
      end

      def show
        render json: @project
      end

      def update
        if @project.update(project_params)
          render json: @project
        else
          render json: { errors: @project.errors.full_messages }, status: :unprocessable_entity
        end
      end

      private

      def set_organization
        @organization = Organization.find_by!(slug: params[:org_slug])
      end

      def set_project
        @project = @organization.projects.find_by!(slug: params[:project_slug])
      end

      def project_params
        params.require(:project).permit(:name)
      end
    end
  end
end
