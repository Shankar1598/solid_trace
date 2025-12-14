# frozen_string_literal: true

class Project < ApplicationRecord
  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :project_keys, dependent: :destroy

  before_validation :generate_slug, on: :create

  def to_param
    slug
  end

  def platform
    "ruby"
  end

  private

  def generate_slug
    self.slug ||= name.parameterize
  end
end
