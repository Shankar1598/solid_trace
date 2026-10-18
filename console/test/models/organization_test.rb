# frozen_string_literal: true

require "test_helper"

class OrganizationTest < ActiveSupport::TestCase
  test "has projects" do
    organization = create(:organization)
    project = create(:project, organization: organization)

    assert_includes organization.projects, project
  end

  test "has users through organization_users" do
    organization = create(:organization)
    user = create(:user)

    organization.users << user

    assert_includes organization.users, user
  end

  test "validates name and slug" do
    org = Organization.new(name: "", slug: "")
    assert_not org.valid?
    assert_includes org.errors[:name], "can't be blank"
    assert_includes org.errors[:slug], "can't be blank"

    org.slug = "invalid slug"
    assert_not org.valid?
    assert_includes org.errors[:slug], "only allows lowercase letters, numbers, and hyphens"

    create(:organization, slug: "existing")
    org.slug = "existing"
    assert_not org.valid?
    assert_includes org.errors[:slug], "has already been taken"
  end

  test "made for a user, with them as its admin and a slug from its name" do
    user = create(:user)

    organization = Organization.make_for(user, name: "  Acme Corp ")

    assert organization.persisted?
    assert_equal "Acme Corp", organization.name
    assert_equal "acme-corp", organization.slug
    assert organization.organization_users.find_by!(user: user).admin?
  end

  # Slugs are shared by every Organization, and a user can't see the others,
  # so a taken one gets a number rather than an error.
  test "a taken slug gets a number" do
    create(:organization, slug: "acme")

    assert_equal "acme-2", Organization.make_for(create(:user), name: "Acme").slug
    assert_equal "acme-3", Organization.make_for(create(:user), name: "ACME").slug
  end

  # An Organization's pages are under /:org_slug, so a slug a top-level page
  # uses would be hidden behind it: /passwords/issues is a reset link.
  test "a slug a top-level page uses gets a number" do
    user = create(:user)

    assert_equal "passwords-2", Organization.make_for(user, name: "Passwords").slug
    assert_equal "login-2", Organization.make_for(user, name: "Login").slug
    assert_equal "organizations-2", Organization.make_for(user, name: "Organizations").slug
  end

  test "a name with no Latin letters or digits gets the slug organization" do
    assert_equal "organization", Organization.make_for(create(:user), name: "日本語").slug
  end

  test "an underscore in the name becomes a hyphen in the slug" do
    assert_equal "acme-ops", Organization.make_for(create(:user), name: "acme_ops").slug
  end

  test "a blank name isn't saved, and makes no membership" do
    assert_no_difference -> { OrganizationUser.count } do
      organization = Organization.make_for(create(:user), name: "  ")

      assert_not organization.persisted?
      assert_includes organization.errors[:name], "can't be blank"
    end
  end
end
