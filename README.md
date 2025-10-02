# Addis Hiwot

"Your Recovery Ally": a mobile app for people recovering from addiction or working on their mental
health. A daily check-in, goals and habits, a recovery counter, SOS tools for cravings, a resource
library, AddisBot (an AI companion grounded in reviewed content) and moderated community rooms. English
and Amharic.

**Status:** early development, built one small step a day. No app code yet.

**Not medical care.** If you are in danger, contact local emergency services or a trusted person.

## Stack (planned)

Flutter app (Android first) · Go backend · PostgreSQL with pgvector · Docker Compose for local development.

## Layout

| Folder | What goes there |
|---|---|
| `backend/` | Go service (API, worker, admin website), from the first weeks |
| `mobile/` | Flutter app, from the first weeks |

## First steps after cloning

Needs Docker with Compose v2, Go (any recent version: Go downloads the 1.27.3 toolchain that `backend/go.mod`
asks for) and make.

```bash
make help    # list all commands
make test    # Go tests with the race detector
make lint    # go vet + golangci-lint (pinned Docker image) + compose file check
make up      # start the API and its services
```

## Run it locally

`make up` creates `.env` from `.env.example` the first time, builds the API image, starts the services below
and waits until each one reports healthy. If `.env.example` later gains a setting your `.env` lacks, `make up`
stops and lists it.

| Service | What it is for | Default address (ports set in `.env`) |
|---|---|---|
| API (Go) | The backend: `/healthz` (process alive), `/readyz` (database reachable) | `http://127.0.0.1:8080` |
| PostgreSQL 18 + pgvector | App data, job queue, AI search vectors | `127.0.0.1:5433` · `make psql` opens a shell |
| Mailpit | Catches every email sent in development | inbox at `http://127.0.0.1:8025`, SMTP on `1025` |
| MinIO (community build) | S3-compatible storage for audio, video and images | API `127.0.0.1:9000`, console `:9001` |

```bash
make up       # start and wait for healthy
make ps       # status
make logs     # follow logs
make down     # stop, keep data
make down-v   # stop and delete all local data
```

## Database

The schema lives in numbered SQL migrations in `backend/migrations/` (golang-migrate format, never edited once
committed). `make up` applies pending migrations before the API starts. Queries live in `backend/queries/`;
sqlc turns them into typed Go in `backend/internal/db/`, and `make lint` fails if that code is out of date.

```bash
make migrate-version   # current migration
make migrate-down      # roll back the last migration
make migrate-up        # apply pending migrations
make generate          # regenerate internal/db after changing queries/ or migrations/
```

`make test` starts PostgreSQL and gives every database test its own fresh copy of a migrated template
database, dropped when the test ends.

## API conventions

The contract is in [`backend/api/openapi.yaml`](backend/api/openapi.yaml) (OpenAPI 3.1, checked by `make lint`).

- Request bodies: `application/json`, one object, at most 64 KB, no unknown fields.
- Every error has one shape, and clients act on `code`:
  `{"error": {"code": "VALIDATION_FAILED", "message": "...", "request_id": "...", "fields": [...]}}`.
- Every response carries `X-Request-ID`; the same ID is in the error body and in the server log.
- Lists: `?limit=` (1–100, default 20) and `?cursor=` from the previous `next_cursor` (`null` on the last page).

## Commit messages

Every commit follows the Angular format:

```
<type>(<scope>): <subject>

<body: why this change, lines wrapped at 72 characters>
```

- Header at most 72 characters; subject in the imperative ("add", not "added"), lowercase start, no period.
- A body is required. A `BREAKING CHANGE: ...` footer is added when a change breaks API clients.
- **type:** `feat` · `fix` · `refactor` · `perf` · `test` · `docs` · `build` · `ci` · `style` · `chore`
- **scope:** `infra` · `api` · `db` · `auth` · `profile` · `checkin` · `mood` · `goals` · `habits` · `badges` ·
  `recovery` · `sos` · `garden` · `journal` · `programs` · `library` · `media` · `ai` · `community` · `buddy` ·
  `notify` · `admin` · `directory` · `telegram` · `l10n` · `design` · `mobile` · `security` · `perf` ·
  `release` · `deps`
