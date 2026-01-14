# Logging Guidelines

## Goals
- Provide actionable information for users.
- Avoid logging sensitive values or high-volume details by default.
- Keep logs stable and minimal in production usage.

## Trace logging
- Use the `GIT_TRANSFER_TRACE` environment variable to enable verbose logging.
- The global flag is initialized on startup from `GIT_TRANSFER_TRACE` and defaults to `0`.
- Treat trace logs as opt-in diagnostics: only emit detailed or noisy messages when `GIT_TRANSFER_TRACE=1`.

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
