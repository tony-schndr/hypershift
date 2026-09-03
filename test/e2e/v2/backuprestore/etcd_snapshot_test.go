//go:build e2ev2 && backuprestore

package backuprestore

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestMatchesHCPEtcdBackupName(t *testing.T) {
	tests := []struct {
		name              string
		hcpEtcdBackupName string
		oadpBackupName    string
		expectedMatch     bool
	}{
		{
			name:              "When HCPEtcdBackup name matches the oadp pattern it should return true",
			hcpEtcdBackupName: "oadp-mycluster-mynamespace-abc123-xyz78",
			oadpBackupName:    "mycluster-mynamespace-abc123",
			expectedMatch:     true,
		},
		{
			name:              "When HCPEtcdBackup name does not match it should return false",
			hcpEtcdBackupName: "some-other-backup",
			oadpBackupName:    "mycluster-mynamespace-abc123",
			expectedMatch:     false,
		},
		{
			name:              "When HCPEtcdBackup name is the exact backup name without prefix it should return false",
			hcpEtcdBackupName: "mycluster-mynamespace-abc123",
			oadpBackupName:    "mycluster-mynamespace-abc123",
			expectedMatch:     false,
		},
		{
			name:              "When HCPEtcdBackup name only shares a backup name prefix it should return false",
			hcpEtcdBackupName: "oadp-mycluster-mynamespace-abc1234-xyz78",
			oadpBackupName:    "mycluster-mynamespace-abc123",
			expectedMatch:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(MatchesHCPEtcdBackupName(tt.hcpEtcdBackupName, tt.oadpBackupName)).To(Equal(tt.expectedMatch))
		})
	}
}
