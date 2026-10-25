# frozen_string_literal: true

# Where a user in no Organization makes one: someone who just signed up, or
# was archived from their only Organization. Making one makes them its admin.
# Someone joining an existing Organization is invited by its admins instead.
class OrganizationsController < ApplicationController
  layout "auth"

  def new
    render inertia: "Organizations/New"
  end

  def create
    organization = Organization.make_for(current_user, name: params.dig(:organization, :name))

    if organization.persisted?
      redirect_to overview_path(org_slug: organization.slug), notice: "#{organization.name} is made. Add a Project to send it Events and Logs."
    else
      redirect_to new_organization_path, inertia: { errors: organization.errors.to_hash }
    end
  end
end
