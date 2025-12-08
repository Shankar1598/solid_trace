class Comment < ApplicationRecord
  belongs_to :issue
  belongs_to :user
  has_rich_text :content
  validates :content, presence: true
end
