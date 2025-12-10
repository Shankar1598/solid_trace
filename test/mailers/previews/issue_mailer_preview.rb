# Preview all emails at http://localhost:3000/rails/mailers/issue_mailer
class IssueMailerPreview < ActionMailer::Preview
  # Preview this email at http://localhost:3000/rails/mailers/issue_mailer/notify
  def notify
    IssueMailer.notify
  end
end
