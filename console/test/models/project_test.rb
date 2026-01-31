# frozen_string_literal: true

require "test_helper"

class ProjectTest < ActiveSupport::TestCase
  test "generates slug on create" do
    project = create(:project, name: "My Project", slug: nil)

    assert_equal "my-project", project.slug
  end

  test "to_param uses slug" do
    project = create(:project, slug: "my-project")

    assert_equal "my-project", project.to_param
  end

  test "platform returns ruby" do
    project = create(:project)

    assert_equal "ruby", project.platform
  end
end
