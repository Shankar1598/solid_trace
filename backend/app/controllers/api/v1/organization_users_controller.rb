module Api
  module V1
    class OrganizationUsersController < ApplicationController
      before_action :set_organization

      def create
        email = params[:email]
        if email.blank?
          render json: { error: "Email is required" }, status: :unprocessable_entity
          return
        end

        @current_org.invite_user(email)
        render json: { message: "Invitation sent" }, status: :ok
      rescue => e
        render json: { error: e.message }, status: :unprocessable_entity
      end

      private

      def set_organization
        @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
      end
    end
  end
end
