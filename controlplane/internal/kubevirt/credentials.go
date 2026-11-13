/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubevirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	InfraCredentialsCMName                        = "kubevirt-infra-credentials-manifests"
	infraCredentialsRoleBindingAnnotation         = "capoa.openshift.io/infra-credentials-role-binding"
	infraCredentialsPendingRoleBindingsAnnotation = "capoa.openshift.io/pending-infra-credentials-role-bindings"
)

var errInfraCredentialsTargetUnreachable = errors.New("infra credentials target unreachable")
var errInfraCredentialsTargetChanged = errors.New("infra credentials target changed")
var errInfraCredentialsDeleteUnauthorized = errors.New("infra credentials delete unauthorized")

type infraCredentialsRoleBindingRecord struct {
	InfraClusterRef    string `json:"infraClusterRef"`
	InfraClusterTarget string `json:"infraClusterTarget"`
	InfraNamespace     string `json:"infraNamespace"`
	RoleBindingName    string `json:"roleBindingName"`
}

type infraCredentialsMutexEntry struct {
	mutex sync.Mutex
	refs  int
}

var infraCredentialsMutexes = struct {
	sync.Mutex
	entries map[string]*infraCredentialsMutexEntry
}{entries: map[string]*infraCredentialsMutexEntry{}}

func lockInfraCredentials(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) func() {
	key := oacp.Namespace + "/" + oacp.Name
	infraCredentialsMutexes.Lock()
	entry := infraCredentialsMutexes.entries[key]
	if entry == nil {
		entry = &infraCredentialsMutexEntry{}
		infraCredentialsMutexes.entries[key] = entry
	}
	entry.refs++
	infraCredentialsMutexes.Unlock()

	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()

		infraCredentialsMutexes.Lock()
		defer infraCredentialsMutexes.Unlock()
		entry.refs--
		if entry.refs == 0 {
			delete(infraCredentialsMutexes.entries, key)
		}
	}
}

// EnsureInfraCredentialsManifests creates a ConfigMap containing the Secret manifests
// that provide the infra cluster kubeconfig to the CCM and CSI operators on the tenant
// cluster. These manifests are injected during installation via AgentClusterInstall's
// ManifestsConfigMapRefs API.
//
// NOTE: The kubeconfig data is embedded in Secret manifest YAML stored in a ConfigMap.
// This is a constraint of the assisted-service manifest injection API, which only
// supports ConfigMap-based references (ManifestsConfigMapRefs). The source Secret
// already exists in the same namespace with equivalent access scope. CAPK creates
// this Secret with a narrowly-scoped ServiceAccount token.
func EnsureInfraCredentialsManifests(
	ctx context.Context,
	sourceClient client.Client,
	infraResult *InfraClientResult,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	unlock := lockInfraCredentials(oacp)
	defer unlock()

	currentOACP, err := getInfraCredentialsOACP(ctx, sourceClient, oacp)
	if err != nil || currentOACP == nil {
		return err
	}
	if currentOACP.DeletionTimestamp != nil {
		return nil
	}
	oacp = currentOACP
	if err := ensureInfraCredentialsTargetCurrent(ctx, sourceClient, oacp, infraResult); err != nil {
		return err
	}
	infraClient := infraResult.Client
	infraNamespace := infraResult.Namespace

	podNetworking := IsPodNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs)
	ccmRequiresCredentials := oacp.Spec.Config.CloudControllerManager != nil &&
		oacp.Spec.Config.CloudControllerManager.Enabled && podNetworking
	csiEnabled := oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.Enabled

	if err := cleanupInactiveInfraCredentialsRoleBinding(ctx, sourceClient, infraClient, oacp, ccmRequiresCredentials); err != nil {
		return err
	}

	if err := reconcilePendingInfraCredentialsRoleBindings(ctx, sourceClient, oacp); err != nil {
		return err
	}

	needsCredentials := ccmRequiresCredentials || csiEnabled
	if !needsCredentials {
		return nil
	}

	kubeconfigData, err := getInfraCredentialsKubeconfig(ctx, sourceClient, oacp)
	if err != nil {
		return err
	}

	if ccmRequiresCredentials {
		if err := ensureCCMInfraCredentialsRBAC(ctx, sourceClient, infraClient, infraNamespace, oacp, kubeconfigData); err != nil {
			return err
		}
	}

	if IsPodNetworking(oacp.Spec.Config.APIVIPs, oacp.Spec.Config.IngressVIPs) {
		kubeconfigData, err = rewriteInfraCredentialsKubeconfig(ctx, infraClient, kubeconfigData)
		if err != nil {
			return err
		}
	}

	manifests := buildInfraCredentialsManifests(infraNamespace, kubeconfigData, ccmRequiresCredentials, csiEnabled)
	return ensureManifestsConfigMap(ctx, sourceClient, oacp, InfraCredentialsCMName, manifests)
}

