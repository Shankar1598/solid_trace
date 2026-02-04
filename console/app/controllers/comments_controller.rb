# frozen_string_literal: true

class CommentsController < ApplicationController
  before_action :set_issue

  def create
    @comment = @issue.comments.new(comment_params)
    @comment.organization_user = @current_org_user

    if @comment.save
      redirect_to project_issue_path(@issue.project, @issue, org_slug: @current_org.slug, anchor: "comments"), notice: "Comment added"
    else
      redirect_to project_issue_path(@issue.project, @issue, org_slug: @current_org.slug, anchor: "comments"), alert: "Error creating comment"
    end
  end

  def destroy
    @comment = @issue.comments.find(params[:id])

    if @comment.organization_user_id == @current_org_user.id
      @comment.destroy
      redirect_to project_issue_path(@issue.project, @issue, org_slug: @current_org.slug, anchor: "comments"), notice: "Comment deleted"
    else
      redirect_to project_issue_path(@issue.project, @issue, org_slug: @current_org.slug, anchor: "comments"), alert: "You can only delete your own comments"
    end
  end

  private

  def comment_params
    params.require(:comment).permit(:content)
  end

  def set_issue
    @current_org = Current.user.organizations.find_by!(slug: params[:org_slug])
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:issue_number])
    @current_org_user = @current_org.organization_users.find_by!(user: Current.user)
  end
end
