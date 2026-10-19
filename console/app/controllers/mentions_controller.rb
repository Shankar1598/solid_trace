# frozen_string_literal: true

class MentionsController < ApplicationController
  before_action :set_current_org

  def index
    @users = @current_org.users

    # Support query filtering for remote-filtering mode
    if params[:query].present?
      @users = @users.where("name LIKE ? OR email LIKE ?", "%#{params[:query]}%", "%#{params[:query]}%")
    end

    respond_to do |format|
      format.html { render layout: false }
      format.json { render json: @users.as_json(only: [:id, :name, :email]) }
    end
  end
end
