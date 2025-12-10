# frozen_string_literal: true

class ProjectKey < ApplicationRecord
  belongs_to :project

  def dsn
    "http://#{public_key}@#{project.organization.slug}.localhost:3000/1"
  end
end
