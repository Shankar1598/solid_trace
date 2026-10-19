# frozen_string_literal: true

module InertiaShare
  extend ActiveSupport::Concern

  included do
    inertia_share do
      {
        current_user: current_user&.as_json(only: [ :id, :name, :email ]),
        current_org: @current_org&.as_json(only: [ :id, :name, :slug ]),
        # For the Organization selector and the command palette.
        organizations: @current_org && current_user.organizations.order(:name).map { |o| { name: o.name, slug: o.slug } },
        # For the command palette, which jumps to any of them from any page.
        org_projects: @current_org&.projects&.order(:name)&.map { |p| { name: p.name, slug: p.slug } },
        # For the command palette and the explorer's Add to dashboard.
        org_dashboards: @current_org&.dashboards&.order(:name)&.map { |d| { id: d.id, name: d.name } },
        # For the command palette.
        org_monitors: @current_org&.monitors&.order(:name)&.map { |m| { id: m.id, name: m.name } },
        flash: {
          notice: flash[:notice],
          alert: flash[:alert],
        },
      }
    end
  end
end
