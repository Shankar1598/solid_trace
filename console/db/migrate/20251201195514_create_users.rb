# frozen_string_literal: true

class CreateUsers < ActiveRecord::Migration[8.1]
  def change
    create_table :users do |t|
      t.string :email
      t.string :password_digest
      t.string :name

      t.timestamps
    end
    add_index :users, :email, unique: true
    add_index :users, :name
  end
end
