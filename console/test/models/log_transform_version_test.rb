# frozen_string_literal: true

require "test_helper"

# A Project's Log transform versions: the newest is current, each keeps who
# saved it until that user is gone, and they go with their Project.
class LogTransformVersionTest < ActiveSupport::TestCase
  setup do
    @project = create(:project)
    @ada = create(:user, name: "Ada")
  end

  test "a Project has no Log transform until a version is saved, then the newest is current" do
    assert_nil @project.log_transform

    @project.log_transform_versions.create!(program: ".a = 1", user: @ada)
    newest = @project.log_transform_versions.create!(program: ".a = 2", user: @ada)
    create(:project).log_transform_versions.create!(program: ".other = 1")

    assert_equal newest, @project.reload.log_transform
  end

  test "a version keeps who saved it until that user is gone" do
    version = @project.log_transform_versions.create!(program: ".a = 1", user: @ada)

    @ada.destroy!

    assert_nil version.reload.user
    assert_equal ".a = 1", version.program
  end

  test "deleting a Project deletes its versions" do
    @project.log_transform_versions.create!(program: ".a = 1", user: @ada)
    kept = create(:project).log_transform_versions.create!(program: ".b = 1")

    @project.destroy!

    assert_equal [ kept ], LogTransformVersion.all.to_a
  end

  test "a program is at most 64 KB" do
    assert @project.log_transform_versions.build(program: "#" * 64.kilobytes).valid?

    version = @project.log_transform_versions.build(program: "é" * (32.kilobytes + 1))
    assert_not version.valid?
    assert_equal [ "is over 64 KB" ], version.errors[:program]
  end
end
