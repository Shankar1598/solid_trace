# frozen_string_literal: true

class ProjectKey < ApplicationRecord
  belongs_to :project

  before_validation :generate_keys, on: :create

  def dsn
    "http://#{public_key}@#{project.organization.slug}.localhost:3000/1"
  end

  def label
    "Default"
  end

  private

  # A key made without values gets fresh random ones.
  def generate_keys
    self.public_key ||= SecureRandom.hex(16)
    self.secret_key ||= SecureRandom.hex(32)
  end
end
