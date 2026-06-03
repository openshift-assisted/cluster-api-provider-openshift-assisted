# ACM-34017: Fix Infinite Restart Loop with Non-Honoring TLS Adherence Policies

## Overview

CAPOA (Cluster API Provider OpenShift Assisted) bootstrap and controlplane controller managers incorrectly enter an infinite restart loop when the cluster has a non-default TLS security profile configured but the `tlsAdherence` policy does not require components to honor it (i.e., `LegacyAdheringComponentsOnly` or unset/`NoOpinion`). The SecurityProfileWatcher detects a spurious profile mismatch on every startup and triggers a manager shutdown, causing pods to cycle indefinitely in CrashLoopBackOff. This bug prevents CAPOA from operating correctly in clusters where administrators have configured a TLS profile for core components but have not yet mandated universal adherence.

## User Stories

- As a cluster administrator, I want to configure a non-default TLS security profile (e.g., Modern) for core OpenShift components without forcing all components to adopt it immediately, so that I can gradually migrate my cluster to stricter TLS settings.

- As a CAPOA operator, I want the controller pods to remain stable and healthy when the cluster's `tlsAdherence` policy is set to `LegacyAdheringComponentsOnly` or unset, so that I can continue managing assisted-install clusters without service disruption.

- As a security engineer, I want CAPOA controllers to correctly respect the `tlsAdherence` policy by using default Intermediate TLS settings when not required to honor the cluster profile, so that the system behaves according to OpenShift's TLS adherence contract.

## Requirements

1. When `tlsAdherence` is set to `LegacyAdheringComponentsOnly` or `NoOpinion` (unset), CAPOA controllers MUST NOT restart due to TLS profile changes on the APIServer CR.

2. When `tlsAdherence` is set to `LegacyAdheringComponentsOnly` or `NoOpinion`, CAPOA controllers MUST continue using the default Intermediate TLS profile for their own TLS configuration (existing behavior).

3. When `tlsAdherence` is set to `StrictAllComponents`, CAPOA controllers MUST restart when the TLS profile on the APIServer CR changes (existing behavior).

4. The SecurityProfileWatcher MUST NOT detect spurious TLS profile mismatches when the controller is not required to honor the cluster TLS profile.

5. The fix MUST preserve backward compatibility with existing behavior for all three adherence policy values: `NoOpinion`, `LegacyAdheringComponentsOnly`, and `StrictAllComponents`.

6. The fix MUST NOT change the TLS configuration actually applied to the controller's HTTP servers, metrics endpoints, or webhook servers when `tlsAdherence` does not require honoring the cluster profile.

7. Controller pods MUST stabilize and reach a healthy running state within normal Kubernetes startup time when `tlsAdherence` is `LegacyAdheringComponentsOnly` or `NoOpinion`.

8. When the `tlsAdherence` policy changes from a non-honoring policy to `StrictAllComponents`, the controller MUST restart to apply the cluster TLS profile.

## Acceptance Criteria

- Starting with `tlsAdherence` set to `StrictAllComponents` and TLS profile Modern, then switching `tlsAdherence` to `LegacyAdheringComponentsOnly` does NOT cause CAPOA pods to enter a restart loop.

- Starting with `tlsAdherence` unset (NoOpinion) and TLS profile Modern configured, CAPOA pods start successfully and remain stable.

- Starting with `tlsAdherence` set to `LegacyAdheringComponentsOnly` and TLS profile Modern, CAPOA pods start successfully and remain stable.

- When `tlsAdherence` is `LegacyAdheringComponentsOnly` or `NoOpinion`, changing the TLS profile on the APIServer CR (e.g., from Intermediate to Modern) does NOT trigger a controller restart.

- When `tlsAdherence` is `StrictAllComponents`, changing the TLS profile on the APIServer CR DOES trigger a graceful controller restart (existing behavior verified).

- When `tlsAdherence` changes from `LegacyAdheringComponentsOnly` to `StrictAllComponents`, the controller restarts to honor the new policy.

- Controller logs do NOT show repeated "TLS profile changed, shutting down to reload" messages when the adherence policy does not require honoring the cluster profile.

- The controller's actual TLS configuration (cipher suites, minimum TLS version) matches the Intermediate default when `tlsAdherence` is `LegacyAdheringComponentsOnly` or `NoOpinion`.

- The controller's actual TLS configuration matches the cluster-configured profile when `tlsAdherence` is `StrictAllComponents`.

## Non-Functional Requirements

- **Backward Compatibility**: The fix must not alter the existing behavior when `tlsAdherence` is `StrictAllComponents`. Components already relying on restart-on-profile-change must continue to work.

- **Performance**: The fix must not introduce additional API calls, reconciliation loops, or watchers that increase resource consumption.

- **Correctness**: The fix must correctly implement the OpenShift TLS adherence contract as defined by the `ShouldHonorClusterTLSProfile()` function from library-go.

- **Simplicity**: The fix should minimize complexity. Changes should be localized to TLS configuration initialization and/or watcher setup logic.

## Out of Scope

- Changing the default TLS profile used by CAPOA when not honoring the cluster profile (remains Intermediate).

- Adding new `tlsAdherence` policy values or behaviors beyond the three existing values.

- Dynamically reloading TLS configuration without a restart when the adherence policy requires honoring the cluster profile.

- Changes to the SecurityProfileWatcher's core reconciliation logic beyond what is necessary to fix this bug.

- Modifying how other OpenShift components handle TLS adherence policies.

- Adding configuration options for administrators to override the default TLS profile for CAPOA.

## Solution Approach

The fix will use **Option B**: implement a guard in CAPOA's `OnProfileChange` callback to skip profile change handling when the adherence policy does not require honoring the cluster TLS profile.

### Why Option B

**Option A** (initialize the watcher with the actual APIServer profile instead of defaults) prevents the infinite loop but still causes one spurious restart when an admin applies a new TLS profile under a non-honoring adherence policy:

1. Controller starts with `LegacyAdheringComponentsOnly`, no TLS profile configured. Watcher initialized with Intermediate (actual = default = Intermediate).
2. Admin applies Modern profile on the APIServer CR.
3. Watcher reconciles: Modern != Intermediate → `OnProfileChange` fires → one unnecessary restart.
4. After restart, watcher initialized with Modern → stable.

**Option B** fully satisfies Requirement 1 (no spurious restarts) and can be implemented entirely within CAPOA's `OnProfileChange` callback in `internal/setup/setup.go` without requiring upstream controller-runtime-common changes:

```go
OnProfileChange: func(ctx context.Context, oldProfile, newProfile configv1.TLSProfileSpec) {
    if !libgocrypto.ShouldHonorClusterTLSProfile(result.TLSAdherencePolicy) {
        tlsLog.V(1).Info("TLS profile changed but adherence policy does not require honoring, ignoring",
            "policy", result.TLSAdherencePolicy)
        return
    }
    tlsLog.Info("TLS profile changed, shutting down to reload",
        "oldProfile", oldProfile, "newProfile", newProfile)
    cancel()
},
```

This is safe because:

- If adherence is non-honoring at startup, profile changes are ignored (no restart).
- If adherence later changes to `StrictAllComponents`, the `OnAdherencePolicyChange` callback fires first and triggers a restart. The new manager starts with `StrictAllComponents`, and subsequent profile changes will correctly call `cancel()`.
- The watcher stays registered in all cases (needed for adherence policy change detection), but profile changes become a no-op when they don't matter.

This approach requires only ~5 lines of code change and directly addresses the root cause.
