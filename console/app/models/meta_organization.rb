# frozen_string_literal: true

# The Organization SolidTrace makes for itself on a VM install, whose one
# Project receives SolidTrace's own Logs (ADR 0011). db_prepare seeds it with
# the project key deploy/install wrote to deploy/.env, and Vector sends with
# that key. Otherwise it is an ordinary Organization.
module MetaOrganization
  NAME = "SolidTrace"

  # Makes the Organization and its Project, whose project key is key, with the
  # oldest user, if any, as its admin. Once: the instance settings record it,
  # so a deleted one is not made again. Its Project sets no Retention, so it
  # has the instance's default. Without a key, as in local development, does
  # nothing.
  def self.seed(key)
    return if key.blank?

    settings = InstanceSetting.current
    return if settings.meta_organization_seeded?

    Organization.transaction do
      organization = Organization.create!(name: NAME, slug: Organization.free_slug(NAME))
      project = organization.projects.build(name: NAME)
      project.project_keys.build(public_key: key)
      project.save!
      oldest_user = User.order(:created_at, :id).first
      organization.add_admin(oldest_user) if oldest_user
      settings.update!(meta_organization_seeded: true, meta_organization_id: organization.id)
    end
  end

  # Makes user the Meta Organization's admin while it has never had a member,
  # as when the person who set up a fresh install signs up first.
  def self.join_first(user)
    organization = InstanceSetting.current.meta_organization
    return if organization.nil? || organization.all_organization_users.exists?

    organization.add_admin(user)
  end
end
