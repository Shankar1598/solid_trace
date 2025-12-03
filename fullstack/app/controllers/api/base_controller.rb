require 'jwt'

module Api
  class BaseController < ActionController::API
    before_action :authenticate_request

    attr_reader :current_user

    private

    def authenticate_request
      header = request.headers['Authorization']
      header = header.split(' ').last if header

      begin
        decoded = JWT.decode(header, Rails.application.secret_key_base)[0]
        @current_user = User.find(decoded['user_id'])
      rescue ActiveRecord::RecordNotFound, JWT::DecodeError
        render json: { error: 'Unauthorized' }, status: :unauthorized
      end
    end
  end
end
