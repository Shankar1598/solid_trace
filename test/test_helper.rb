ENV["RAILS_ENV"] ||= "test"
require_relative "../config/environment"
require "rails/test_help"

module ActiveSupport
  class TestCase
    # Run tests in parallel with specified workers
    # parallelize(workers: :number_of_processors)

    # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
    # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
    # fixtures :all
    # fixtures :users, :organizations, :projects

    # Add more helper methods to be used by all tests here...
    include FactoryBot::Syntax::Methods
  end
end

module IntegrationTestHelper
  def sign_in_as(user, password: "password")
    post login_url, params: { email: user.email, password: password }
  end
end
