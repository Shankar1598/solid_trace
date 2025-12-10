# frozen_string_literal: true

class CommentsController < ApplicationController
  before_action :set_issue

  def create
    @comment = @issue.comments.new(comment_params)
    @comment.user = Current.user

    if @comment.save
      respond_to do |format|
        format.turbo_stream
      end
    else
      respond_to do |format|
        format.turbo_stream do
          render turbo_stream: turbo_stream.update("comment-flash", partial: "shared/error_alert", locals: { message: "Error creating comment." })
        end
      end
    end
  end

  def destroy
    @comment = @issue.comments.find(params[:id])

    if @comment.user == Current.user
      @comment.destroy
      respond_to do |format|
        format.turbo_stream
      end
    else
      respond_to do |format|
        format.turbo_stream do
          render turbo_stream: turbo_stream.update("comment-flash", partial: "shared/error_alert", locals: { message: "You can only delete your own comments." })
        end
      end
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
