# frozen_string_literal: true

class CommentSerializer
  def initialize(comment)
    @comment = comment
  end

  def as_json(*)
    {
      id: @comment.id,
      body: @comment.body.to_s,
      created_at: @comment.created_at.iso8601,
      user: {
        id: @comment.user.id,
        name: @comment.user.name,
        email: @comment.user.email
      }
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
