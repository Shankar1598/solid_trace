# frozen_string_literal: true

module Authentication
  extend ActiveSupport::Concern

  included do
    helper_method :current_user
  end

  def current_user
    @current_user
  end

  class_methods do
    def allow_unauthenticated_access(**options)
      skip_before_action :authenticate_user!, **options
    end
  end

  private
    def authenticate_user!
      resume_session || request_authentication
    end

    def resume_session
      if session_record = find_session_by_cookie
        Current.session = session_record
        @current_user = session_record.user
      end
    end

    # An expired Session is refused and ended, whatever the cookie says, so a
    # copied cookie stops working on time.
    def find_session_by_cookie
      session_record = Session.find_by(id: cookies.signed[:session_id]) if cookies.signed[:session_id]
      if session_record&.expired?
        session_record.destroy
        cookies.delete(:session_id)
        nil
      else
        session_record
      end
    end

    def request_authentication
      session[:return_to_after_authenticating] = request.url
      redirect_to login_path
    end

    def after_authentication_url
      session.delete(:return_to_after_authenticating) || root_url
    end

    # The Session and its cookie expire together, 24 hours from now or 7 days
    # with Remember me.
    def start_new_session_for(user, remember_me: false)
      expires_at = Session.lifetime(remember_me: remember_me).from_now
      user.sessions.create!(user_agent: request.user_agent, ip_address: request.remote_ip, expires_at: expires_at).tap do |session|
        Current.session = session
        cookies.signed[:session_id] = { value: session.id, expires: expires_at, httponly: true, same_site: :lax }
      end
    end

    # The sign-in page's Remember me, for a password or a Passkey.
    def remember_me?
      ActiveModel::Type::Boolean.new.cast(params[:remember_me]) || false
    end

    # Signing in's second step, pending after a right password from a user
    # with Two-factor: the user, when, and Remember me, in the Rails session
    # for SECOND_STEP_LASTS. Success or a new sign-in clears it.
    SECOND_STEP_LASTS = 5.minutes

    def start_second_step_for(user, remember_me:)
      session[:second_step] = { "user_id" => user.id, "started_at" => Time.current.to_i, "remember_me" => remember_me }
    end

    # The pending second step's user and Remember me, or nil when none is
    # pending or it has expired.
    def pending_second_step
      pending = session[:second_step]
      return unless pending.is_a?(Hash) && Time.at(pending["started_at"].to_i) > SECOND_STEP_LASTS.ago

      user = User.find_by(id: pending["user_id"])
      [ user, pending["remember_me"] == true ] if user
    end

    def clear_second_step
      session.delete(:second_step)
    end

    def terminate_session
      if Current.session
        Current.session.destroy
        cookies.delete(:session_id)
      end
    end
end
