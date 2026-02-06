# frozen_string_literal: true

FactoryBot.define do
  factory :comment do
    association :issue
    organization_user do
      issue_org = issue&.project&.organization
      issue_org ? association(:organization_user, organization: issue_org) : association(:organization_user)
    end
    content { "This is a comment" }
  end
end
