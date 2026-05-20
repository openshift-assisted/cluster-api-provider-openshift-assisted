---
jira: MGMT-24420
title: Fix bootstrap controller nil pointer panic when Machine owner is deleted
created: 2026-05-20
---

# Specification: Fix bootstrap controller nil pointer panic when Machine owner is deleted

## Overview

The CAPOA bootstrap controller (capoa-bootstrap-controller-manager) crashes with a nil pointer dereference when an OpenshiftAssistedConfig's owning Machine is deleted before reconciliation completes. This panic causes the controller's single reconcile worker to enter a panic-recovery-retry loop that eventually exhausts backoff, after which all reconciliation stops. This blocks spoke cluster installation for all machines, not just the deleted one. This specification defines requirements to gracefully handle the owner-deleted case without panicking, allowing the controller to continue processing other OpenshiftAssistedConfig objects.

## User Stories

- As a cluster administrator deploying spoke clusters via CAPI with assisted-service, I want the bootstrap controller to remain operational when MachineSet scaling operations occur, so that valid machines can continue their installation process without disruption.

- As a platform engineer, I want the bootstrap controller to handle race conditions between Machine deletion and OpenshiftAssistedConfig reconciliation gracefully, so that temporary over-provisioning scenarios (like CAPI issue #13649) don't cause complete reconciliation failure.

- As an SRE monitoring spoke cluster deployments, I want clear, actionable log messages when OpenshiftAssistedConfig objects have missing owners, so that I can distinguish between transient conditions and actual problems requiring intervention.

## Requirements

1. When `GetTypedConfigOwner()` returns a `NotFound` error (indicating the owner Machine was deleted), the controller MUST NOT dereference the nil `configOwner` pointer.

2. When an OpenshiftAssistedConfig's owner Machine is not found, the controller MUST log an appropriate message without attempting to access fields on the nil `configOwner` object.

3. When an OpenshiftAssistedConfig's owner Machine is not found, the controller MUST return without error (allowing Kubernetes to requeue naturally via owner reference garbage collection).

4. The controller MUST continue processing other OpenshiftAssistedConfig objects in the work queue when one object's owner is deleted.

5. The fix MUST NOT change the existing behavior for other error conditions returned by `GetTypedConfigOwner()`.

6. The fix MUST NOT change the existing behavior when `configOwner` is successfully retrieved (non-nil and no error).

## Acceptance Criteria

- The controller does not panic when an OpenshiftAssistedConfig's owner Machine is deleted during reconciliation.

- When a Machine is deleted while its OpenshiftAssistedConfig is being reconciled, the controller logs a debug-level message indicating the owner was not found, without including the owner's name (since it's nil).

- After handling an OpenshiftAssistedConfig with a missing owner, the controller continues to reconcile other OpenshiftAssistedConfig objects without entering a panic loop.

- Existing unit tests continue to pass without modification. [ASSUMPTION: unit tests exist for the OpenshiftAssistedConfig controller]

- A new test case exists that verifies graceful handling when `GetTypedConfigOwner()` returns `(nil, NotFoundError)`.

- In a scenario matching the JIRA description (MachineSet scales down from 4 to 2 workers while OpenshiftAssistedConfig objects are in the work queue), all valid OpenshiftAssistedConfig objects (for the 2 remaining workers) are reconciled successfully, and bootstrap data secrets are created.

## Non-Functional Requirements

### Backward Compatibility
- The fix MUST NOT alter the controller's behavior for successfully-found owners or other error types.
- The fix MUST maintain the existing log message intent (indicating owner not found), only removing the unsafe field dereference.

### Operational Impact
- The fix should have zero performance impact, as it only changes error handling logic in a failure path.
- Log volume should remain essentially unchanged (same log message, just without the name field).

### Testing
- The fix MUST be testable via unit tests that mock `GetTypedConfigOwner()` to return the problematic `(nil, NotFoundError)` state.

## Out of Scope

- Fixing the upstream CAPI cache sync race condition (kubernetes-sigs/cluster-api#13649) that can trigger MachineSet over-provisioning. This is an upstream issue and outside the scope of CAPOA.

- Adding retry logic or special handling for transient owner-not-found conditions. The existing reconciliation mechanism (owner reference watches and garbage collection) is sufficient.

- Changing the log level for the "owner not found" message. The log level (DebugLevel) was already adjusted in commit abfb0de5 and is appropriate for this condition.

- Adding metrics or telemetry for owner-not-found events. This is a normal condition during scale-down and doesn't warrant dedicated instrumentation.

- Handling other potential nil pointer issues in the codebase. This spec focuses solely on the identified bug in line 150 of `openshiftassistedconfig_controller.go`.

- Adding concurrency to the bootstrap controller (increasing MaxConcurrentReconciles from 1). While multiple workers would reduce the blast radius of panics, the root cause fix is to eliminate the panic itself.

## Open Questions

None. The bug is well-understood: line 150 in `bootstrap/internal/controller/openshiftassistedconfig_controller.go` dereferences `configOwner.GetName()` when `configOwner` is nil (when `GetTypedConfigOwner` returns a NotFound error).
