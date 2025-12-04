class CreateEnvironments < ActiveRecord::Migration[8.1]
  def change
    create_table :environments do |t|
      t.string :name
      t.references :project, null: false, foreign_key: true

      t.timestamps
    end
    add_index :environments, [:project_id, :name], unique: true
  end
end
