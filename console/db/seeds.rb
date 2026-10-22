# frozen_string_literal: true

# The Organization, Project and users development and the end-to-end flows
# sign in with, whose passwords and project key are public. db:prepare runs
# this on a fresh database, so outside development and test it makes nothing.
return unless Rails.env.local?

org = Organization.find_or_create_by!(slug: "solid-trace-org", name: "SolidTrace Org")
project = org.projects.find_or_create_by!(slug: "solid-trace-project", name: "SolidTrace Project")
project.project_keys.find_or_create_by!(public_key: "testkey123", secret_key: "secret123")

admin = User.find_or_create_by!(email: "admin@solidtrace.local") do |u|
  u.name = "Admin User"
  u.password = "password123"
  u.password_confirmation = "password123"
end
# The admin changes the install's settings, such as email, as the first user
# to sign up would.
admin.update!(instance_admin: true)
member =User.find_or_create_by!(email: "member@solidtrace.local") do |u|
  u.name = "Member User"
  u.password = "password123"
  u.password_confirmation = "password123"
end
OrganizationUser.find_or_create_by!(organization: org, user: admin, role: :admin)
OrganizationUser.find_or_create_by!(organization: org, user: member, role: :member)
