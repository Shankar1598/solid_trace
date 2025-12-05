FactoryBot.define do
  factory :issue do
    title { Faker::Lorem.sentence }
    culprit { Faker::Internet.url }
    kind { "error" }
    status { 0 }

    project
  end
end