func cleanupInactiveInfraCredentialsRoleBinding(
	ctx context.Context,
	sourceClient client.Client,
	infraClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	ccmRequiresCredentials bool,
) error {
	if ccmRequiresCredentials || oacp.Annotations[infraCredentialsRoleBindingAnnotation] == "" {
		return nil
	}

	log := ctrl.LoggerFrom(ctx)
	record, err := parseInfraCredentialsRoleBindingRecord(oacp.Annotations[infraCredentialsRoleBindingAnnotation])
	if err != nil {
		return err
	}
	recordClient, err := infraClientForRecord(ctx, sourceClient, oacp, record, infraClient, true)
	if err != nil {
		if !errors.Is(err, errInfraCredentialsTargetUnreachable) {
			return err
		}
		log.Info(
			"recorded infra CCM role binding target is unreachable; retaining it for later cleanup",
			"infraClusterRef", record.InfraClusterRef,
			"error", err,
		)
		if err := retainInfraCredentialsRoleBindingRecord(ctx, sourceClient, oacp, record); err != nil {
			return err
		}
	} else if err := deleteInfraCredentialsRoleBinding(ctx, recordClient, record); err != nil {
		return err
	}
	return clearInfraCredentialsRoleBindingAnnotation(ctx, sourceClient, oacp)
}

func getInfraCredentialsKubeconfig(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) (string, error) {
	credSecretName := "infra-cluster-credentials"
	if oacp.Spec.Config.InfraClusterRef != nil {
		credSecretName = oacp.Spec.Config.InfraClusterRef.Name
	}

	sourceSecret := &corev1.Secret{}
	if err := sourceClient.Get(ctx, client.ObjectKey{
		Name:      credSecretName,
		Namespace: oacp.Namespace,
	}, sourceSecret); err != nil {
		return "", fmt.Errorf("failed to get infra credentials secret %s/%s: %w",
			oacp.Namespace, credSecretName, err)
	}

	kubeconfigData := string(sourceSecret.Data["kubeconfig"])
	if kubeconfigData == "" {
		return "", fmt.Errorf("infra credentials secret %s does not contain 'kubeconfig' key", credSecretName)
	}
	return kubeconfigData, nil
}

func ensureCCMInfraCredentialsRBAC(
	ctx context.Context,
	sourceClient client.Client,
	infraClient client.Client,
	infraNamespace string,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	kubeconfigData string,
) error {
	record, err := newInfraCredentialsRoleBindingRecord(oacp, infraNamespace, kubeconfigData)
	if err != nil {
		return err
	}
	if err := ensureInfraCredentialsRoleBindingRecord(ctx, sourceClient, infraClient, oacp, record); err != nil {
		return err
	}
	return ensureInfraCredentialsRBAC(ctx, infraClient, infraNamespace, kubeconfigData, infraRoleBindingName(oacp))
}

func rewriteInfraCredentialsKubeconfig(
	ctx context.Context,
	infraClient client.Client,
	kubeconfigData string,
) (string, error) {
	infraAPIIP, err := GetOptionalInfraKubernetesServiceIP(ctx, infraClient)
	if err != nil {
		return "", err
	}
	if infraAPIIP == "" {
		return kubeconfigData, nil
	}
	return rewriteKubeconfigForInfraService(kubeconfigData, infraAPIIP)
}

func buildInfraCredentialsManifests(
	infraNamespace string,
	kubeconfigData string,
	ccmRequiresCredentials bool,
	csiEnabled bool,
) []ManifestEntry {
	var manifests []ManifestEntry
	if ccmRequiresCredentials {
		manifests = append(manifests, ManifestEntry{
			Filename: "01-ccm-credentials-secret.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: openshift-cloud-controller-manager
type: Opaque
stringData:
  kubeconfig: |
%s
`, ccmCredSecretName, indentMultiline(kubeconfigData)),
		})
	}
	if csiEnabled {
		manifests = append(manifests, ManifestEntry{
			Filename: "02-csi-credentials-secret.yaml",
			Content: fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: openshift-cluster-csi-drivers
type: Opaque
stringData:
  infraClusterNamespace: %s
  kubeconfig: |
%s
`, csiCredSecretName, infraNamespace, indentMultiline(kubeconfigData)),
		})
	}
	if len(manifests) == 0 {
		// Even when CCM/CSI are not enabled, if InfraClusterCredentials is set,
		// create an empty ConfigMap so that ACI's manifestsConfigMapRefs doesn't
		// fail with "ConfigMap not found".
		return []ManifestEntry{
			{Filename: "placeholder.yaml", Content: "# No infra credentials manifests needed\n"},
		}
	}
	return manifests
}

