# frozen_string_literal: true

FactoryBot.define do
  factory :issue_fingerprint do
    fingerprint { Digest::MD5.hexdigest(Faker::Lorem.sentence) }
    issue
    project { issue.project }
  end
end
