class CreateProjects < ActiveRecord::Migration[8.1]
  def change
    create_table :projects do |t|
      t.string :name
      t.string :slug
      t.references :organization, null: false, foreign_key: true

      t.timestamps
    end
    add_index :projects, :slug
  end
end
