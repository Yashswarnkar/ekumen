---
id: C-http-api
aliases: [C5]
title: HTTP API for the UI
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: [T-graph-view, T-decision-views]
producers: [T-server]
---

# C-http-api · HTTP API for the UI

What `ekumen serve` offers the web UI. **It is the CLI over HTTP:** the same objects, the same rules, the same internal packages. Handlers hold no logic of their own (spine rule 11: the UI never has a capability the CLI lacks).

## 1. Conventions

- **Bind:** `127.0.0.1:7477` by default. Binding to any other address requires `--allow-remote` and is out of scope for v0.1.
- **Format:** JSON in and out, UTF-8. Objects are exactly the shapes in C-cli-json §4. Lists are wrapped as in the matching CLI command, e.g. `{"decisions": [...]}`.
- **Errors:** the C-cli-json §1 error object, with HTTP status `400` usage/validation · `403` lane or token · `404` not found · `409` conflict · `500` internal.
- **Actor:** the server acts as the lead, taken from `EKUMEN_ACTOR` or `--as` at start-up (it must be a `lead:` actor). Every write records that actor, exactly as the CLI would.
- **Write protection:** any web page you visit could try to send requests to `localhost`. So every write needs the header `X-Ekumen-Token: <token>` **and** an `Origin` matching the server's own. The token is generated at start-up, written to `.ekumen/.serve-token` (git-ignored, mode 0600) and embedded in the served UI.

## 2. Read endpoints

| Endpoint | Same as |
|---|---|
| `GET /api/health` | (exists) `{status, version}` |
| `GET /api/status` | `ekumen status` |
| `GET /api/inbox?for=lead` | `ekumen inbox --for lead` |
| `GET /api/tasks?status=` · `GET /api/tasks/{id}` | `ekumen task list` · `task show` (including brief text and entries) |
| `GET /api/decisions?status=` · `GET /api/decisions/{id}` | `ekumen decision list` · `decision show` |
| `GET /api/questions?for=&open=` | `ekumen question list` |
| `GET /api/graph` | `ekumen graph` |
| `GET /api/check` | `ekumen check` |

Ids in paths may be aliases. Responses use the id.

## 3. Write endpoints (v0.1: what a lead does from the decision screen)

| Endpoint | Body | Same as |
|---|---|---|
| `POST /api/decisions/{id}/answer` | `{answer, note?}` | `ekumen decision answer` |
| `POST /api/questions` | `{subject, text, to?}` | `ekumen question ask` |
| `POST /api/questions/{id}/answer` | `{text}` (questions addressed to the lead) | `ekumen question answer` |
| `POST /api/routes/{id}/resolve` | `{note}` | `ekumen route resolve` |

Writes return the C-cli-json write result `{ok, events, paths}`. Approving tasks, freezing contracts and superseding decisions stay CLI-only in v0.1. They are rarer, and worth the deliberate step.

## 4. Live updates

`GET /api/stream` is Server-Sent Events:

```
id: 01JD8Z3K6Q4M2V7X9T1B5N0RCE
event: changed
data: {"subjects": [{"kind": "decision", "id": "D-control-loop"}], "cursor": "01JD8Z3K6Q4M2V7X9T1B5N0RCE"}
```

- An event is sent whenever new event files appear, and also when observed state changes (PR or session status), with `subjects` naming what changed. The UI then refetches those objects. No object data travels on the stream.
- The **cursor** is the newest event id. A client reconnecting with `Last-Event-ID` receives one `changed` event covering everything since then (R2 §4: replay from a cursor).
- The server notices new files by polling `.ekumen/events/` (standard library only). The default interval is 1 second.
- `event: ping` every 25 seconds keeps the connection alive.

## 5. Versioning

Same rule as C-cli-json §5: adding endpoints, parameters or fields is minor, and clients ignore what they don't know. Anything else needs a tier-2 decision. No `/v1/` in paths for now.

## Open before freeze

- **The token file.** It is git-ignored, but it sits inside `.ekumen/`. Proposal: keep it there; the alternative is the OS temp dir, which is harder to find for scripts.
- **Lead-only server.** In v0.1 sessions don't use HTTP; they use the CLI. Proposal: keep it that way, so lane checks stay in one place.
