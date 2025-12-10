# frozen_string_literal: true

FactoryBot.define do
  factory :project do
    name { Faker::App.name }
    slug { Faker::Internet.slug(words: name, glue: "-") }
    organization
  end
end