func ensureInfraCredentialsRBAC(
	ctx context.Context,
	infraClient client.Client,
	infraNamespace string,
	kubeconfigData string,
	roleBindingName string,
) error {
	if infraClient == nil {
		return fmt.Errorf("infra cluster client is required for CCM credentials")
	}

	subject, err := kubeconfigSubject(ctx, kubeconfigData)
	if err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ccmInfraRoleName,
			Namespace: infraNamespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, infraClient, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"services"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"discovery.k8s.io"},
				Resources: []string{"endpointslices"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"kubevirt.io"},
				Resources: []string{"virtualmachineinstances"},
				Verbs:     []string{"get", "list", "watch"},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to ensure infra CCM role: %w", err)
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: infraNamespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, infraClient, roleBinding, func() error {
		if !containsRoleBindingSubject(roleBinding.Subjects, subject) {
			roleBinding.Subjects = append(roleBinding.Subjects, subject)
		}
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     ccmInfraRoleName,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to ensure infra CCM role binding: %w", err)
	}

	return nil
}

func containsRoleBindingSubject(subjects []rbacv1.Subject, subject rbacv1.Subject) bool {
	for _, existing := range subjects {
		if existing.Kind == subject.Kind &&
			existing.APIGroup == subject.APIGroup &&
			existing.Name == subject.Name &&
			existing.Namespace == subject.Namespace {
			return true
		}
	}
	return false
}

func CleanupInfraCredentialsRBAC(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraClient func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error),
) error {
	unlock := lockInfraCredentials(oacp)
	defer unlock()

	currentOACP, err := getInfraCredentialsOACP(ctx, sourceClient, oacp)
	if err != nil || currentOACP == nil {
		return err
	}
	oacp = currentOACP

	var currentRecord *infraCredentialsRoleBindingRecord
	if value := oacp.Annotations[infraCredentialsRoleBindingAnnotation]; value != "" {
		record, err := parseInfraCredentialsRoleBindingRecord(value)
		if err != nil {
			return err
		}
		currentRecord = record
	}
	pendingRecords, err := parseInfraCredentialsPendingRecords(oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
	if err != nil {
		return err
	}
	if currentRecord == nil && len(pendingRecords) == 0 {
		return nil
	}

	var currentClient client.Client
	if currentRecord != nil {
		if infraClient == nil {
			return fmt.Errorf("infra cluster client factory is required for CCM credentials cleanup")
		}
		currentClient, err = infraClient(oacp)
		if err != nil {
			if !errors.Is(err, errInfraCredentialsTargetUnreachable) {
				return fmt.Errorf("failed to get infra cluster client for CCM credentials cleanup: %w", err)
			}
			return fmt.Errorf("current infra CCM role binding target is unreachable; retaining cleanup record: %w", err)
		}
	}

	cleanupRecord := func(record *infraCredentialsRoleBindingRecord, allowHistoricalRecord bool) error {
		var recordClient client.Client
		var err error
		if allowHistoricalRecord {
			recordClient, err = infraClientForHistoricalRecord(ctx, sourceClient, oacp, record)
		} else {
			recordClient, err = infraClientForRecord(ctx, sourceClient, oacp, record, currentClient, false)
		}
		if err != nil {
			if !errors.Is(err, errInfraCredentialsTargetUnreachable) {
				return fmt.Errorf("failed to get infra cluster client for CCM credentials cleanup: %w", err)
			}
			return fmt.Errorf("infra CCM role binding target is unreachable; retaining cleanup record: %w", err)
		}
		if err := deleteInfraCredentialsRoleBinding(ctx, recordClient, record); err != nil {
			if errors.Is(err, errInfraCredentialsDeleteUnauthorized) {
				return fmt.Errorf("infra CCM role binding cannot be deleted; retaining cleanup record: %w", err)
			}
			return err
		}
		return nil
	}

	if currentRecord != nil {
		if err := cleanupRecord(currentRecord, false); err != nil {
			return err
		}
	}
	for index := range pendingRecords {
		record := pendingRecords[index]
		if err := cleanupRecord(&record, true); err != nil {
			return err
		}
	}

	return clearInfraCredentialsRoleBindingAnnotations(ctx, sourceClient, oacp)
}

func getInfraCredentialsOACP(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) (*controlplanev1alpha3.OpenshiftAssistedControlPlane, error) {
	currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
	if err := sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OpenshiftAssistedControlPlane %s/%s: %w", oacp.Namespace, oacp.Name, err)
	}
	return currentOACP, nil
}

