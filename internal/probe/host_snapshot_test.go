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

func TestHostPrerequisiteSnapshot(t *testing.T) {
	t.Parallel()
	present := true
	original := model.Observation{Host: &model.HostObservation{
		Filesystems: &model.FilesystemObservation{Registered: []model.FilesystemRegistration{{Name: "overlay"}},
			OverlayRegistered: &present, FUSERegistered: &present},
		Network: &model.NetworkObservation{Protocols: []string{"TCP"}, IPv4TCP: &present, IPv4UDP: &present, IPv6TCP: &present,
			IPv6UDP: &present, IPv6AllDisabled: &present, IPv6DefaultDisabled: &present},
		Resolver: &model.ResolverObservation{Present: &present, Nameservers: []string{"192.0.2.53"},
			Search: []string{"example.test"}, Options: []string{"rotate"}},
	}}
	retained := SnapshotObservation(original).Host
	present = false
	original.Host.Filesystems.Registered[0].Name = snapshotChanged
	original.Host.Network.Protocols[0] = snapshotChanged
	original.Host.Resolver.Nameservers[0] = snapshotChanged
	original.Host.Resolver.Search[0] = snapshotChanged
	original.Host.Resolver.Options[0] = snapshotChanged
	if !*retained.Filesystems.OverlayRegistered || retained.Filesystems.Registered[0].Name != "overlay" ||
		!*retained.Network.IPv6DefaultDisabled || retained.Network.Protocols[0] != "TCP" || !*retained.Resolver.Present ||
		retained.Resolver.Nameservers[0] != "192.0.2.53" || retained.Resolver.Search[0] != "example.test" ||
		retained.Resolver.Options[0] != "rotate" {
		t.Fatal("prerequisite snapshot aliased its producer")
	}
}
