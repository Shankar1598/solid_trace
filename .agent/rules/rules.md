---
trigger: always_on
---

- Right now we are not yet live with this project. So dont worry about data migrations and backword compatibility. We can always drop the db and rerun migrations. We can also mutate the existing migrations. Just remember to delete the schema.rb file and run `./bin/rails db:reset db:migrate db:seed` command.
- Skip creating walkthroughs after completing all the tasks.
- Do not open the browser and verify things unless I explicitly ask you to do so.
- Email and password for logging into the app are "admin@solidtrace.local" and "password123"