func newInfraCredentialsRoleBindingRecord(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraNamespace string,
	kubeconfigData string,
) (*infraCredentialsRoleBindingRecord, error) {
	infraClusterRef := localInfraClusterTarget
	target := localInfraClusterTarget
	if oacp.Spec.Config.InfraClusterRef != nil {
		infraClusterRef = "secret/" + oacp.Spec.Config.InfraClusterRef.Name
		var err error
		target, err = kubeconfigClusterTarget(kubeconfigData)
		if err != nil {
			return nil, err
		}
	}
	return &infraCredentialsRoleBindingRecord{
		InfraClusterRef:    infraClusterRef,
		InfraClusterTarget: target,
		InfraNamespace:     infraNamespace,
		RoleBindingName:    infraRoleBindingName(oacp),
	}, nil
}

func parseInfraCredentialsRoleBindingRecord(value string) (*infraCredentialsRoleBindingRecord, error) {
	record := &infraCredentialsRoleBindingRecord{}
	if err := json.Unmarshal([]byte(value), record); err == nil {
		if record.InfraNamespace == "" || record.RoleBindingName == "" {
			return nil, fmt.Errorf("invalid infra CCM role binding record %q", value)
		}
		return record, nil
	}

	infraNamespace, roleBindingName, found := strings.Cut(value, "/")
	if !found || infraNamespace == "" || roleBindingName == "" {
		return nil, fmt.Errorf("invalid infra CCM role binding record %q", value)
	}
	record.InfraNamespace = infraNamespace
	record.RoleBindingName = roleBindingName
	return record, nil
}

func (record *infraCredentialsRoleBindingRecord) equal(other *infraCredentialsRoleBindingRecord) bool {
	return *record == *other
}

func (record *infraCredentialsRoleBindingRecord) legacyCompatible(other *infraCredentialsRoleBindingRecord) bool {
	return record.InfraClusterRef == "" &&
		record.InfraNamespace == other.InfraNamespace &&
		record.RoleBindingName == other.RoleBindingName
}

func (record *infraCredentialsRoleBindingRecord) marshal() (string, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("failed to encode infra CCM role binding record: %w", err)
	}
	return string(data), nil
}

func parseInfraCredentialsPendingRecords(value string) ([]infraCredentialsRoleBindingRecord, error) {
	if value == "" {
		return nil, nil
	}
	records := []infraCredentialsRoleBindingRecord{}
	if err := json.Unmarshal([]byte(value), &records); err != nil {
		return nil, fmt.Errorf("failed to parse pending infra CCM role binding records: %w", err)
	}
	for _, record := range records {
		if record.InfraNamespace == "" || record.RoleBindingName == "" {
			return nil, fmt.Errorf("invalid pending infra CCM role binding record in %q", value)
		}
	}
	return records, nil
}

func marshalInfraCredentialsPendingRecords(records []infraCredentialsRoleBindingRecord) (string, error) {
	if len(records) == 0 {
		return "", nil
	}
	data, err := json.Marshal(records)
	if err != nil {
		return "", fmt.Errorf("failed to encode pending infra CCM role binding records: %w", err)
	}
	return string(data), nil
}

func appendInfraCredentialsPendingRecord(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	record *infraCredentialsRoleBindingRecord,
) (bool, error) {
	records, err := parseInfraCredentialsPendingRecords(oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
	if err != nil {
		return false, err
	}
	for _, pendingRecord := range records {
		if pendingRecord.equal(record) {
			return false, nil
		}
	}
	if oacp.Annotations == nil {
		oacp.Annotations = map[string]string{}
	}
	value, err := marshalInfraCredentialsPendingRecords(append(records, *record))
	if err != nil {
		return false, err
	}
	oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation] = value
	return true, nil
}

