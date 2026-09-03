//go:build e2ev2 && backuprestore

package backuprestore

import (
	"context"
	"fmt"
	"strings"
	"time"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/test/e2e/v2/internal"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// HCPEtcdBackupNamePrefix is the prefix used by the OADP plugin when creating
	// HCPEtcdBackup resources. The full name follows the pattern: oadp-<BackupName>-<random>.
	HCPEtcdBackupNamePrefix = "oadp-"

	// RestoreMarkerNamespace is the hosted-cluster namespace where the restore marker
	// ConfigMap is created. "default" always exists and its contents are captured by the
	// etcd snapshot.
	RestoreMarkerNamespace = "default"
	// restoreMarkerDataKey is the ConfigMap data key holding the unique marker value.
	restoreMarkerDataKey = "restoreMarker"
)

// MatchesHCPEtcdBackupName checks whether an HCPEtcdBackup resource name matches the
// expected naming pattern for a given OADP backup name. The OADP plugin creates
// HCPEtcdBackup resources with the naming pattern: oadp-<BackupName>-<random>.
func MatchesHCPEtcdBackupName(hcpEtcdBackupName, oadpBackupName string) bool {
	return strings.HasPrefix(hcpEtcdBackupName, HCPEtcdBackupNamePrefix+oadpBackupName+"-")
}

// WaitForHCPEtcdBackupCondition waits for an HCPEtcdBackup resource matching the given
// OADP backup name to have a BackupCompleted condition with the specified status.
// HCPEtcdBackup names follow the pattern: oadp-<BackupName>-<random>.
func WaitForHCPEtcdBackupCondition(testCtx *internal.TestContext, backupName string, expectedStatus metav1.ConditionStatus) error {
	return wait.PollUntilContextTimeout(testCtx.Context, PollInterval, BackupTimeout, true, func(ctx context.Context) (bool, error) {
		hcpEtcdBackupList := &hyperv1.HCPEtcdBackupList{}
		if err := testCtx.MgmtClient.List(ctx, hcpEtcdBackupList, crclient.InNamespace(testCtx.ControlPlaneNamespace)); err != nil {
			return false, fmt.Errorf("failed to list HCPEtcdBackup resources: %w", err)
		}

		for _, backup := range hcpEtcdBackupList.Items {
			if !MatchesHCPEtcdBackupName(backup.Name, backupName) {
				continue
			}
			condition := meta.FindStatusCondition(backup.Status.Conditions, string(hyperv1.BackupCompleted))
			if condition == nil {
				return false, nil
			}
			if condition.Status == expectedStatus {
				return true, nil
			}
			// If the condition is explicitly False, the backup failed - stop polling.
			if expectedStatus == metav1.ConditionTrue && condition.Status == metav1.ConditionFalse {
				return false, fmt.Errorf("HCPEtcdBackup %s has BackupCompleted=False: reason=%s, message=%s",
					backup.Name, condition.Reason, condition.Message)
			}
			return false, nil
		}
		return false, nil
	})
}

// RestoreMarkerName returns the name of the restore marker ConfigMap for a cluster.
func RestoreMarkerName(clusterName string) string {
	return fmt.Sprintf("etcd-restore-marker-%s", clusterName)
}

// SeedRestoreMarker creates (or updates) a ConfigMap in the hosted cluster carrying a
// unique value and returns that value. It must be called before the etcd snapshot backup
// is taken so the marker is captured in the snapshot.
//
// After a break-and-restore cycle the entire control plane (including etcd) is destroyed
// and rebuilt, so the marker can only reappear if the etcd snapshot was actually restored.
// Verifying it post-restore therefore proves the restore was not skipped (e.g. via the
// split-brain / "data directory not empty" path). ConfigMap writes are synchronous to
// etcd, so the marker is durable once this returns.
func SeedRestoreMarker(ctx context.Context, hostedClusterClient crclient.Client, clusterName string) (string, error) {
	value := fmt.Sprintf("restore-marker-%d", time.Now().UnixNano())
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RestoreMarkerName(clusterName),
			Namespace: RestoreMarkerNamespace,
		},
		Data: map[string]string{restoreMarkerDataKey: value},
	}
	if err := hostedClusterClient.Create(ctx, cm); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("failed to create restore marker ConfigMap %s/%s: %w", RestoreMarkerNamespace, cm.Name, err)
		}
		existing := &corev1.ConfigMap{}
		if err := hostedClusterClient.Get(ctx, crclient.ObjectKeyFromObject(cm), existing); err != nil {
			return "", fmt.Errorf("failed to get existing restore marker ConfigMap %s/%s: %w", RestoreMarkerNamespace, cm.Name, err)
		}
		existing.Data = cm.Data
		if err := hostedClusterClient.Update(ctx, existing); err != nil {
			return "", fmt.Errorf("failed to update restore marker ConfigMap %s/%s: %w", RestoreMarkerNamespace, cm.Name, err)
		}
	}
	return value, nil
}

// VerifyRestoreMarker fetches the restore marker ConfigMap from the (restored) hosted
// cluster and verifies its value matches expectedValue. A missing marker or a mismatched
// value indicates the etcd snapshot was not restored (the datastore is fresh/empty),
// which is the failure mode this check is designed to catch.
func VerifyRestoreMarker(ctx context.Context, hostedClusterClient crclient.Client, clusterName, expectedValue string) error {
	cm := &corev1.ConfigMap{}
	key := crclient.ObjectKey{Namespace: RestoreMarkerNamespace, Name: RestoreMarkerName(clusterName)}
	if err := hostedClusterClient.Get(ctx, key, cm); err != nil {
		return fmt.Errorf("failed to get restore marker ConfigMap %s/%s: %w", key.Namespace, key.Name, err)
	}
	if got := cm.Data[restoreMarkerDataKey]; got != expectedValue {
		return fmt.Errorf("restore marker value mismatch for %s/%s: expected %q, got %q; etcd snapshot may not have been restored", key.Namespace, key.Name, expectedValue, got)
	}
	return nil
}
