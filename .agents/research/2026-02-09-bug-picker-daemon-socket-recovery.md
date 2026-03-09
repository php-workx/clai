# Bug Report: Picker Fails When Daemon Socket Is Missing

**Date:** 2026-02-09  
**Severity:** medium  
**Status:** root-cause-found, fixed

## Symptom
History picker opens, then shows:
`Error: history provider: rpc: rpc error: code = Unavailable ... dial unix ~/.clai/clai.sock: connect: no such file or directory`

## Expected Behavior
Picker should recover if the daemon socket is transiently missing (daemon restart/race) and continue loading history instead of surfacing transport failure immediately.

## Root Cause
- `internal/picker/history_provider.go` performed a single RPC attempt and returned transport errors directly.
- There was no daemon recovery path (`EnsureDaemon`) and no bounded retry window for startup races.

## Why It Happens
`grpc.NewClient` is lazy; connection failure occurs on first RPC. If socket is absent at that moment, fetch fails with `codes.Unavailable`, and picker enters `stateError`.

## Fix
1. Added daemon recovery path in `HistoryProvider.Fetch` for recoverable transport failures on the default IPC socket.
2. Recovery now calls `ipc.EnsureDaemon()` and retries fetch in a bounded window (`recoveryTimeout=600ms`, retry delay `30ms`).
3. Added regression test proving recovery when socket appears after initial failure.

## Validation
- `go test ./internal/picker ./cmd/clai-picker`
- New regression test: `TestHistoryProvider_RecoversWhenDefaultSocketAppears`

## Files Changed
- `internal/picker/history_provider.go`
- `internal/picker/history_provider_test.go`
