# frozen_string_literal: true

class IssueMailer < ApplicationMailer
  def notify(issue, recipients)
    @issue = issue
    @project = issue.project

    mail(
      to: recipients,
      subject: "[Garnet] New Issue in #{@project.name}: #{issue.title}"
    )
  end
end
