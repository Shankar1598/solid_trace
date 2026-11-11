# frozen_string_literal: true

ENV["RAILS_ENV"] ||= "test"
require_relative "../config/environment"
require "rails/test_help"

# Without node_modules, Vite's test auto-build runs `bun x vite`, which finds the
# vite_ruby binstub on PATH and calls itself until memory runs out.
unless Rails.root.join("node_modules/.bin/vite").exist?
  abort "node_modules is missing: run `bun install` in console/ before running tests."
end

module ActiveSupport
  class TestCase
    # Run tests in parallel with specified workers
    # parallelize(workers: :number_of_processors)

    # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
    # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
    # fixtures :all
    # fixtures :users, :organizations, :projects

    # Add more helper methods to be used by all tests here...
    include FactoryBot::Syntax::Methods
  end
end

Dir[File.expand_path("support/**/*.rb", __dir__)].each { |file| require file }

module IntegrationTestHelper
  # Signs in through the sign-in page. For a user with Two-factor it finishes
  # the second step, as the user's Authenticator app would, with a code for
  # the newest step not yet used: now's, or the next one's when now's was.
  # Signing in twice within two steps needs a travel in between. Given
  # passkey (a WebAuthn::FakeClient), or for a user without the app, it
  # answers with a Passkey instead: passkey, else PasskeyTestHelper's
  # passkey_client.
  def sign_in_as(user, password: "password", passkey: nil)
    post login_url, params: { email: user.email, password: password }
    return unless response.redirect? && response.location == second_step_url

    user.reload
    if user.authenticator_app? && !passkey
      step = [ Time.current.to_i / User::TwoFactor::STEP, user.authenticator_last_step.to_i + 1 ].max
      post second_step_url, params: { code: ROTP::TOTP.new(user.authenticator_secret).at(step * User::TwoFactor::STEP) }
    else
      finish_second_step_with_passkey(passkey || passkey_client)
    end
  end

  # The second step's "Use a passkey", answered by client, a
  # WebAuthn::FakeClient holding one of the user's Passkeys.
  def finish_second_step_with_passkey(client)
    post second_step_passkey_options_url, as: :json
    challenge = response.parsed_body["challenge"]
    post second_step_url, params: { credential: client.get(challenge: challenge, user_verified: true) }, as: :json
  end
end
