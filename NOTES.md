# NOTES.md

## Teaching preferences
- User wants diagrams (Mermaid) with every node annotated with file paths and function names — the anti-lost tool is "diagram → code" links.
- Style they respond well to: a concrete plan first, then execution; anything done on their own ("do something myself") must come back to me for review.
- Everything lives in `teach/`, never the repo root.
- Cite code as `path/to/file.ts` + function name so the mental map snaps to the real tree.
- User is clearly technical and hands-on; could benefit from teaching others. Bias towards "explain it like you're teaching a teammate" framing.

## Session log
- Set up workspace.
- Lesson 2 (FSM) added: verified every branch against build.go/run.go/store.go directly, including the four failure classes (permanent / transient / infra-crash / busy-bounce) and the "repair paths" where retries are not retries (same-attempt busy bounce). User wants "every path" lessons — keep table-per-branch format.
- User wants this to serve two modes: *navigation* ("where is X handled?") and *explanation* ("how do I describe this to someone else").
