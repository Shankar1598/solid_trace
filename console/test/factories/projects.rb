# frozen_string_literal: true

FactoryBot.define do
  factory :project do
    sequence(:name) { |n| "#{Faker::App.name} #{n}" }
    slug { Faker::Internet.slug(words: name, glue: "-") }
    organization
  end
end
