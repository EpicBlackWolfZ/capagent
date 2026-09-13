package probe

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"testing"
)

const snapshotController = "cpu"
const snapshotChanged = "changed"

func TestHostSnapshotOwnsNestedValues(t *testing.T) {
	t.Parallel()
	boolean := true
	mode := uint32(2)
	maximum := uint64(1024)
	host := &model.HostObservation{
		OS: &model.OSObservation{IDLike: []string{"rhel"}}, Kernel: &model.KernelObservation{Parts: &model.KernelVersion{Major: 6}},
		Systemd: &model.SystemdObservation{Running: &boolean, Installed: &boolean, UtilityInstalled: &boolean,
			RuntimeDirectory: &boolean, Accessible: &boolean},
		Cgroups: &model.CgroupObservation{Memberships: []model.CgroupMembership{{Controllers: []string{snapshotController}}},
			Mounts: []model.CgroupMount{{Controllers: []string{snapshotController}, SubtreeControl: []string{snapshotController}}}},
		Namespaces: &model.NamespaceObservation{Namespaces: []model.Namespace{{Kind: "user", ID: "user:[42]"}}},
		Security: &model.SecurityObservation{LSMs: []string{"selinux"},
			AppArmorProfiles: []model.AppArmorProfile{{Name: "profile", Mode: "enforce"}},
			SeccompActions:   []string{"allow"}, AppArmorEnabled: &boolean, SeccompMode: &mode, NoNewPrivileges: &boolean,
			UnprivilegedUserNSClone: &boolean, MaxUserNamespaces: &maximum},
	}
	copy := SnapshotObservation(model.Observation{Host: host}).Host
	host.OS.IDLike[0] = snapshotChanged
	host.Kernel.Parts.Major = 1
	boolean = false
	mode = 0
	maximum = 0
	host.Cgroups.Memberships[0].Controllers[0] = snapshotChanged
	host.Cgroups.Mounts[0].Controllers[0] = snapshotChanged
	host.Cgroups.Mounts[0].SubtreeControl[0] = snapshotChanged
	host.Namespaces.Namespaces[0].ID = snapshotChanged
	host.Security.LSMs[0] = snapshotChanged
	host.Security.AppArmorProfiles[0].Name = snapshotChanged
	host.Security.SeccompActions[0] = snapshotChanged
	if copy.OS.IDLike[0] != "rhel" || copy.Kernel.Parts.Major != 6 || !*copy.Systemd.Running || *copy.Security.SeccompMode != 2 ||
		*copy.Security.MaxUserNamespaces != 1024 || copy.Security.LSMs[0] != "selinux" || copy.Security.AppArmorProfiles[0].Name != "profile" ||
		copy.Security.SeccompActions[0] != "allow" || copy.Cgroups.Memberships[0].Controllers[0] != snapshotController ||
		copy.Cgroups.Mounts[0].Controllers[0] != snapshotController || copy.Cgroups.Mounts[0].SubtreeControl[0] != snapshotController ||
		copy.Namespaces.Namespaces[0].ID != "user:[42]" {
		t.Fatal("host measurement aliased its producer")
	}
}