func retainInfraCredentialsRoleBindingRecord(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	record *infraCredentialsRoleBindingRecord,
) error {
	appended, err := appendInfraCredentialsPendingRecord(oacp, record)
	if err != nil {
		return err
	}
	if !appended {
		return nil
	}
	if err := sourceClient.Update(ctx, oacp); err != nil {
		return fmt.Errorf("failed to retain pending infra CCM role binding record: %w", err)
	}
	return nil
}

func reconcilePendingInfraCredentialsRoleBindings(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	log := ctrl.LoggerFrom(ctx)
	records, err := parseInfraCredentialsPendingRecords(oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	remaining := make([]infraCredentialsRoleBindingRecord, 0, len(records))
	for index := range records {
		record := records[index]
		recordClient, err := infraClientForHistoricalRecord(ctx, sourceClient, oacp, &record)
		if err != nil {
			if !errors.Is(err, errInfraCredentialsTargetUnreachable) {
				return err
			}
			log.Info(
				"pending infra CCM role binding target is unreachable; retaining it for later cleanup",
				"infraClusterRef", record.InfraClusterRef,
				"error", err,
			)
			remaining = append(remaining, record)
			continue
		}
		if err := deleteInfraCredentialsRoleBinding(ctx, recordClient, &record); err != nil {
			if errors.Is(err, errInfraCredentialsDeleteUnauthorized) {
				log.Info(
					"pending infra CCM role binding cannot be deleted with the recorded credentials; retaining it",
					"infraClusterRef", record.InfraClusterRef,
					"error", err,
				)
				remaining = append(remaining, record)
				continue
			}
			return err
		}
	}

	if len(remaining) == len(records) {
		return nil
	}
	value, err := marshalInfraCredentialsPendingRecords(remaining)
	if err != nil {
		return err
	}
	if value == "" {
		delete(oacp.Annotations, infraCredentialsPendingRoleBindingsAnnotation)
	} else {
		if oacp.Annotations == nil {
			oacp.Annotations = map[string]string{}
		}
		oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation] = value
	}
	if err := sourceClient.Update(ctx, oacp); err != nil {
		return fmt.Errorf("failed to update pending infra CCM role binding records: %w", err)
	}
	return nil
}

func infraClientForRecord(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	record *infraCredentialsRoleBindingRecord,
	currentClient client.Client,
	allowTargetRotation bool,
) (client.Client, error) {
	currentRef := infraClusterRefForOACP(oacp)
	if err := validateInfraCredentialsRoleBindingRecord(oacp, record); err != nil {
		return nil, err
	}
	recordRef := record.InfraClusterRef
	if recordRef == "" {
		recordRef = localInfraClusterTarget
	}
	if !allowTargetRotation {
		if recordRef != currentRef {
			return nil, fmt.Errorf(
				"infra CCM role binding record reference %q does not match the current infra cluster %q",
				recordRef, currentRef,
			)
		}
		if currentRef == localInfraClusterTarget {
			if record.InfraClusterTarget != "" && record.InfraClusterTarget != localInfraClusterTarget {
				return nil, fmt.Errorf(
					"infra CCM role binding record target %q does not match the current local infra cluster",
					record.InfraClusterTarget,
				)
			}
			if currentClient == nil {
				return nil, fmt.Errorf("current infra cluster client is required for role binding cleanup")
			}
			return currentClient, nil
		}

		currentTarget, err := infraClusterTargetFromRef(ctx, sourceClient, oacp.Namespace, currentRef)
		if err != nil {
			return nil, err
		}
		if record.InfraClusterTarget != "" && record.InfraClusterTarget != currentTarget {
			return nil, fmt.Errorf(
				"infra cluster secret %s now identifies target %s, but role binding record identifies target %s: %w",
				strings.TrimPrefix(currentRef, "secret/"), currentTarget, record.InfraClusterTarget, errInfraCredentialsTargetUnreachable,
			)
		}
		if currentClient == nil {
			return nil, fmt.Errorf("current infra cluster client is required for role binding cleanup")
		}
		return currentClient, nil
	}

	if recordRef == currentRef {
		if currentClient == nil {
			return nil, fmt.Errorf("current infra cluster client is required for role binding rotation")
		}
		return currentClient, nil
	}

	return infraClientForHistoricalRecord(ctx, sourceClient, oacp, record)
}

