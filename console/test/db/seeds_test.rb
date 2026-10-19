# frozen_string_literal: true

require "test_helper"

# db/seeds.rb makes the users and Organization that development and the
# end-to-end flows sign in with. db:prepare runs it on a fresh database, so on
# a production install it must make nothing.
class SeedsTest < ActiveSupport::TestCase
  test "in development it makes the seed Organization and users" do
    load_seeds("development")

    assert Organization.exists?(slug: "solid-trace-org")
    assert User.exists?(email: "admin@solidtrace.local")
  end

  test "in production it makes nothing" do
    load_seeds("production")

    assert_not Organization.exists?(slug: "solid-trace-org")
    assert_not User.exists?(email: "admin@solidtrace.local")
  end

  private

  def load_seeds(env)
    Rails.stub(:env, ActiveSupport::EnvironmentInquirer.new(env)) do
      load Rails.root.join("db/seeds.rb")
    end
  end
end
