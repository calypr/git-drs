# Mini cleanup contract

The baseline is commit `3388d6c8`, whose full suite passed 420 tests across 48 packages.

The wave changes no persisted format, wire request, CLI output contract, transfer policy, or public API.

- `pushRuntime` owns the Syfon multipart backend used by its uploads. Tests inject the backend through the runtime value, not a package global.
- `Gen3Options.Bucket` reaches `gen3Init` as an argument. The embedded API does not mutate Cobra flag globals.
- Command-local values contain only fields that later code reads.
- One-caller wrappers disappear when the caller can express the same operation directly.
- Existing behavior tests remain the pin. Private-helper tests move to the nearest observable result when required.

Workers own disjoint files. Only the root updates this contract, the todo, and the final accepted report.