func infraClientForHistoricalRecord(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	record *infraCredentialsRoleBindingRecord,
) (client.Client, error) {
	if err := validateInfraCredentialsRoleBindingRecord(oacp, record); err != nil {
		return nil, err
	}

	switch record.InfraClusterRef {
	case "", localInfraClusterTarget:
		if record.InfraClusterTarget != "" && record.InfraClusterTarget != localInfraClusterTarget {
			return nil, fmt.Errorf(
				"infra CCM role binding record target %q does not match its local infra cluster",
				record.InfraClusterTarget,
			)
		}
		return sourceClient, nil
	}

	if !strings.HasPrefix(record.InfraClusterRef, "secret/") {
		return nil, fmt.Errorf("invalid infra cluster reference %q in role binding record", record.InfraClusterRef)
	}
	secretName := strings.TrimPrefix(record.InfraClusterRef, "secret/")
	result, err := GetInfraClusterClientFromSecret(ctx, sourceClient, oacp.Namespace, secretName, sourceClient.Scheme())
	if err != nil {
		return nil, fmt.Errorf("failed to build recorded infra cluster client: %w", err)
	}
	if record.InfraClusterTarget != "" && result.Target != record.InfraClusterTarget {
		return nil, fmt.Errorf(
			"infra cluster secret %s now identifies target %s, but role binding record identifies target %s: %w",
			secretName, result.Target, record.InfraClusterTarget, errInfraCredentialsTargetUnreachable,
		)
	}
	return result.Client, nil
}

func validateInfraCredentialsRoleBindingRecord(
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	record *infraCredentialsRoleBindingRecord,
) error {
	if record.RoleBindingName != infraRoleBindingName(oacp) {
		return fmt.Errorf(
			"infra CCM role binding record names %q, but the expected RoleBinding for %s/%s is %q",
			record.RoleBindingName, oacp.Namespace, oacp.Name, infraRoleBindingName(oacp),
		)
	}
	return nil
}

func infraClusterRefForOACP(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) string {
	if oacp.Spec.Config.InfraClusterRef == nil {
		return localInfraClusterTarget
	}
	return "secret/" + oacp.Spec.Config.InfraClusterRef.Name
}

func infraClusterTargetFromRef(
	ctx context.Context,
	sourceClient client.Client,
	sourceNamespace string,
	infraClusterRef string,
) (string, error) {
	if infraClusterRef == localInfraClusterTarget {
		return localInfraClusterTarget, nil
	}
	secretName := strings.TrimPrefix(infraClusterRef, "secret/")
	secret := &corev1.Secret{}
	if err := sourceClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: sourceNamespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("infra cluster secret %s/%s is missing: %w", sourceNamespace, secretName, errInfraCredentialsTargetUnreachable)
		}
		return "", fmt.Errorf("failed to get infra cluster secret %s/%s: %w", sourceNamespace, secretName, err)
	}
	kubeconfigData := string(secret.Data["kubeconfig"])
	if kubeconfigData == "" {
		return "", fmt.Errorf("infra cluster secret %s/%s does not contain 'kubeconfig' key", sourceNamespace, secretName)
	}
	return kubeconfigClusterTarget(kubeconfigData)
}

func ensureInfraCredentialsRoleBindingRecord(
	ctx context.Context,
	sourceClient client.Client,
	infraClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	desired *infraCredentialsRoleBindingRecord,
) error {
	desiredValue, err := desired.marshal()
	if err != nil {
		return err
	}
	existingValue := oacp.Annotations[infraCredentialsRoleBindingAnnotation]
	if existingValue == desiredValue {
		return nil
	}

	if existingValue != "" {
		existing, err := parseInfraCredentialsRoleBindingRecord(existingValue)
		if err != nil {
			return err
		}
		if !existing.equal(desired) && !existing.legacyCompatible(desired) {
			existingClient, err := infraClientForRecord(ctx, sourceClient, oacp, existing, infraClient, true)
			if err != nil {
				if !errors.Is(err, errInfraCredentialsTargetUnreachable) {
					return err
				}
				ctrl.LoggerFrom(ctx).Info(
					"previous infra CCM role binding target is unreachable; retaining it for later cleanup",
					"infraClusterRef", existing.InfraClusterRef,
					"error", err,
				)
				if _, err := appendInfraCredentialsPendingRecord(oacp, existing); err != nil {
					return err
				}
			}
			if existingClient != nil {
				if err := deleteInfraCredentialsRoleBinding(ctx, existingClient, existing); err != nil {
					if errors.Is(err, errInfraCredentialsDeleteUnauthorized) {
						ctrl.LoggerFrom(ctx).Info(
							"previous infra CCM role binding cannot be deleted with the recorded credentials; retaining it",
							"infraClusterRef", existing.InfraClusterRef,
							"error", err,
						)
						if _, err := appendInfraCredentialsPendingRecord(oacp, existing); err != nil {
							return err
						}
					} else {
						return err
					}
				}
			}
		}
	}

	if oacp.Annotations == nil {
		oacp.Annotations = map[string]string{}
	}
	oacp.Annotations[infraCredentialsRoleBindingAnnotation] = desiredValue
	if err := sourceClient.Update(ctx, oacp); err != nil {
		return fmt.Errorf("failed to record infra CCM role binding: %w", err)
	}
	return nil
}

