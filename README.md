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

```bash
make help    # list all commands
make test    # run every test
```

## Run it locally

Needs Docker with Compose v2. `make up` creates `.env` from `.env.example` the first time, starts the
services below and waits until each one reports healthy.

| Service | What it is for | Default address (ports set in `.env`) |
|---|---|---|
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
