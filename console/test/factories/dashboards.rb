# frozen_string_literal: true

FactoryBot.define do
  factory :dashboard do
    organization
    name { "Checkout health" }
  end

  factory :dashboard_section do
    dashboard
    title { "Errors" }
    sequence(:position) { |n| n }
  end

  factory :dashboard_chart do
    dashboard
    project { association :project, organization: dashboard.organization }
    title { "Requests" }
    query { "['#{project.slug}']\n| summarize count() by bin_auto(time)" }
    viz { "line" }

    # A Text block: Markdown, with no Project, Query or title.
    trait :text do
      viz { "text" }
      project { nil }
      query { nil }
      title { nil }
      text { "**Read me** first." }
    end

    # A SQL Chart: ClickHouse SQL in query.
    trait :sql do
      language { "sql" }
      query { "SELECT level, count() AS n FROM logs WHERE (empty({level:Array(String)}) OR level IN {level:Array(String)}) GROUP BY level" }
      viz { "bar" }
    end
  end
end