func clearInfraCredentialsRoleBindingAnnotation(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	if _, exists := oacp.Annotations[infraCredentialsRoleBindingAnnotation]; !exists {
		return nil
	}
	delete(oacp.Annotations, infraCredentialsRoleBindingAnnotation)
	if err := sourceClient.Update(ctx, oacp); err != nil {
		return fmt.Errorf("failed to clear infra CCM role binding record: %w", err)
	}
	return nil
}

func clearInfraCredentialsRoleBindingAnnotations(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
) error {
	_, hasCurrentRecord := oacp.Annotations[infraCredentialsRoleBindingAnnotation]
	_, hasPendingRecords := oacp.Annotations[infraCredentialsPendingRoleBindingsAnnotation]
	if !hasCurrentRecord && !hasPendingRecords {
		return nil
	}
	delete(oacp.Annotations, infraCredentialsRoleBindingAnnotation)
	delete(oacp.Annotations, infraCredentialsPendingRoleBindingsAnnotation)
	if err := sourceClient.Update(ctx, oacp); err != nil {
		return fmt.Errorf("failed to clear infra CCM role binding records: %w", err)
	}
	return nil
}

func deleteInfraCredentialsRoleBinding(
	ctx context.Context,
	infraClient client.Client,
	record *infraCredentialsRoleBindingRecord,
) error {
	if infraClient == nil {
		return fmt.Errorf("infra cluster client is required for CCM credentials cleanup")
	}
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      record.RoleBindingName,
			Namespace: record.InfraNamespace,
		},
	}
	if err := infraClient.Delete(ctx, roleBinding); client.IgnoreNotFound(err) != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return fmt.Errorf("%w: %v", errInfraCredentialsDeleteUnauthorized, err)
		}
		return fmt.Errorf("failed to delete infra CCM role binding: %w", err)
	}
	return nil
}

func infraRoleBindingName(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) string {
	digest := sha256.Sum256([]byte(oacp.Namespace + "\x00" + oacp.Name))
	return fmt.Sprintf("%s-%s", ccmInfraRoleName, hex.EncodeToString(digest[:]))
}

func kubeconfigClusterTarget(kubeconfigData string) (string, error) {
	kubeconfig, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return "", fmt.Errorf("failed to parse infra credentials kubeconfig: %w", err)
	}
	currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
	if !ok {
		return "", fmt.Errorf("infra credentials kubeconfig has no current context")
	}
	cluster, ok := kubeconfig.Clusters[currentContext.Cluster]
	if !ok {
		return "", fmt.Errorf("infra credentials kubeconfig has no cluster for current context")
	}

	digest := sha256.Sum256([]byte(cluster.Server + "\x00" + string(cluster.CertificateAuthorityData)))
	return hex.EncodeToString(digest[:]), nil
}

func kubeconfigSubject(ctx context.Context, kubeconfigData string) (rbacv1.Subject, error) {
	kubeconfig, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return rbacv1.Subject{}, fmt.Errorf("failed to parse infra credentials kubeconfig: %w", err)
	}

	currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
	if !ok {
		return rbacv1.Subject{}, fmt.Errorf("infra credentials kubeconfig has no current context")
	}
	user, ok := kubeconfig.AuthInfos[currentContext.AuthInfo]
	if !ok {
		return rbacv1.Subject{}, fmt.Errorf("infra credentials kubeconfig has no user for current context")
	}

	identity := ""
	switch {
	case user.Token != "", len(user.ClientCertificateData) > 0:
		identity, err = selfSubjectReview(ctx, kubeconfigData)
		if err != nil {
			return rbacv1.Subject{}, err
		}
	default:
		return rbacv1.Subject{}, fmt.Errorf("infra credentials kubeconfig has no supported identity credentials")
	}

	if identity == "" {
		return rbacv1.Subject{}, fmt.Errorf("infra credentials kubeconfig has an empty identity")
	}
	if serviceAccountNamespace, serviceAccountName, ok := serviceAccountIdentity(identity); ok {
		return rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      serviceAccountName,
			Namespace: serviceAccountNamespace,
		}, nil
	}

	return rbacv1.Subject{
		Kind:     rbacv1.UserKind,
		APIGroup: rbacv1.GroupName,
		Name:     identity,
	}, nil
}

