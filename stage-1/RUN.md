# Running Tablekeeper — Stage 1

A single containerized HTTP service with zero runtime dependencies (only
Node's built-ins: `http`, `crypto`, `Intl`). No network access is required at
build time or at run time.

## Build and start

```sh
docker build -t tablekeeper-stage1 stage-1
docker run --rm -p 8080:8080 -e PORT=8080 tablekeeper-stage1
```

The service listens on `0.0.0.0:$PORT` (default `8080`) and is ready as soon
as the container starts logging; `GET /health` returns `200 {"status":"ok"}`
once the in-memory store is initialized, well within 60 seconds.

## Try it

```sh
curl http://localhost:8080/health

curl -X POST http://localhost:8080/_test/reset -H 'Content-Type: application/json' -d '{
  "users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
  "restaurants": [{
    "id": "r_anker", "name": "Zum Anker", "timezone": "Europe/Berlin",
    "slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
    "opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
    "tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}]
  }],
  "reservations": []
}'

curl http://localhost:8080/restaurants
```

No other setup, seed step or environment variable is required.
