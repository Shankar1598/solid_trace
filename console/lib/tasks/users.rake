# frozen_string_literal: true

# The operator's commands for users. No Console page or MCP tool can remove
# a user's Two-factor, so a stolen admin Session or API token can't either:
# a user who has lost every factor and every Recovery code asks the operator.
namespace :users do
  desc "Remove a user's Two-factor (Authenticator app, Passkeys, Recovery codes) and end their Sessions: EMAIL=<their email>"
  task reset_two_factor: :environment do
    email = ENV["EMAIL"].to_s.strip.downcase
    abort "Give EMAIL=<a user's email>." if email.empty?
    user = User.find_by(email: email) || abort("No user has the email #{email}.")

    user.reset_two_factor
    puts "Two-factor is off for #{user.email}, and their Sessions have ended: they sign in with their password alone."
  end
end
