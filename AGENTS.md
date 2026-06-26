# WeKnora Local Agent Instructions

This submodule is the primary workspace for ictrek-specific WeKnora changes.

- Keep ictrek-only operational notes under `docs/ictrek/` instead of editing upstream-facing docs unless the change is meant to be contributed upstream.
- Use `ssh tc232` as the deployment/test SSH target when the user asks to test WeKnora deployment on the prepared remote machine.
- Treat `tc232` as a local SSH config alias only. Do not document it as a reachable hostname or API base URL.
- When documenting remote services, distinguish between the remote listen address (for example `127.0.0.1:18118` on the SSH target) and any external mapping, tunnel, reverse proxy, or public endpoint that the operator creates separately.
- Do not run `git` commands on remote deployment/test hosts for local code validation. Copy files or use another transport channel first, then run runtime commands remotely.
