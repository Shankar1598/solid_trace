FactoryBot.define do
  factory :issue do
    title { Faker::Lorem.sentence }
    culprit { Faker::Internet.url }
    event_type { "error" }
    status { 0 }
    level { 1 }
    project
  end
end
