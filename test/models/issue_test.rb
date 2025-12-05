require "test_helper"

class IssueTest < ActiveSupport::TestCase
  setup do
    @project = create(:project)
  end

  test "compute_hash should generate consistent hash" do
    hash1 = Issue.compute_hash(title: "Error", culprit: "main.rb", event_type: "error")
    hash2 = Issue.compute_hash(title: "Error", culprit: "main.rb", event_type: "error")

    assert_equal hash1, hash2
  end

  test "compute_hash should generate different hash for different inputs" do
    hash1 = Issue.compute_hash(title: "Error 1", culprit: "main.rb", event_type: "error")
    hash2 = Issue.compute_hash(title: "Error 2", culprit: "main.rb", event_type: "error")

    assert_not_equal hash1, hash2
  end

  test "compute_hash should handle custom fingerprint" do
    # Fingerprint that ignores title
    fingerprint = ["{{ default }}", "custom-part"]

    hash1 = Issue.compute_hash(title: "Error 1", culprit: "main.rb", event_type: "error", fingerprint: fingerprint)
    hash2 = Issue.compute_hash(title: "Error 1", culprit: "main.rb", event_type: "error", fingerprint: fingerprint)

    assert_equal hash1, hash2
  end

  test "find_or_create_by_hash should create new issue if not exists" do
    hash = "unique_hash_1"
    attributes = { title: "New Issue", culprit: "test", event_type: "error", status: 0, level: 1 }

    assert_difference("Issue.count", 1) do
      issue = Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)
      assert_equal "New Issue", issue.title
      assert_equal @project, issue.project
    end
  end

  test "find_or_create_by_hash should return existing issue if exists" do
    hash = "unique_hash_2"
    attributes = { title: "Existing Issue", culprit: "test", event_type: "error", status: 0, level: 1 }

    # Create initial issue
    issue1 = Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)

    # Try to find again
    assert_no_difference("Issue.count") do
      issue2 = Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)
      assert_equal issue1.id, issue2.id
    end
  end

  test "find_or_create_by_hash should reopen resolved issue" do
    hash = "unique_hash_3"
    attributes = { title: "Resolved Issue", culprit: "test", event_type: "error", status: 0, level: 1 }

    issue = Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)
    issue.update!(status: 1) # Resolve it (assuming 1 is resolved)

    assert_equal 1, issue.reload.status

    # Trigger again
    reopened_issue = Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)

    assert_equal 0, reopened_issue.status # Should be open (0)
    assert_equal issue.id, reopened_issue.id
  end

  test "find_or_create_by_hash should scope to project" do
    other_project = create(:project)
    hash = "shared_hash"
    attributes = { title: "Scoped Issue", culprit: "test", event_type: "error", status: 0, level: 1 }

    # Create in project 1
    Issue.find_or_create_by_hash(project: @project, hash: hash, attributes: attributes)

    # Should create new issue in project 2 even with same hash
    assert_difference("Issue.count", 1) do
      issue = Issue.find_or_create_by_hash(project: other_project, hash: hash, attributes: attributes)
      assert_equal other_project, issue.project
    end
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
end
