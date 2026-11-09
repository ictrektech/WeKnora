# API Reference: Automation and Notifications

Automations are executed by the built-in WeKnora assistant as the creator. The following endpoints require a logged-in user; Space Viewer+ can use them and can only access their own tasks and notifications; API Keys are not granted access to these routes.

## Task Management {#tasks}

| Method and Path | Purpose |
| --- | --- |
| `GET /api/v1/automations` | List your own tasks |
| `POST /api/v1/automations` | Create a task |
| `POST /api/v1/automations/validate-trigger` | Validate the schedule and preview the next execution time |
| `GET /api/v1/automations/by-session/:session_id` | Query the task associated with a session |
| `GET /api/v1/automations/:id` | Task details |
| `PUT /api/v1/automations/:id` | Update a task, using the fields present in `AutomationPatch` |
| `DELETE /api/v1/automations/:id` | Delete a task |
| `POST /api/v1/automations/:id/pause` | Pause the schedule |
| `POST /api/v1/automations/:id/resume` | Resume the schedule |
| `POST /api/v1/automations/:id/run` | Run immediately |

The required fields for a create request are `name`, `instruction`, and `trigger`. Example:

```json
{
  "name": "Daily Knowledge Summary",
  "instruction": "Read the knowledge bases I have access to, summarize the new content and provide sources, without modifying any materials.",
  "trigger": {"type": "cron", "expr": "0 9 * * *", "timezone": "Asia/Shanghai"},
  "session_mode": "per_run",
  "access_level": "read_only",
  "notify_on_success": true
}
```

| Field | Description |
| --- | --- |
| `trigger.type` | `cron` or `once` |
| `trigger.expr` | Five-field or six-field Cron, also accepts descriptors such as `@daily`; minimum interval 5 minutes |
| `trigger.at` | One-time time, RFC3339 or local time in the specified timezone; nonexistent local times are rejected |
| `trigger.timezone` | IANA timezone, defaults to UTC if omitted |
| `model_id` | Optional model; an empty value uses the assistant's default model |
| `session_mode` | `per_run` (default) or `persistent` |
| `carry_last_result` | Carry the last successful result into each new session, default false |
| `misfire_policy` | `skip` (default) or `run_once` |
| `overlap_policy` | `skip` (default) or `queue` |
| `access_level` | `read_only` / `confirm` (default) / `auto_write`, subject to the assistant's permission ceiling |
| `approval_timeout_minutes` | Default 1440, range 5–10080 |
| `limits.max_duration_minutes` | Default 30, maximum 240; approval waiting time is not counted |
| `notify_on_success` | Default false; failures are always notified |

Task instructions support the template variables `now`, `last_run_at`, `last_success_at`, `run_id`, and `automation_name`, written in double curly brace form. Automations are executed by the built-in assistant; collaboration requirements are expressed through the task instructions.

## Run Records and Authorization {#runs}

| Method and Path | Purpose |
| --- | --- |
| `GET /api/v1/automations/:id/runs` | Run records |
| `GET /api/v1/automation-runs/:id` | Details of a single run |
| `POST /api/v1/automation-runs/:id/cancel` | Cancel that run |
| `GET /api/v1/automations/:id/grants` | Long-term grants for the task |
| `GET /api/v1/automations/:id/grant-candidates` | List grantable candidates from existing approvals |
| `POST /api/v1/automations/:id/grants` | Add a grant with `{"prompt_id":"..."}` |

Run statuses include `queued`, `running`, `awaiting_approval`, `succeeded`, `failed`, `cancelled`, `skipped`, and `needs_attention`. The latter indicates that execution was interrupted and the result is uncertain; write operations that may already have taken effect should not be retried automatically.

The unified grant query and revocation endpoints are `GET /api/v1/agent/grants` and `DELETE /api/v1/agent/grants/:id`; approval responses use `POST /api/v1/agent/prompts/:prompt_id`. For request structures and validation, see `internal/handler/mcp_service.go` and `agent_grant.go`.

## In-App Notifications {#notifications}

| Method and Path | Purpose |
| --- | --- |
| `GET /api/v1/notifications` | Query your own notifications |
| `POST /api/v1/notifications/read` | Mark as read, request `{"ids":["..."]}` |
| `DELETE /api/v1/notifications/read` | Clear read notifications |

Implementation references: `internal/router/routes_automation.go`, `internal/handler/automation.go`, `internal/types/automation.go`. For usage, see [Automation](../03-features/27-automations.md).
