# frozen_string_literal: true

# The Organization SolidTrace makes for itself on a VM install, whose one
# Project receives SolidTrace's own Logs (ADR 0011). db_prepare seeds it with
# the project key deploy/install wrote to deploy/.env, and Vector sends with
# that key. Otherwise it is an ordinary Organization.
module MetaOrganization
  NAME = "SolidTrace"

  # Makes the Organization and its Project, whose project key is key.
  def self.seed(key)
    return if key.blank?

    Organization.transaction do
      organization = Organization.create!(name: NAME, slug: Organization.free_slug(NAME))
      project = organization.projects.build(name: NAME)
      project.project_keys.build(public_key: key)
      project.save!
    end
  end
end
