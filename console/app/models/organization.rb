# frozen_string_literal: true

class Organization < ApplicationRecord
  has_many :projects, dependent: :destroy
  has_many :organization_users, -> { kept }, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :all_organization_users, class_name: "OrganizationUser", dependent: :restrict_with_error
  has_many :users, through: :organization_users
  has_many :integrations, dependent: :destroy
  has_many :dashboards, dependent: :destroy
  has_many :monitors, class_name: "LogMonitor", dependent: :destroy

  validates :name, presence: true
  validates :slug, presence: true, uniqueness: true, format: { with: /\A[a-z0-9-]+\z/, message: "only allows lowercase letters, numbers, and hyphens" }

  # A new Organization with user as its admin. Its slug comes from its name,
  # with a number when the slug is taken, since a user can't see the other
  # Organizations, or when a top-level page uses it. It is returned unsaved,
  # with its errors, when it isn't valid.
  def self.make_for(user, name:)
    organization = new(name: name.to_s.strip)
    organization.slug = free_slug(organization.name)
    transaction do
      organization.save && organization.organization_users.create!(user: user, role: :admin)
    end
    organization
  end

  # The first of base, base-2, base-3… that no Organization has and no
  # top-level page uses.
  def self.free_slug(name)
    base = name.parameterize.tr("_", "-").squeeze("-").delete_prefix("-").delete_suffix("-").presence || "organization"
    taken = where("slug = ? OR slug LIKE ?", base, "#{sanitize_sql_like(base)}-%").pluck(:slug).to_set + top_level_paths
    (1..).lazy.map { |n| n == 1 ? base : "#{base}-#{n}" }.find { |slug| taken.exclude?(slug) }
  end

  # The first segments of Console's paths outside an Organization, such as
  # login and passwords. An Organization's pages are under /:org_slug, so a
  # slug that is one of them would be hidden behind that page's routes.
  def self.top_level_paths
    Rails.application.routes.routes.filter_map { |route| route.path.spec.to_s[%r{\A/([a-z0-9_-]+)}, 1] }.to_set
  end
end
