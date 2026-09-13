package podman_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestNetworkHelperSearchUsesDeclaredDirectoriesAndFallbackPolicy(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"paths", "special file first", "empty directories", "denied directory",
		networkTestNonExecutable, "explicit slirp",
		"unknown defaults", "inherited defaults", "bindir", networkTestEngineEnvironment, networkTestWrongScope,
		"partial source", "cancelled empty", "candidate limit"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			text := "[engine]\nhelper_binaries_dir=['/helpers','/next']"
			for _, role := range []string{networkTestBackend, "aardvark-dns", networkTestPasta, networkTestSlirp} {
				for _, dir := range []string{"/helpers", "/next", "/usr/bin"} {
					name := dir + "/" + role
					addEngineFile(mem, name, "synthetic helper")
					mem.AddFile(name, []byte("synthetic helper"), 0o755)
					if err := mem.SetExecutableAccess(name, true, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			switch scenario {
			case "empty directories", "cancelled empty":
				text = "[engine]\nhelper_binaries_dir=[]"
			case "unknown defaults":
				text = ""
			case "inherited defaults":
				text = "[engine]\nhelper_binaries_dir=['/helpers',{append=true}]"
			case "bindir":
				text = "[engine]\nhelper_binaries_dir=['$BINDIR/helpers']"
			case networkTestEngineEnvironment:
				text += "\nenv=['SECRET=value']"
			case "candidate limit":
				text = "[engine]\nhelper_binaries_dir=[" + strings.Repeat("'/missing',", 17) + "]"
			case "explicit slirp":
				text = "[engine]\nnetwork_cmd_path='/next/slirp4netns'"
			case "special file first":
				for _, role := range []string{networkTestBackend, "aardvark-dns", networkTestPasta, networkTestSlirp} {
					if err := mem.AddSpecial("/helpers/"+role, fs.ModeNamedPipe|0o755); err != nil {
						t.Fatal(err)
					}
				}
			case "denied directory":
				mem.AddError("/helpers", fs.ErrPermission)
			case networkTestNonExecutable:
				for _, role := range []string{networkTestBackend, "aardvark-dns", networkTestPasta, networkTestSlirp} {
					if err := mem.SetExecutableAccess("/helpers/"+role, false, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			addEngineFile(mem, engineSystemPath, text)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			_, source, err := engineProbe(t, testVersion, 1001).Collect(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch scenario {
			case networkTestWrongScope:
				source.Scope.ContextID = "other"
			case "partial source":
				source.Completeness = model.Partial
			case "cancelled empty":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			probes := podman.NetworkHelperProbes(source, nil)
			if len(probes) != 4 {
				t.Fatal("missing network helper roles")
			}
			for _, p := range probes {
				obs, _ := p.Run(ctx, env)
				role := p.Role
				binary := role
				if role == "aardvark_dns" {
					binary = "aardvark-dns"
				}
				want, partial := "/helpers/"+binary, false
				switch scenario {
				case "empty directories":
					want = ""
					if role == networkTestPasta || role == networkTestSlirp {
						want = "/usr/bin/" + binary
					}
				case networkTestNonExecutable, "denied directory":
					want = "/next/" + binary
					partial = scenario == "denied directory"
				case "explicit slirp":
					want, partial = "", true
					if role == networkTestSlirp {
						want, partial = "/next/slirp4netns", false
					}
				case "special file first", "unknown defaults", "inherited defaults", "bindir", networkTestEngineEnvironment,
					networkTestWrongScope, "partial source", "cancelled empty", "candidate limit":
					want, partial = "", true
				}
				if obs.Executable.SelectedPath != want || (obs.Completeness == model.Partial) != partial {
					t.Fatalf("%s path=%s completeness=%s; want=%s partial=%t", role, obs.Executable.SelectedPath, obs.Completeness, want, partial)
				}
				if len(obs.Executable.Candidates) > 16 {
					t.Fatal("unbounded helper search")
				}
			}
		})
	}
}
