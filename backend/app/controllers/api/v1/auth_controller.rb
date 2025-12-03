require 'jwt'

module Api
  module V1
    class AuthController < ApplicationController
      # skip_before_action :authenticate_request, only: [:login, :register]

      def login
        user = User.find_by(email: params[:email])
        if user&.authenticate(params[:password])
          token = encode_token(user_id: user.id)
          render json: { token: token, user: { id: user.id, name: user.name, email: user.email, organizations: user.organizations.select(:id, :name, :slug) } }
        else
          render json: { error: 'Invalid email or password' }, status: :unauthorized
        end
      end

      def register
        user = User.new(user_params)
        if user.save
          token = encode_token(user_id: user.id)
          render json: { token: token, user: { id: user.id, name: user.name, email: user.email, organizations: user.organizations.select(:id, :name, :slug) } }, status: :created
        else
          render json: { errors: user.errors.full_messages }, status: :unprocessable_entity
        end
      end

      def me
        render json: { user: { id: @current_user.id, name: @current_user.name, email: @current_user.email, organizations: @current_user.organizations.select(:id, :name, :slug) } }
      end

      private

      def user_params
        params.require(:user).permit(:name, :email, :password)
      end

      def encode_token(payload)
        JWT.encode(payload, Rails.application.secret_key_base)
      end
    end
  end
end
