# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"

class IssueAssignmentNotificationJobTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper

  setup do
    @organization = create(:organization)
    @project = create(:project, organization: @organization)
  end

  test "enqueues notification when assignee changes" do
    issue = create(:issue, project: @project, assignee: nil)
    assignee = create(:organization_user, organization: @organization)

    assert_enqueued_with(job: IssueAssignmentNotificationJob, args: [ issue, nil, assignee.id ]) do
      issue.update!(assignee: assignee)
    end
  end
end

