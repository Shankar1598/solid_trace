# frozen_string_literal: true

module InertiaShare
  extend ActiveSupport::Concern

  included do
    inertia_share do
      {
        current_user: current_user&.as_json(only: [ :id, :name, :email ]),
        current_org: @current_org&.as_json(only: [ :id, :name, :slug ]),
        # For the command palette, which jumps to any of them from any page.
        org_projects: @current_org&.projects&.order(:name)&.map { |p| { name: p.name, slug: p.slug } },
        flash: {
          notice: flash[:notice],
          alert: flash[:alert],
        },
      }
    end
  end
end
