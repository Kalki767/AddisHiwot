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
| `backend/` | Go service (API, worker, admin website) |
| `mobile/` | Flutter app |
| `design/` | Figma exports, screen inventory and design gaps ([SCREENS.md](design/SCREENS.md), [GAPS.md](design/GAPS.md)) |
| `content/` | Articles, audio, video and programs with licence info |
| `docs/` | Architecture, product, safety and privacy docs |
| `bench/results/` | Raw output of every measurement |
| `scripts/` | Developer scripts |
| `site/` | Project website |

## First steps after cloning

```bash
make hooks   # turn on the commit-message checker
make test    # run every test
make help    # list all commands
```

## Commit messages

Every commit follows the Angular format, and the `commit-msg` hook (`make hooks`) rejects anything else:

```
<type>(<scope>): <subject>

<body: why this change, lines wrapped at 72 characters>

Refs: day-NNN
```

- Header at most 72 characters; subject in the imperative ("add", not "added"), lowercase start, no period.
- A body is required. The `Refs: day-NNN` footer and a `BREAKING CHANGE: ...` line are optional.
- **type:** `feat` · `fix` · `refactor` · `perf` · `test` · `docs` · `build` · `ci` · `style` · `chore`
- **scope:** `infra` · `api` · `db` · `auth` · `profile` · `checkin` · `mood` · `goals` · `habits` · `badges` ·
  `recovery` · `sos` · `garden` · `journal` · `programs` · `library` · `media` · `ai` · `community` · `buddy` ·
  `notify` · `admin` · `directory` · `telegram` · `l10n` · `design` · `mobile` · `security` · `perf` ·
  `release` · `deps`
