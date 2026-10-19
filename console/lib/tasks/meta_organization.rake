# frozen_string_literal: true

namespace :meta_organization do
  desc "Seed the Meta Organization once, with META_PROJECT_KEY as its Project's key. Without it, does nothing. db_prepare runs it"
  task seed: :environment do
    MetaOrganization.seed(ENV["META_PROJECT_KEY"])
  end
end
