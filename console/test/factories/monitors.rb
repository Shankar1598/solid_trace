# frozen_string_literal: true

FactoryBot.define do
  factory :log_monitor, aliases: [ :monitor ] do
    project
    organization { project.organization }
    name { "Checkout errors" }
    kind { "threshold" }
    query { "['#{project.slug}']\n| where level == 'error'\n| summarize count()" }
    interval_minutes { 1 }
    range_minutes { 5 }
    operator { ">" }
    threshold { 10 }

    trait :match do
      kind { "match" }
      query { "['#{project.slug}']\n| where level == 'error'" }
      operator { nil }
      threshold { nil }
    end
  end
end
