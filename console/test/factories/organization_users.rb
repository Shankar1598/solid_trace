# frozen_string_literal: true

FactoryBot.define do
  factory :organization_user do
    organization
    user
  end
end
