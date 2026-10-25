# 0001 — Baseline

**Date:** 2026-10-19
**Mission link:** understand & explain ZeroOps architecture.

## What I know now
- User wrote/bought the codebase (NestJS + Go worker + Vue + big docker-compose), but gets lost "every now and then" — the comments in code aren't enough.
- User asked for docs built from the `teach` skill: diagrams, reference files, function references.
- User is hands-on and technical; responded well to plan-first, then check ("tell me what are you going to do?" → "ok start that").

## Zone of proximal development
Start from the 30,000-ft map (containers, message flow, routing), then drill into lifecycle (who writes which status). Do NOT start inside individual functions — that's where they get lost today.

## Non-obvious lessons / insights
- The anti-lost tool the user actually needs is **diagram→code annotation**: every box on a diagram should name the exact file/function that implements it.
- User wants this to serve two modes: *navigation* ("where is X handled?") and *explanation* ("how do I describe this to someone else").
