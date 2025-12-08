class CommentsController < ApplicationController
  before_action :set_issue

  def create
    @comment = @issue.comments.new(comment_params)
    @comment.user = Current.user

    if @comment.save
      redirect_to issue_path(@issue, org_slug: params[:org_slug]), notice: "Comment was successfully created."
    else
      redirect_to issue_path(@issue, org_slug: params[:org_slug]), alert: "Error creating comment."
    end
  end

  private

  def set_issue
    @current_org = Current.user.organizations.find_by!(slug: params[:org_slug])
    @issue = Issue.joins(project: :organization)
                  .where(organizations: { id: @current_org.id })
                  .find_by!(number: params[:issue_id])
  end

  def comment_params
    params.require(:comment).permit(:content)
  end
end
