# frozen_string_literal: true

# The operator's commands for instance admins, who change the install's
# settings, such as email, in Console. The first user to sign up is one. There
# is no UI to grant it yet.
namespace :instance_admins do
  print_admins = lambda do
    emails = User.where(instance_admin: true).order(:email).pluck(:email)
    puts emails.any? ? "Instance admins: #{emails.join(", ")}" : "No instance admins."
  end

  find_user = lambda do
    email = ENV["EMAIL"].to_s.strip.downcase
    abort "Give EMAIL=<a user's email>." if email.empty?
    User.find_by(email: email) || abort("No user has the email #{email}.")
  end

  desc "List the instance admins"
  task list: :environment do
    print_admins.call
  end

  desc "Make a user an instance admin: EMAIL=<their email>"
  task grant: :environment do
    find_user.call.update!(instance_admin: true)
    print_admins.call
  end

  desc "Stop a user being an instance admin: EMAIL=<their email>"
  task revoke: :environment do
    find_user.call.update!(instance_admin: false)
    print_admins.call
  end
end
