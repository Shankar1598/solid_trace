require "test_helper"

class ProjectIssueCounterTest < ActiveSupport::TestCase
  setup do
    @project = create(:project)
  end

  test "next_value_for returns 1 for new project" do
    value = ProjectIssueCounter.next_value_for(@project)
    assert_equal 1, value
  end

  test "next_value_for returns sequential values" do
    first = ProjectIssueCounter.next_value_for(@project)
    second = ProjectIssueCounter.next_value_for(@project)
    third = ProjectIssueCounter.next_value_for(@project)

    assert_equal 1, first
    assert_equal 2, second
    assert_equal 3, third
  end

  test "different projects have separate counters" do
    project_two = create(:project)

    value_one = ProjectIssueCounter.next_value_for(@project)
    value_two = ProjectIssueCounter.next_value_for(project_two)
    value_one_again = ProjectIssueCounter.next_value_for(@project)

    assert_equal 1, value_one
    assert_equal 1, value_two
    assert_equal 2, value_one_again
  end

  test "counter persists value in database" do
    ProjectIssueCounter.next_value_for(@project)
    ProjectIssueCounter.next_value_for(@project)

    counter = ProjectIssueCounter.find_by(project_id: @project.id)
    assert_equal 2, counter.value
  end
end
