---
jira: MGMT-24420
title: Fix bootstrap controller nil pointer panic when Machine owner is deleted - Implementation Plan
created: 2026-05-20
---

# Implementation Plan: Fix bootstrap controller nil pointer panic when Machine owner is deleted

## Spec Summary

The bootstrap controller (`capoa-bootstrap-controller-manager`) crashes with a nil pointer dereference when an OpenshiftAssistedConfig's owning Machine is deleted before reconciliation completes. This occurs at line 150 of `openshiftassistedconfig_controller.go` where `configOwner.GetName()` is called on a nil pointer when `GetTypedConfigOwner()` returns `(nil, NotFoundError)`. The fix must eliminate the panic, log appropriately, and allow continued reconciliation of other objects.

## Approach

This is a straightforward nil pointer bug fix with test coverage. The solution involves:

1. **Defensive coding**: Remove the unsafe field dereference from the log statement when `configOwner` is nil
2. **Test-first approach**: Add a unit test that verifies graceful handling before implementing the fix
3. **Minimal change**: Keep the existing control flow intact; only modify the log statement

The fix targets line 150 in `bootstrap/internal/controller/openshiftassistedconfig_controller.go` where the `IsNotFound` error path attempts to log the owner's name despite `configOwner` being nil.

**Why this approach:**
- Zero risk of behavioral regression (only changes a log statement in an error path)
- Maintains existing reconciliation logic (early return without error is correct)
- Preserves log intent (still logs that owner was not found, just without the name field)
- Testable via unit test mocking

## Changes

### File: `bootstrap/internal/controller/openshiftassistedconfig_controller.go`

**Lines 148-152** — Fix nil pointer dereference in NotFound error path

Current code:
```go
configOwner, err := bsutil.GetTypedConfigOwner(ctx, r.Client, config)
if apierrors.IsNotFound(err) {
    // Could not find the owner yet, this is not an error and will re-reconcile when the owner gets set.
    log.V(logutil.DebugLevel).Info("config owner not found", "name", configOwner.GetName())
    return ctrl.Result{}, nil
}
```

**Change:** Remove `"name", configOwner.GetName()` from line 150 since `configOwner` is nil when `IsNotFound(err)` is true.

Updated code:
```go
configOwner, err := bsutil.GetTypedConfigOwner(ctx, r.Client, config)
if apierrors.IsNotFound(err) {
    // Could not find the owner yet, this is not an error and will re-reconcile when the owner gets set.
    log.V(logutil.DebugLevel).Info("config owner not found")
    return ctrl.Result{}, nil
}
```

**Rationale:**
- When `GetTypedConfigOwner()` returns a `NotFound` error, `configOwner` is `nil` (see vendor code at `vendor/sigs.k8s.io/cluster-api/bootstrap/util/configowner.go:203-206`)
- The log message intent is preserved (still indicates owner not found)
- The owner's name is unknowable because the object doesn't exist
- Follows Kubernetes logging conventions (debug level is appropriate for normal scale-down events)

### File: `bootstrap/internal/controller/openshiftassistedconfig_controller_test.go`

**Add new test case** — Verify graceful handling when owner Machine is deleted

Add a new `When` block in the existing `Describe("OpenshiftAssistedConfig Controller")` test suite (after line 183, near other owner-related tests):

```go
When("OpenshiftAssistedConfig owner Machine is deleted during reconciliation", func() {
    It("should not panic and continue gracefully", func() {
        // Create cluster and machine first
        cluster := testutils.NewCluster(clusterName, namespace)
        Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
        
        machine := testutils.NewMachine(namespace, machineName, clusterName)
        Expect(k8sClient.Create(ctx, machine)).To(Succeed())
        Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(machine), machine)).To(Succeed())
        machine.SetGroupVersionKind(clusterv1.GroupVersion.WithKind("Machine"))
        
        // Create OpenshiftAssistedConfig with owner reference to machine
        oac := NewOpenshiftAssistedConfigWithOwner(namespace, oacName, clusterName, machine)
        Expect(k8sClient.Create(ctx, oac)).To(Succeed())
        
        // Delete the machine before reconciliation
        Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
        
        // Reconcile should not panic
        _, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
            NamespacedName: client.ObjectKeyFromObject(oac),
        })
        Expect(err).NotTo(HaveOccurred())
        
        // Verify no conditions were set (early return before condition logic)
        Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(oac), oac)).To(Succeed())
        condition := v1beta1conditions.Get(oac, bootstrapv1alpha2.DataSecretAvailableCondition)
        Expect(condition).To(BeNil())
    })
})
```

