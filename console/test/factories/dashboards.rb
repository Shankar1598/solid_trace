# frozen_string_literal: true

FactoryBot.define do
  factory :dashboard do
    organization
    name { "Checkout health" }
  end

  factory :dashboard_chart do
    dashboard
    project { association :project, organization: dashboard.organization }
    title { "Requests" }
    query { "['#{project.slug}']\n| summarize count() by bin_auto(time)" }
    viz { "line" }
  end
end
