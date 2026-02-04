# frozen_string_literal: true

FactoryBot.define do
  factory :comment do
    association :issue
    organization_user do
      if issue&.project
        association :organization_user, organization: issue.project.organization
      else
        association :organization_user
      end
    end
    content { "This is a comment" }
  end
end
