# frozen_string_literal: true

require "test_helper"

class IssueTest < ActiveSupport::TestCase
  setup do
    @project = create(:project)
  end

  test "should assign sequential numbers scoped to project" do
    issue1 = create(:issue, project: @project)
    issue2 = create(:issue, project: @project)

    assert_equal 1, issue1.number
    assert_equal 2, issue2.number

    another_project = create(:project)
    issue1 = create(:issue, project: another_project)
    issue2 = create(:issue, project: another_project)

    assert_equal 1, issue1.number
    assert_equal 2, issue2.number
  end

  test "should have events through issue_fingerprints" do
    issue = create(:issue, project: @project)
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-hash", project: @project)
    event = issue_fingerprint.events.create!(environment: "production")
    event.create_event_payload!(payload: {})

    assert_includes issue.events, event
  end
end
