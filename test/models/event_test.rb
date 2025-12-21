# frozen_string_literal: true

require "test_helper"

class EventTest < ActiveSupport::TestCase
  test "compute_hash should generate consistent hash" do
    hash1 = Event.compute_hash(title: "Error", culprit: "main.rb", kind: Issue::KIND::ERROR)
    hash2 = Event.compute_hash(title: "Error", culprit: "main.rb", kind: Issue::KIND::ERROR)

    assert_equal hash1, hash2
  end

  test "compute_hash should generate different hash for different inputs" do
    hash1 = Event.compute_hash(title: "Error 1", culprit: "main.rb", kind: Issue::KIND::ERROR)
    hash2 = Event.compute_hash(title: "Error 2", culprit: "main.rb", kind: Issue::KIND::ERROR)

    assert_not_equal hash1, hash2
  end

  test "compute_hash should handle custom fingerprint" do
    # Fingerprint that ignores title
    fingerprint = [ "{{ default }}", "custom-part" ]

    hash1 = Event.compute_hash(title: "Error 1", culprit: "main.rb", kind: Issue::KIND::ERROR, fingerprint: fingerprint)
    hash2 = Event.compute_hash(title: "Error 1", culprit: "main.rb", kind: Issue::KIND::ERROR, fingerprint: fingerprint)

    assert_equal hash1, hash2
  end

  test "should delegate issue to event_fingerprint" do
    project = create(:project)
    issue = create(:issue, project: project)
    event_fingerprint = issue.event_fingerprints.create!(fingerprint: "test-hash", project: project)
    event = event_fingerprint.events.create!(environment: "production")
    event.create_event_payload!(payload: {})

    assert_equal issue, event.issue
  end
end
