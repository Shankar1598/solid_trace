# Running Garnet with Docker

To start the application, use the following commands:

```bash
# Start the services (Database, Redis, etc.)
docker compose up -d

# Start the Rails application with Tailwind watcher
docker compose run --rm --service-ports backend bin/dev
```

Note: Ensure the application binds to `0.0.0.0` in Docker.
