# frozen_string_literal: true

# Times the Log reads a request makes under its controller and action, such
# as dashboard_charts#data, and its request id (CallTimings).
module TimesCalls
  extend ActiveSupport::Concern

  included do
    prepend_before_action do
      CallTimings::Context.origin = "#{controller_path}##{action_name}"
      CallTimings::Context.request_id = request.request_id
    end
  end
end