**Test design rationale:**
- Reproduces the exact scenario from the spec: Machine deleted while OpenshiftAssistedConfig exists
- Uses existing test utilities (`NewOpenshiftAssistedConfigWithOwner`, `NewMachine`)
- Follows existing test patterns (Ginkgo BDD style with `When`/`It`)
- Verifies both non-panic behavior and correct early return (no conditions set)
- Positions the test near related owner-handling tests for logical grouping

## Data Model Changes

None. This is a bug fix in error handling logic only.

## API Changes

None. No changes to CRDs, status fields, or reconciliation behavior beyond the log statement.

## Testing Strategy

### Unit Tests
1. **New test case** (required by spec acceptance criteria): Verify no panic when `GetTypedConfigOwner()` returns `(nil, NotFoundError)`
   - Maps to acceptance criterion: "A new test case exists that verifies graceful handling when GetTypedConfigOwner() returns (nil, NotFoundError)"
   - Located in: `bootstrap/internal/controller/openshiftassistedconfig_controller_test.go`

2. **Existing tests** must continue to pass (spec requirement #5: no behavior change for other error conditions)
   - Run with: `make test`
   - All existing tests in `openshiftassistedconfig_controller_test.go` must remain green

### Manual Verification Commands
```bash
# Run all bootstrap controller tests
make test

# Run only the OpenshiftAssistedConfig controller tests with verbose output
TEST_VERBOSE=true go test ./bootstrap/internal/controller/... -run TestOpenshiftAssistedConfig -v

# Run linter to verify code quality
make lint
```

### Acceptance Criteria Mapping

| Acceptance Criterion | Test/Verification Method |
|---------------------|--------------------------|
| Controller does not panic when owner deleted | New unit test: panic would fail the test |
| Debug log message logged without owner name | Verified by code inspection (log statement contains no name field) |
| Controller continues processing other configs | Implicit in test: reconciliation returns without error, allowing queue to continue |
| Existing unit tests pass | Run `make test` |
| New test case for NotFoundError | New test case added |
| Real-world MachineSet scale-down scenario | Out of scope for unit tests; covered by e2e tests if needed |

## Risks

### Low Risk: Regression in other error paths
**Mitigation:** The change only affects the `IsNotFound(err)` branch. The fix does not modify:
- Lines 153-158: Other error handling (non-NotFound errors)
- Lines 156-158: Nil configOwner check (separate from NotFound case)
- Lines 160+: Success path logic

### Low Risk: Log message information loss
**Impact assessment:** The removed `name` field was always nil, so no actual information is lost. The log message still clearly indicates "config owner not found" which is sufficient for debugging.

### Low Risk: Test coverage gaps
**Mitigation:** The new test directly exercises the problematic code path by deleting the Machine before reconciliation, exactly matching the real-world scenario.

## Backward Compatibility

**Code behavior:** Fully backward compatible. The change only affects logging in an error path; all other behavior remains identical.

**Log format:** Minor change to debug log format (removed nil `name` field). Debug logs are not part of any API contract and are expected to evolve.

**Existing tests:** No modifications needed to existing tests (per spec acceptance criteria). All existing tests verify current behavior and will continue to pass.

## Performance Impact

Zero. The change removes a single field from a debug log statement in an error path. No additional computation, network calls, or memory allocation.

## Dependencies

None. The fix uses only existing APIs and does not require changes to dependencies or vendored code.

## Verification Checklist

Before marking implementation complete, verify:

- [ ] Line 150 in `openshiftassistedconfig_controller.go` no longer dereferences `configOwner`
- [ ] New test case exists in `openshiftassistedconfig_controller_test.go`
- [ ] `make test` passes (all existing tests green)
- [ ] `make lint` passes (no new linter warnings)
- [ ] Log statement still conveys "owner not found" intent at debug level
- [ ] No changes to lines outside the NotFound error path (lines 148-152)
