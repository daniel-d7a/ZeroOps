# Mission: Understand and explain the ZeroOps codebase

## Why
I own this codebase but keep getting lost in it when I come back after a break. I want a mental map durable enough that I can navigate straight to the right file/function, and confidently explain how the platform works to someone else (interviewer, teammate, or future me).

## Success looks like
- I can draw the ZeroOps architecture from memory: what each container service does, and where the request flows go (console → API, upload → worker, proxy → student app).
- Given "where is X handled?" (e.g. env var encryption, health checks, deploy status transitions), I can name the file and function without grepping.
- I can explain the boundaries and invariants (state ownership, image_ref, Traefik file provider vs Docker provider) and say *why* they exist.
- I can do it cold: explain the full deploy lifecycle end-to-end in ~5 minutes without notes, then check myself against my reference docs.

## Constraints
- Learning happens inside this repo — everything must cite real files/functions, no hand-waving.
- Time-boxed sessions; prefer dense reference documents + short lessons over长 prose.
- Best retention format: re-derive from memory first, then check against the reference.

## Out of scope
- Rewriting/refactoring the codebase.
- Backend concepts beyond what ZeroOps uses (e.g. general RabbitMQ internals), unless they unlock the mental map.
