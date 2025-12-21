# frozen_string_literal: true

FactoryBot.define do
  factory :event_fingerprint do
    fingerprint { Digest::MD5.hexdigest(Faker::Lorem.sentence) }
    issue
    project { issue.project }
  end
end
