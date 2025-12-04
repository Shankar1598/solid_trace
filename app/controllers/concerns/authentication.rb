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
        @current_user = session_record.user
      end
    end

    def find_session_by_cookie
      if token = cookies.signed[:session_id]
        Session.find_by(id: token)
      end
    end

    def request_authentication
      session[:return_to_after_authenticating] = request.url
      redirect_to login_path
    end

    def after_authentication_url
      session.delete(:return_to_after_authenticating) || root_url
    end

    def start_new_session_for(user)
      user.sessions.create!(user_agent: request.user_agent, ip_address: request.remote_ip).tap do |session|
        Current.session = session
        cookies.signed.permanent[:session_id] = { value: session.id, httponly: true, same_site: :lax }
      end
    end

    def terminate_session
      if Current.session
        Current.session.destroy
        cookies.delete(:session_id)
      end
    end
end
