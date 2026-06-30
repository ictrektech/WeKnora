# WeKnora Local Agent Instructions

This submodule is the primary workspace for ictrek-specific WeKnora changes.

- Keep ictrek-only operational notes under `docs/ictrek/` instead of editing upstream-facing docs unless the change is meant to be contributed upstream.
- Use `ssh tc232` as the deployment/test SSH target when the user asks to test WeKnora deployment on the prepared remote machine.
- Treat `tc232` as a local SSH config alias only. Do not document it as a reachable hostname or API base URL.
- When documenting remote services, distinguish between the remote listen address (for example `127.0.0.1:18118` on the SSH target) and any external mapping, tunnel, reverse proxy, or public endpoint that the operator creates separately.
- Do not run `git` commands on remote deployment/test hosts for local code validation. Copy files or use another transport channel first, then run runtime commands remotely.
- Keep build sync directories and deployment directories separate. Directories such as `/data/jhu/build/weknora` are for `build_image.sh` only; do not run `docker compose pull`, `docker compose up`, or runtime restart commands there unless that directory is confirmed to be the actual compose project.
- Before restarting an existing remote deployment, identify the real compose project from the running container labels, for example `docker inspect <app-container> --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}} {{index .Config.Labels "com.docker.compose.project.config_files"}}'`, then run compose from that working directory with the same compose file set.
- Never let a production-like deployment fall back to upstream default images such as `wechatopenai/weknora-app:latest`. The deployment compose or image override must point to `swr.cn-southwest-2.myhuaweicloud.com/ictrek/...` images before running `pull` or `up`.
