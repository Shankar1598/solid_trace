# frozen_string_literal: true

# Pages outside any Organization that a member is sent to from one, as
# Require two-factor sends them to set-up, and on from there to Settings →
# User to add a Passkey: ?org= (or a form's org) names one of the user's
# Organizations, to return to once Two-factor is set up.
#
# The approval page sends a member there too, and keeps its own path in the
# Rails session (#keep_return_after_set_up), never in a URL parameter, so
# the return cannot go to a URL that another site gives.
module ReturnsToOrganization
  extend ActiveSupport::Concern

  KEPT_RETURN = :return_after_set_up
  KEPT_RETURN_LASTS = 10.minutes

  private

  # The user's Organization named by org, or nil.
  def return_organization
    return @return_organization if defined?(@return_organization)

    @return_organization = (current_user.organizations.find_by(slug: params[:org].to_s) if params[:org].present?)
  end

  # Where the user goes once Two-factor is on: the kept path when it applies
  # (#kept_return_path), else the Organization's Overview, else Settings →
  # User. The kept path is deleted in both cases: once Two-factor is on, the
  # user needs no return to set it up again.
  def return_url
    path = kept_return_path
    session.delete(KEPT_RETURN)
    path || (return_organization ? overview_path(org_slug: return_organization.slug) : user_settings_path)
  end

  # Keeps path, which Console made, as the place to return to once
  # Two-factor is on, for organization only and for 10 minutes.
  def keep_return_after_set_up(path, organization)
    session[KEPT_RETURN] = { "path" => path, "organization_id" => organization.id, "at" => Time.current.to_i }
  end

  # Deletes the kept path when it is path: the place it returns to has
  # ended, so the set-up returns as if nothing was kept.
  def drop_kept_return(path)
    kept = session[KEPT_RETURN]
    session.delete(KEPT_RETURN) if kept.is_a?(Hash) && kept["path"] == path
  end

  # The kept path, when org names its Organization and it is less than 10
  # minutes old, else nil.
  def kept_return_path
    kept = session[KEPT_RETURN]
    return unless kept.is_a?(Hash) && return_organization
    return unless kept["organization_id"] == return_organization.id
    return unless Time.current.to_i - kept["at"].to_i < KEPT_RETURN_LASTS

    kept["path"]
  end
end
