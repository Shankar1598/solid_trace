# frozen_string_literal: true

class Project < ApplicationRecord
  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy
  has_many :project_keys, dependent: :destroy
  has_many :dashboard_charts, dependent: :destroy
  has_many :monitors, class_name: "LogMonitor", dependent: :destroy

  before_validation :generate_slug, on: :create

  # Whole days; nil means the instance default (InstanceSetting).
  validates :log_retention_days, numericality: { only_integer: true, greater_than: 0 }, allow_nil: true

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
