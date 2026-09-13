package host

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestPassivePrerequisites(t *testing.T) {
	t.Parallel()
	env := topologyEnvironment(t, map[string]string{
		"/proc/filesystems": "nodev\toverlay\nnodev\tfuse\next4\n",
		"/proc/self/net/protocols": "protocol size sockets memory press maxhdr slab module\n" +
			"TCP 1 0 0 no 0 yes kernel\nUDP 1 0 0 no 0 yes kernel\n",
		"/etc/resolv.conf": "nameserver 127.0.0.53\nsearch example.test\noptions ndots:2 rotate\n",
	}, nil)
	filesystem, _ := (FilesystemProbe{Now: testClock}).Run(t.Context(), env)
	if filesystem.Host.Filesystems.OverlayRegistered == nil ||
		!*filesystem.Host.Filesystems.OverlayRegistered {
		t.Fatal("overlay fact missing")
	}
	network, _ := (NetworkProbe{Now: testClock}).Run(t.Context(), env)
	if network.Host.Network.IPv4TCP == nil || !*network.Host.Network.IPv4TCP ||
		network.Host.Network.IPv6TCP == nil || *network.Host.Network.IPv6TCP {
		t.Fatal("protocol registration mismatch")
	}
	resolver, _ := (ResolverProbe{Now: testClock}).Run(t.Context(), env)
	if resolver.Completeness != model.Complete || len(resolver.Host.Resolver.Nameservers) != 1 ||
		resolver.Host.Resolver.Search[0] != "example.test" {
		t.Fatalf("resolver: %+v", resolver)
	}
}

func TestResolverMalformedAndMissing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text    string
		partial bool
	}{
		{"nameserver 2001:db8::1\ndomain example.test\nsearch newer.test other.test\noptions attempts:2 timeout:3 no-aaaa\n", false},
		{"search older.test\ndomain example.test\n", false},
		{"# comment\n; comment\n", false}, {"nameserver invalid\n", true}, {"nameserver 1.1.1.1 extra\n", true},
		{"search bad/name\n", true}, {"domain two names\n", true}, {"search\n", true}, {"unknown value\n", true},
		{"options unknown:value\n", true}, {"options ndots:invalid\n", true}, {"options invalid\n", true},
	} {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			env := topologyEnvironment(t, map[string]string{"/etc/resolv.conf": tt.text}, nil)
			obs, _ := (ResolverProbe{Now: testClock}).Run(t.Context(), env)
			if (obs.Completeness != model.Complete) != tt.partial {
				t.Fatal(obs)
			}
		})
	}
	env := topologyEnvironment(t, nil, nil)
	obs, _ := (ResolverProbe{Now: testClock}).Run(t.Context(), env)
	if obs.Host.Resolver.Present == nil || *obs.Host.Resolver.Present || obs.Completeness != model.Complete {
		t.Fatal("absence is not failure")
	}
	for _, domain := range []string{strings.Repeat("x", 254), strings.Repeat("x", 64) + ".test", "empty..test"} {
		if resolverDomain(domain) {
			t.Fatal("invalid domain accepted")
		}
	}
}

func TestPrerequisitesIncompleteObservations(t *testing.T) {
	t.Parallel()
	env := topologyEnvironment(t, map[string]string{
		"/proc/filesystems":        "nodev overlay\nmalformed row text\n",
		"/proc/self/net/protocols": "protocol size sockets memory press maxhdr slab module\nTCP 1 0 0 no 0 yes kernel\nmalformed\n",
	}, nil)
	filesystem, _ := (FilesystemProbe{Now: testClock}).Run(t.Context(), env)
	if filesystem.Completeness != model.Partial || filesystem.Host.Filesystems.FUSERegistered != nil ||
		!*filesystem.Host.Filesystems.OverlayRegistered {
		t.Fatal("partial filesystem list became false")
	}
	network, _ := (NetworkProbe{Now: testClock}).Run(t.Context(), env)
	if network.Completeness != model.Partial || network.Host.Network.IPv6UDP != nil || !*network.Host.Network.IPv4TCP {
		t.Fatal("partial protocol list became false")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, p := range Probes(testClock) {
		if _, err := p.Run(ctx, env); err == nil {
			t.Fatal("cancellation lost", p.ID())
		}
	}
}

func TestResolverUnreadableAndSymlinkLoop(t *testing.T) {
	t.Parallel()
	for _, loop := range []bool{false, true} {
		t.Run(map[bool]string{false: "permission", true: "symlink loop"}[loop], func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/etc", 0o755)
			if loop {
				mem.AddSymlink("/etc/resolv.conf", "resolv.conf")
			} else {
				mem.AddError("/etc/resolv.conf", fs.ErrPermission)
			}
			files := platform.NewScopedMemReader("/", mem)
			t.Cleanup(func() { files.Close() })
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files)
			obs, err := (ResolverProbe{Now: testClock}).Run(t.Context(), env)
			if err != nil || obs.Completeness != model.Partial || obs.Host.Resolver.Present != nil ||
				len(obs.Host.Resolver.Nameservers) != 0 {
				t.Fatalf("unreadable resolver became known: %+v %v", obs, err)
			}
		})
	}
}

func TestNetworkIPv6Controls(t *testing.T) {
	t.Parallel()
	env := topologyEnvironment(t, map[string]string{
		"/proc/self/net/protocols": "protocol size sockets memory press maxhdr slab module\n" +
			"TCPv6 1 0 0 no 0 yes kernel\nUDPv6 1 0 0 no 0 yes kernel\n",
		"/proc/sys/net/ipv6/conf/all/disable_ipv6":     "0\n",
		"/proc/sys/net/ipv6/conf/default/disable_ipv6": "1\n",
	}, nil)
	obs, err := (NetworkProbe{Now: testClock}).Run(t.Context(), env)
	network := obs.Host.Network
	if err != nil || obs.Completeness != model.Complete || !*network.IPv6TCP || !*network.IPv6UDP ||
		*network.IPv6AllDisabled || !*network.IPv6DefaultDisabled {
		t.Fatalf("IPv6 registration and controls were conflated: %+v %v", network, err)
	}
}
