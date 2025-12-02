FactoryBot.define do
  factory :issue_event do
    data { { message: Faker::Lorem.sentence, extra: { key: "value" } } }
    issue
  end
end
