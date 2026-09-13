package platform

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestTargetRequestValidation(t *testing.T) {
	t.Parallel()
	valid := TargetRequest{Version: 1, RunID: "worker-run", ContextID: "uid:1000",
		Target:     model.UserIdentity{UID: 1000, GID: 1000, GroupsKnown: true},
		Namespaces: []model.Namespace{{Kind: "user", ID: "user:[1]"}}, Payload: []byte(`{}`)}
	for _, name := range []string{"valid", "version", "run", "context", "groups", "namespace", "payload"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := valid
			switch name {
			case "version":
				request.Version = 0
			case "run":
				request.RunID = ""
			case "context":
				request.ContextID = ""
			case "groups":
				request.Target.GroupsKnown = false
			case "namespace":
				request.Namespaces = []model.Namespace{{Kind: "../self", ID: "bad"}}
			case "payload":
				request.Payload = nil
			}
			if err := request.validate(); (err == nil) != (name == "valid") {
				t.Fatal(err)
			}
		})
	}
}

func TestTargetBootstrapFailureBoundaries(t *testing.T) {
	t.Parallel()
	request := TargetRequest{Version: 1, RunID: "worker-run", ContextID: "uid:1000",
		Target: model.UserIdentity{UID: 1000, GID: 100, GroupsKnown: true,
			SupplementaryGroups: []uint32{100, 200}}, Namespaces: []model.Namespace{{Kind: "user", ID: "user:[1]"}}, Payload: []byte(`{}`)}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"complete bootstrap", "close", "read", "decode", "credentials", "nonroot", "groups", "gid", "uid",
		"verify"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			fail := func(stage string) error {
				calls = append(calls, stage)
				if name == stage {
					return errors.New("failure")
				}
				return nil
			}
			ops := targetBootstrap{
				closeExecutable: func() error { return fail("close") },
				credentials: func() (model.CurrentCredentials, error) {
					c := model.CurrentCredentials{GroupsKnown: true}
					if name == "nonroot" {
						c.UID = 1
						c.EUID = 1
					}
					return c, fail("credentials")
				},
				setgroups: func(groups []int) error {
					if !reflect.DeepEqual(groups, []int{100, 200}) {
						t.Fatal(groups)
					}
					return fail("groups")
				},
				setresgid: func(r, e, s int) error {
					if r != 100 || e != 100 || s != 100 {
						t.Fatal("gid")
					}
					return fail("gid")
				},
				setresuid: func(r, e, s int) error {
					if r != 1000 || e != 1000 || s != 1000 {
						t.Fatal("uid")
					}
					return fail("uid")
				},
				verify: func(_ context.Context, r TargetRequest) error {
					if !reflect.DeepEqual(r, request) {
						t.Fatal("request changed")
					}
					return fail("verify")
				},
			}
			var input io.Reader = strings.NewReader(string(data))
			if name == "read" {
				input = failingTargetReader{}
			}
			if name == "decode" {
				input = strings.NewReader("null")
			}
			_, err := bootstrapTarget(t.Context(), input, ops)
			if (err == nil) != (name == "complete bootstrap") {
				t.Fatal(err)
			}
			if name == "complete bootstrap" && !reflect.DeepEqual(calls, []string{"close", "credentials", "groups", "gid", "uid",
				"verify"}) {
				t.Fatal(calls)
			}
			if name != "complete bootstrap" {
				for i, stage := range calls {
					if stage == name && i != len(calls)-1 {
						t.Fatalf("continued after %s", stage)
					}
				}
			}
		})
	}
}

type failingTargetReader struct{}

func (failingTargetReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestTargetStatusAndNamespaceVerification(t *testing.T) {
	t.Parallel()
	const status = "Uid:\t1000 1000 1000 1000\nGid:\t100 100 100 100\nCapInh:\t0\nCapPrm:\t0\nCapEff:\t0\nCapAmb:\t0\n"
	target := model.UserIdentity{UID: 1000, GID: 100, GroupsKnown: true}
	for _, data := range []string{status, strings.Replace(status, "1000 1000 1000 1000", "1000 1000 0 1000", 1),
		strings.Replace(status, "CapEff:\t0", "CapEff:\t1", 1), strings.Replace(status, "CapInh:\t0", "CapInh:\tbad!", 1),
		strings.Replace(status, "Uid:\t1000 1000 1000 1000", "Uid:\tbad bad bad bad", 1),
		strings.Replace(status, "Uid:\t1000 1000 1000 1000", "Uid:\t1000", 1), status + "CapEff:\t0\n", ""} {
		if err := verifyTargetStatus([]byte(data), target); (err == nil) != (data == status) {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"valid", "namespace", "missing status", "missing namespace"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mem := NewMemPlatformReader()
			for _, dir := range []string{"/proc", "/proc/self", "/proc/self/ns"} {
				mem.AddDir(dir, 0o755)
			}
			if name != "missing status" {
				mem.AddFile("/proc/self/status", []byte(status), 0o644)
			}
			if name != "missing namespace" {
				mem.AddSymlink("/proc/self/ns/user", "user:[1]")
			}
			files := NewScopedMemReader("/", mem)
			defer files.Close()
			r := TargetRequest{Target: target, Namespaces: []model.Namespace{{Kind: "user", ID: "user:[1]"}}}
			if name == "namespace" {
				r.Namespaces[0].ID = "user:[2]"
			}
			if err := verifyTargetFiles(t.Context(), files, r); (err == nil) != (name == "valid") {
				t.Fatal(err)
			}
		})
	}
}

func TestTargetDecodeAndCurrentVerification(t *testing.T) {
	t.Parallel()
	for _, data := range []string{"null", `{"version":1}`, strings.Repeat(" ", TargetMessageBytes+1),
		`{"version":1,"run_id":"a","context_id":"b","target":{"groups_known":true},` +
			`"namespaces":[{"kind":"user","id":"user:[1]"}],"payload":"e30="}`} {
		if _, err := decodeTarget([]byte(data)); err == nil {
			t.Fatal("accepted incomplete request")
		}
	}
	c, err := CurrentCredentials()
	if err != nil {
		t.Fatal(err)
	}
	request := TargetRequest{Target: model.UserIdentity{UID: c.EUID, GID: c.EGID, GroupsKnown: c.GroupsKnown, SupplementaryGroups: c.Groups}}
	files, err := NewScopedOSReader("/")
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	status, err := files.ReadFile(t.Context(), "proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	want := verifyTargetStatus(status, request.Target)
	if err := verifyTarget(t.Context(), request); (err == nil) != (want == nil) {
		t.Fatal(err)
	}
	request.Target.UID++
	if err := verifyTarget(t.Context(), request); err == nil {
		t.Fatal("mismatched credentials")
	}
	if _, err := RunTarget(t.Context(), TargetRequest{}); err == nil {
		t.Fatal("invalid request")
	}
	if c.EUID != 0 {
		request.Version = 1
		request.RunID = "run"
		request.ContextID = "ctx"
		request.Payload = []byte(`{}`)
		request.Namespaces = []model.Namespace{{Kind: "user", ID: fmt.Sprintf("user:[%d]", 1)}}
		if _, err := RunTarget(t.Context(), request); !errors.Is(err, ErrTargetCredentials) {
			t.Fatal(err)
		}
	}
}
