---
jira: MGMT-24420
title: Fix bootstrap controller nil pointer panic when Machine owner is deleted - Task Breakdown
created: 2026-05-20
---

# Tasks: Fix bootstrap controller nil pointer panic when Machine owner is deleted

## Tasks

- [ ] 1. Add unit test for owner-deleted scenario
  - **Files:** `bootstrap/internal/controller/openshiftassistedconfig_controller_test.go`
  - **Depends on:** none
  - **Test:** Run `make test` or `go test ./bootstrap/internal/controller/... -run "TestOpenshiftAssistedConfig.*owner.*deleted"` — test should FAIL (exposing the panic) before the fix is applied
  - **Details:**
    - Add new test case after line 183 in the existing `Describe("OpenshiftAssistedConfig Controller")` block
    - Create cluster and machine using existing test utilities
    - Create OpenshiftAssistedConfig with owner reference to the machine
    - Delete the machine to simulate the race condition
    - Call `Reconcile()` and verify no panic occurs (this will panic before task 2)
    - Verify no conditions are set (early return behavior)
    - Follow existing test patterns (Ginkgo BDD, existing test utilities)
  - **Why test-first:** Exposes the bug before fixing it, provides regression coverage

- [ ] 2. Fix nil pointer dereference in openshiftassistedconfig_controller.go
  - **Files:** `bootstrap/internal/controller/openshiftassistedconfig_controller.go`
  - **Depends on:** 1 (test should exist to verify fix)
  - **Test:** Run `make test` — the test from task 1 should now PASS, and all existing tests should remain green
  - **Details:**
    - Modify line 150 only
    - Remove `"name", configOwner.GetName()` from the log statement
    - Keep the log message text: `"config owner not found"`
    - Do not modify the comment (line 149) or control flow (lines 148, 151)
    - Preserve debug log level (`log.V(logutil.DebugLevel)`)
  - **Why this order:** Test exists to verify the fix works, follows TDD principles

- [ ] 3. Verify all existing tests pass
  - **Files:** N/A (verification only)
  - **Depends on:** 2 (fix must be in place)
  - **Test:** Run `make test` from repository root — all tests in `bootstrap/internal/controller/` must pass
  - **Details:**
    - Execute full test suite with coverage: `make test`
    - Verify no regressions in:
      - `openshiftassistedconfig_controller_test.go` (all existing test cases)
      - `agent_controller_test.go` (bootstrap agent controller)
      - Any other bootstrap controller tests
    - Check that test coverage includes the modified line 150
  - **Acceptance criteria:** Maps to spec requirement "Existing unit tests continue to pass without modification"

- [ ] 4. Run linter and fix any issues
  - **Files:** `bootstrap/internal/controller/openshiftassistedconfig_controller.go`, `bootstrap/internal/controller/openshiftassistedconfig_controller_test.go`
  - **Depends on:** 2 (code must be complete)
  - **Test:** Run `make lint` — should pass with zero warnings
  - **Details:**
    - Run `make lint` to check for code quality issues
    - If linter reports issues in the modified code, run `make lint-fix` to auto-fix
    - Manually fix any issues that cannot be auto-fixed
    - Verify line length, formatting, and Go conventions are followed
  - **Why:** Ensures code meets project quality standards before review

- [ ] 5. Verify reconciliation continues after error (smoke test)
  - **Files:** N/A (manual verification)
  - **Depends on:** 2 (fix must be in place)
  - **Test:** Inspect test output from task 1 — verify that reconciliation returns `(ctrl.Result{}, nil)` without panic
  - **Details:**
    - Review test from task 1 to confirm:
      - `Reconcile()` returns without error
      - No panic occurs (test would fail if panic happened)
      - Early return happens (no conditions set on OpenshiftAssistedConfig)
    - This verifies spec requirement #3: "controller MUST return without error"
    - This verifies spec requirement #4: "controller MUST continue processing other objects" (no panic means queue continues)
  - **Acceptance criteria:** Maps to spec requirements #3 and #4

## Parallelization Notes

Tasks must be executed sequentially in order 1 → 2 → 3 → 4 → 5 because:
- Task 2 depends on task 1 (test-first development)
- Task 3 depends on task 2 (can't verify tests pass until fix is in)
- Task 4 depends on task 2 (can't lint code that doesn't exist)
- Task 5 depends on task 2 (can't verify behavior until fix exists)

No tasks can be parallelized.

## Success Criteria

All tasks complete when:
1. ✅ New test case exists in `openshiftassistedconfig_controller_test.go`
2. ✅ Line 150 no longer dereferences nil `configOwner`
3. ✅ `make test` passes (all bootstrap tests green)
4. ✅ `make lint` passes (zero warnings)
5. ✅ Reconciliation returns without error when owner is deleted (verified by test)

## Estimated Effort

- **Task 1:** 15-20 minutes (write test using existing patterns)
- **Task 2:** 5 minutes (one-line change)
- **Task 3:** 2-3 minutes (run test suite)
- **Task 4:** 2-3 minutes (run linter)
- **Task 5:** 1 minute (review test output)

**Total:** ~25-35 minutes of active development time

## Notes

- This is a minimal, surgical fix targeting a single line
- No architectural changes or refactoring needed
- No changes to dependencies, CRDs, or APIs
- Test coverage is comprehensive despite simplicity (unit test directly exercises the bug path)
- The fix maintains existing behavior for all other code paths (see plan.md "Risks" section)
