FactoryBot.define do
  factory :project_key do
    public_key { SecureRandom.hex(16) }
    secret_key { SecureRandom.hex(32) }
    project
  end
end
