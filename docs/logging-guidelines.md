# Logging Guidelines

## Goals
- Provide actionable information for users.
- Avoid logging sensitive values or high-volume details by default.
- Keep logs stable and minimal in production usage.

## Trace logging (design proposal)
- We intend to use the `GIT_TRANSFER_TRACE` environment variable to enable verbose logging when trace support is implemented.
- When trace logging is available, treat trace logs as opt-in diagnostics and only emit detailed or noisy messages when explicitly enabled.
## What belongs in default logs
- Errors that block normal operation.
- User-facing warnings that require action.
- High-level lifecycle messages that are essential for support.

## What belongs in trace logs only
- Per-file or per-object identifiers (OIDs, DRS IDs).
- Raw command invocations or argument dumps.
- Repetitive status messages in loops.
- Internal bookkeeping or intermediate state.

## Sensitive data handling
- Do not log credentials, tokens, or full URLs that include secrets.
- Avoid logging full file paths unless needed for actionable errors.

## Changes to existing logs
- When adding new logs, default to minimal output and gate detailed output behind trace.
- When reviewing existing logs, move verbose entries behind trace and remove entries that expose sensitive data without clear value.
