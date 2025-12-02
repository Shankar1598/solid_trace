class CreateProjectKeys < ActiveRecord::Migration[8.1]
  def change
    create_table :project_keys do |t|
      t.references :project, null: false, foreign_key: true
      t.string :public_key
      t.string :secret_key

      t.timestamps
    end
    add_index :project_keys, :public_key
  end
end
