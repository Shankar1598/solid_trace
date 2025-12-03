module Api
  module V1
    class OrganizationCrudController < Api::BaseController
      before_action :set_organization

      private

      def set_organization
        @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
      end
    end
  end
end
