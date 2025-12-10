# frozen_string_literal: true

FactoryBot.define do
  factory :event do
    data { { message: Faker::Lorem.sentence, extra: { key: "value" } } }
    issue
  end
end
