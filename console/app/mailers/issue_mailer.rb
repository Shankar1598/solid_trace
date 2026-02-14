# frozen_string_literal: true

class IssueMailer < ApplicationMailer
  def notify(issue, recipients)
    @issue = issue
    @project = issue.project

    mail(
      to: recipients,
      subject: "[SolidTrace] New Issue in #{@project.name}: #{issue.title}"
    )
  end

  def batch_notify(issues, recipients)
    @issues = issues
    @project = issues.first.project
    @organization = @project.organization
    @count = issues.size

    mail(
      to: recipients,
      subject: "[SolidTrace] #{@count} New Issues Detected"
    )
  end

  def assignment_updated(issue, recipients, previous_assignee_name:, new_assignee_name:)
    @issue = issue
    @project = issue.project
    @previous_assignee_name = previous_assignee_name.presence || "Unassigned"
    @new_assignee_name = new_assignee_name.presence || "Unassigned"

    mail(
      to: recipients,
      subject: "[SolidTrace] Issue assignment updated in #{@project.name}: #{issue.title}"
    )
  end
end
