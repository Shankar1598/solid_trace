require "test_helper"

class SessionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = User.create!(name: "Test User", email: "test@example.com", password: "password123")
  end

  test "should login and logout" do
    # Login
    post login_url, params: { email: @user.email, password: "password123" }
    assert_redirected_to root_url
    session_id = cookies[:session_id]
    assert_not_nil session_id

    # Logout
    delete session_url
    assert_redirected_to login_url

    # Verify session is destroyed
    assert_predicate cookies[:session_id], :blank?
    assert_nil Session.find_by(id: session_id)

    # Verify we can't access protected page
    get root_url
    assert_redirected_to login_url
  end
end
