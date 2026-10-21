# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# An admin's save of a Project's Log transform: a program EventStore's check
# compiles becomes the newest version; one with errors saves nothing and
# answers them; the current program again changes nothing; an old version's
# program saved again is a restore. Anyone else's save is refused before it
# changes anything.
class LogTransformSaveTest < ActiveSupport::TestCase
  ERROR = {
    "message" => "call to undefined function", "code" => 105, "start" => 0, "end" => 12,
    "text" => "error[E105]: call to undefined function",
  }.freeze

  setup do
    @organization = create(:organization)
    @project = create(:project, organization: @organization)
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @check_url = "#{EventStore.base_url}/api/#{@project.id}/logs/transform/check"
  end

  test "an admin's program that compiles becomes the Project's newest version, saved by them" do
    check = stub_request(:post, @check_url).with(body: { program: ".a = 1" }.to_json)
      .to_return(status: 200, body: { errors: [], warnings: [] }.to_json)

    save = LogTransformSave.call(project: @project, user: @admin, program: ".a = 1")

    assert save.done?
    assert_equal :saved, save.outcome
    assert_equal save.version, @project.log_transform
    assert_equal [ ".a = 1", @admin ], [ save.version.program, save.version.user ]
    assert_requested check
  end

  test "a program that doesn't compile saves nothing and answers the check's errors" do
    stub_request(:post, @check_url).to_return(status: 200, body: { errors: [ ERROR ], warnings: [] }.to_json)

    save = LogTransformSave.call(project: @project, user: @admin, program: "http_request(\"x\")")

    assert_not save.done?
    assert_nil save.outcome
    assert_equal [ ERROR ], save.errors
    assert_nil @project.log_transform
  end

  test "the current program saved again changes nothing" do
    current = @project.log_transform_versions.create!(program: ".a = 1", user: @member)

    save = LogTransformSave.call(project: @project, user: @admin, program: ".a = 1")

    assert save.done?
    assert_equal :unchanged, save.outcome
    assert_equal current, save.version
    assert_equal 1, @project.log_transform_versions.count
  end

  test "an old version's program saved again is restored as the newest version" do
    stub_request(:post, @check_url).to_return(status: 200, body: { errors: [], warnings: [] }.to_json)
    old = @project.log_transform_versions.create!(program: ".a = 1", user: @member)
    @project.log_transform_versions.create!(program: ".a = 2", user: @member)

    save = LogTransformSave.call(project: @project, user: @admin, program: old.program)

    assert_equal :saved, save.outcome
    assert_equal [ ".a = 1", ".a = 2", ".a = 1" ], @project.log_transform_versions.order(:id).pluck(:program)
    assert_equal [ ".a = 1", @admin ], [ @project.log_transform.program, @project.log_transform.user ]
  end

  test "an empty program turns the Log transform off without a check" do
    @project.log_transform_versions.create!(program: ".a = 1", user: @admin)

    save = LogTransformSave.call(project: @project, user: @admin, program: "")

    assert_equal :saved, save.outcome
    assert_equal "", @project.log_transform.program
  end

  test "a member's save is refused before anything is checked or saved" do
    assert_raises(Pundit::NotAuthorizedError) do
      LogTransformSave.call(project: @project, user: @member, program: ".a = 1")
    end

    assert_nil @project.log_transform
  end
end
