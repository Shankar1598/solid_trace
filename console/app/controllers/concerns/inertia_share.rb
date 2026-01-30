# frozen_string_literal: true

module InertiaShare
  extend ActiveSupport::Concern

  included do
    inertia_share do
      {
        current_user: current_user&.as_json(only: [ :id, :name, :email ]),
        current_org: @current_org&.as_json(only: [ :id, :name, :slug ]),
        flash: {
          notice: flash[:notice],
          alert: flash[:alert],
        },
      }
    end
  end
end
