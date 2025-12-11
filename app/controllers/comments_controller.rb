# frozen_string_literal: true

class CommentsController < ApplicationController
  before_action :set_issue

  def create
    @comment = @issue.comments.new(comment_params)
    @comment.user = Current.user

    if @comment.save
      redirect_to issue_path(@issue.number, org_slug: @current_org.slug, anchor: "comments"), notice: "Comment added"
    else
      redirect_to issue_path(@issue.number, org_slug: @current_org.slug, anchor: "comments"), alert: "Error creating comment"
    end
  end

  def destroy
    @comment = @issue.comments.find(params[:id])

    if @comment.user == Current.user
      @comment.destroy
      redirect_to issue_path(@issue.number, org_slug: @current_org.slug, anchor: "comments"), notice: "Comment deleted"
    else
      redirect_to issue_path(@issue.number, org_slug: @current_org.slug, anchor: "comments"), alert: "You can only delete your own comments"
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
