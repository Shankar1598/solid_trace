# frozen_string_literal: true

# Pages outside any Organization that a member is sent to from one, as
# Require two-factor sends them to set-up, and on from there to Settings →
# User to add a Passkey: ?org= (or a form's org) names one of the user's
# Organizations, to return to once Two-factor is set up.
module ReturnsToOrganization
  extend ActiveSupport::Concern

  private

  # The user's Organization named by org, or nil.
  def return_organization
    return @return_organization if defined?(@return_organization)

    @return_organization = (current_user.organizations.find_by(slug: params[:org].to_s) if params[:org].present?)
  end

  # That Organization's Overview, else Settings → User.
  def return_url
    return_organization ? overview_path(org_slug: return_organization.slug) : user_settings_path
  end
end
