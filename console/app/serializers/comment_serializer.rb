# frozen_string_literal: true

class CommentSerializer
  def initialize(comment)
    @comment = comment
  end

  def as_json(*)
    user = @comment.user

    {
      id: @comment.id,
      body: @comment.content.to_s,
      created_at: @comment.created_at.iso8601,
      user: {
        id: user.id,
        name: user.name,
        email: user.email,
      },
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
