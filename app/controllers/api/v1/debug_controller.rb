module Api
  module V1
    class DebugController < ApplicationController
      skip_before_action :authenticate_request

      def trigger_error
        raise StandardError.new("This is a test error from the Garnet Backend!")
      end
    end
  end
end
