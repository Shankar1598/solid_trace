# frozen_string_literal: true

# This file should ensure the existence of records required to run the application in every environment (production,
# development, test). The code here should be idempotent so that it can be executed at any point in every environment.
# The data can then be loaded with the bin/rails db:seed command (or created alongside the database with db:setup).
#
# Example:
#
#   ["Action", "Comedy", "Drama", "Horror"].each do |genre_name|
#     MovieGenre.find_or_create_by!(name: genre_name)
#   end

org = Organization.find_or_create_by!(slug: "solid-trace-org", name: "SolidTrace Org")
project = org.projects.find_or_create_by!(slug: "solid-trace-project", name: "SolidTrace Project")
project.project_keys.find_or_create_by!(public_key: "testkey123", secret_key: "secret123")

user = User.find_or_create_by!(email: "admin@solidtrace.local") do |u|
  u.name = "Admin User"
  u.password = "password123"
  u.password_confirmation = "password123"
end
user.organizations << org unless user.organizations.include?(org)
