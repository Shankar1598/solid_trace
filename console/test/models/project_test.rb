# frozen_string_literal: true

require "test_helper"

class ProjectTest < ActiveSupport::TestCase
  test "generates slug on create" do
    project = create(:project, name: "My Project", slug: nil)

    assert_equal "my-project", project.slug
  end

  test "a Project needs a name" do
    project = build(:project, name: "", slug: nil)

    assert_not project.valid?
    assert_equal [ "can't be blank" ], project.errors[:name]
  end

  # A Project is found by its slug in its Organization, at
  # /:org_slug/projects/:slug, so a slug no Project could be reached at is
  # refused, on the name the slug is made from.
  test "a name that gives no slug is refused" do
    [ "日本語", "Проект", "!!!" ].each do |name|
      project = build(:project, name: name, slug: nil)

      assert_not project.valid?, name
      assert_equal [ "needs Latin letters or digits, since the Project's slug is made from them" ], project.errors[:name]
    end
  end

  test "a name that gives the slug new is refused, since that is the New Project page" do
    project = build(:project, name: "New", slug: nil)

    assert_not project.valid?
    assert_equal [ "gives the slug new, which is the New Project page's address" ], project.errors[:name]
  end

  test "a name whose slug another Project of the Organization has is refused" do
    shop = create(:project, name: "Shop", slug: nil)
    project = build(:project, organization: shop.organization, name: "SHOP", slug: nil)

    assert_not project.valid?
    assert_equal [ "gives the slug shop, which another of this Organization's Projects has" ], project.errors[:name]
    assert build(:project, name: "Shop", slug: nil).valid?, "another Organization's Project may have it"
  end

  test "a Project made before the slug checks can still be saved" do
    project = create(:project, name: "Shop", slug: nil)
    project.update_column(:slug, "")

    assert project.update(log_retention_days: 7)
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