func selfSubjectReview(ctx context.Context, kubeconfigData string) (string, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		return "", fmt.Errorf("failed to create infra credentials identity client: %w", err)
	}
	restConfig.Timeout = 10 * time.Second
	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create infra credentials identity client: %w", err)
	}
	review, err := clientSet.AuthenticationV1().SelfSubjectReviews().Create(
		ctx,
		&authenticationv1.SelfSubjectReview{},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to resolve infra credentials identity: %w", err)
	}
	username := review.Status.UserInfo.Username
	if username == "" {
		return "", fmt.Errorf("infra credentials identity response has no username")
	}
	return username, nil
}

func serviceAccountIdentity(identity string) (namespace string, name string, ok bool) {
	parts := strings.Split(identity, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func GetInfraKubernetesServiceIP(ctx context.Context, infraClient client.Client) (string, error) {
	if infraClient == nil {
		return "", fmt.Errorf("infra cluster client is required for pod-networking credentials")
	}

	kubernetesService := &corev1.Service{}
	if err := infraClient.Get(ctx, client.ObjectKey{Name: "kubernetes", Namespace: "default"}, kubernetesService); err != nil {
		return "", fmt.Errorf("failed to get infra cluster Kubernetes service: %w", err)
	}
	if kubernetesService.Spec.ClusterIP == "" || kubernetesService.Spec.ClusterIP == "None" {
		return "", fmt.Errorf("infra cluster Kubernetes service has no ClusterIP")
	}

	return kubernetesService.Spec.ClusterIP, nil
}

func rewriteKubeconfigForInfraService(kubeconfigData, infraAPIIP string) (string, error) {
	kubeconfig, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return "", fmt.Errorf("failed to parse infra credentials kubeconfig: %w", err)
	}

	currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
	if !ok {
		return "", fmt.Errorf("infra credentials kubeconfig has no current context")
	}
	cluster, ok := kubeconfig.Clusters[currentContext.Cluster]
	if !ok {
		return "", fmt.Errorf("infra credentials kubeconfig has no cluster for current context")
	}

	serverURL, err := url.Parse(cluster.Server)
	if err != nil || serverURL.Hostname() == "" {
		return "", fmt.Errorf("infra credentials kubeconfig has an invalid server URL %q", cluster.Server)
	}

	if cluster.TLSServerName == "" {
		cluster.TLSServerName = serverURL.Hostname()
	}
	cluster.Server = fmt.Sprintf("https://%s", net.JoinHostPort(infraAPIIP, "443"))

	rewritten, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return "", fmt.Errorf("failed to write infra credentials kubeconfig: %w", err)
	}
	return string(rewritten), nil
}

func ensureInfraCredentialsTargetCurrent(
	ctx context.Context,
	sourceClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	infraResult *InfraClientResult,
) error {
	if infraResult == nil || infraResult.Client == nil {
		return fmt.Errorf("infra cluster client is required for CCM credentials")
	}

	currentRef := infraClusterRefForOACP(oacp)
	if currentRef != infraResult.InfraClusterRef {
		return fmt.Errorf("%w: selected %q, current %q", errInfraCredentialsTargetChanged, infraResult.InfraClusterRef, currentRef)
	}
	if currentRef == localInfraClusterTarget {
		if infraResult.Target != localInfraClusterTarget {
			return fmt.Errorf("%w: selected target %q", errInfraCredentialsTargetChanged, infraResult.Target)
		}
		return nil
	}

	currentResult, err := GetInfraClusterClientFromSecret(
		ctx,
		sourceClient,
		oacp.Namespace,
		strings.TrimPrefix(currentRef, "secret/"),
		sourceClient.Scheme(),
	)
	if err != nil {
		return err
	}
	if currentResult.Target != infraResult.Target || currentResult.Namespace != infraResult.Namespace {
		return fmt.Errorf(
			"%w: selected target %q in namespace %q, current target %q in namespace %q",
			errInfraCredentialsTargetChanged, infraResult.Target, infraResult.Namespace, currentResult.Target, currentResult.Namespace,
		)
	}
	return nil
}
