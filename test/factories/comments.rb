FactoryBot.define do
  factory :comment do
    association :issue
    association :user
    content { "This is a comment" }
  end
end
