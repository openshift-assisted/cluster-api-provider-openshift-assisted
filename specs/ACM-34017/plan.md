# ACM-34017: Fix Infinite Restart Loop - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent CAPOA controller pods from entering infinite restart loops when TLS adherence policy does not require honoring cluster TLS profile.

**Architecture:** Add a guard in the SecurityProfileWatcher's `OnProfileChange` callback to check the TLS adherence policy before triggering a restart. Use `libgocrypto.ShouldHonorClusterTLSProfile()` to determine whether profile changes should trigger a restart.

**Tech Stack:** Go, Ginkgo/Gomega testing framework, OpenShift library-go, controller-runtime-common

---

## Spec Summary

CAPOA controllers incorrectly restart in an infinite loop when the cluster has a non-default TLS profile configured but the `tlsAdherence` policy is `LegacyAdheringComponentsOnly` or `NoOpinion`. The SecurityProfileWatcher detects spurious profile mismatches and triggers unnecessary shutdowns. The fix adds a guard in the `OnProfileChange` callback to skip restart when the adherence policy does not require honoring the cluster TLS profile.

## Approach

The fix implements **Option B** from the spec: add a guard in CAPOA's `OnProfileChange` callback in `internal/setup/setup.go` to check if the adherence policy requires honoring the cluster TLS profile before calling `cancel()`.

This is the minimal change that:
1. Prevents spurious restarts when `tlsAdherence` is `NoOpinion` or `LegacyAdheringComponentsOnly`
2. Preserves existing behavior when `tlsAdherence` is `StrictAllComponents`
3. Requires only ~5 lines of code change
4. Directly addresses the root cause

The watcher remains registered in all cases (needed for adherence policy change detection), but profile changes become a no-op when they don't matter.

## Changes

### internal/setup/setup.go

**What:** Add guard in `OnProfileChange` callback to check TLS adherence policy

**Why:** This is the root cause - the callback currently always calls `cancel()` on profile changes, even when the component is not required to honor the cluster profile

**Changes:**
- Import `libgocrypto "github.com/openshift/library-go/pkg/crypto"` (line ~24)
- In `SetupSecurityProfileWatcher` function, modify the `OnProfileChange` callback (lines 58-62):
  - Add check: `if !libgocrypto.ShouldHonorClusterTLSProfile(result.TLSAdherencePolicy)`
  - If true, log at V(1) level and return early
  - If false, continue with existing shutdown logic

**Existing pattern to follow:** The `internal/tlsconfig/tlsconfig.go:92` already uses `libgocrypto.ShouldHonorClusterTLSProfile(adherencePolicy)` to check if the cluster TLS profile should be honored

### internal/setup/suite_test.go

**What:** Create test suite boilerplate for the setup package

**Why:** New test file requires Ginkgo test suite setup

**Changes:**
- Create new file with standard license header
- Register Ginkgo fail handler and run specs for "Setup Suite"

**Existing pattern to follow:** `internal/tlsconfig/suite_test.go` (lines 1-30)

### internal/setup/setup_test.go

**What:** Create comprehensive unit tests for `SetupSecurityProfileWatcher` behavior

**Why:** Verify the fix works correctly for all three adherence policy values and that the guard logic is correct

**Test cases:**
1. When adherence is `NoOpinion`, profile changes should NOT call cancel
2. When adherence is `LegacyAdheringComponentsOnly`, profile changes should NOT call cancel
3. When adherence is `StrictAllComponents`, profile changes SHOULD call cancel
4. When adherence is `NoOpinion`, adherence policy changes SHOULD call cancel
5. When adherence is `StrictAllComponents`, adherence policy changes SHOULD call cancel

**Existing pattern to follow:** `internal/tlsconfig/tlsconfig_test.go` for Ginkgo test structure and fake client setup

## Data Model Changes

None. No schema, type, or API changes required.

## API Changes

None. This is an internal implementation fix with no external API changes.

## Testing Strategy

### Unit Tests
- **File:** `internal/setup/setup_test.go`
- **Framework:** Ginkgo/Gomega
- **Coverage:**
  - Verify `OnProfileChange` callback behavior for all three adherence policies
  - Verify `OnAdherencePolicyChange` callback always triggers cancel
  - Use fake manager and test whether cancel context was called

**Maps to acceptance criteria:**
- "When `tlsAdherence` is `LegacyAdheringComponentsOnly` or `NoOpinion`, changing the TLS profile on the APIServer CR does NOT trigger a controller restart" → Test cases 1-2
- "When `tlsAdherence` is `StrictAllComponents`, changing the TLS profile on the APIServer CR DOES trigger a graceful controller restart" → Test case 3
- "When `tlsAdherence` changes from `LegacyAdheringComponentsOnly` to `StrictAllComponents`, the controller restarts" → Test cases 4-5

### Manual Testing
Manual testing will be performed in an OpenShift cluster to verify end-to-end behavior:
1. Configure cluster with Modern TLS profile and `tlsAdherence: LegacyAdheringComponentsOnly`
2. Deploy CAPOA controllers
3. Verify pods start successfully and remain stable (no restart loop)
4. Change TLS profile from Modern to Intermediate
5. Verify pods do NOT restart
6. Change `tlsAdherence` to `StrictAllComponents`
7. Verify pods restart gracefully
8. Change TLS profile again
9. Verify pods restart when profile changes

**Maps to acceptance criteria:**
- All acceptance criteria in spec lines 35-52 require manual testing in an OpenShift cluster

### Build Verification
Run existing test suite to ensure no regressions:
```bash
make test
```

## Risks

### Low Risk: Import Addition
**Risk:** Adding `libgocrypto` import might introduce dependency issues

**Mitigation:** The package is already imported and used in `internal/tlsconfig/tlsconfig.go`, so it's already in the vendor directory and poses no new dependency risk

### Low Risk: Logic Error in Guard
**Risk:** Incorrect guard logic could prevent restarts when they should happen

**Mitigation:** 
- The `ShouldHonorClusterTLSProfile()` function is well-tested in library-go
- Comprehensive unit tests verify all three policy values
- Manual testing in OpenShift cluster validates end-to-end behavior

### Low Risk: Backward Compatibility
**Risk:** Change could affect existing behavior for `StrictAllComponents`

**Mitigation:**
- Guard only affects `NoOpinion` and `LegacyAdheringComponentsOnly` policies
- For `StrictAllComponents`, the guard evaluates to false and control flow is unchanged
- Test case 3 explicitly verifies `StrictAllComponents` behavior is preserved

### No Migration Risk
This is a pure code fix with no data migration, configuration changes, or API modifications. The fix is backward compatible and changes only internal behavior.
