# frozen_string_literal: true

# For integration tests of Session lifetimes: when the last response's
# session cookie expires, as the browser reads its Set-Cookie header.
module SessionCookieHelper
  def session_cookie_expires
    header = Array(response.headers["Set-Cookie"]).join("\n").lines.find { |line| line.start_with?("session_id=") }
    assert header, "the response sets the session cookie"
    expires = header[/expires=([^;]+)/i, 1]
    assert expires, "the session cookie has an expiry, so it isn't permanent or for the browser's session only"
    Time.httpdate(expires)
  end
end
