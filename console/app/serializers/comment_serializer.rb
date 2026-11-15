# frozen_string_literal: true

class CommentSerializer
  def initialize(comment)
    @comment = comment
  end

  # user is the member who wrote it; nil for a Factory comment, whose run
  # is its Run's id and Run page (nil for a member's).
  def as_json(*)
    user = @comment.user

    {
      id: @comment.id,
      body: @comment.content.to_s,
      created_at: @comment.created_at.iso8601,
      user: user && { id: user.id, name: user.name, email: user.email },
      run: @comment.factory? ? { id: @comment.run_id, url: @comment.run_path } : nil,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
