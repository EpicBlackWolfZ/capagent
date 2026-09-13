package contract_test

import (
	"bytes"
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
	"reflect"
	"testing"
)

func FuzzLocalIdentity(f *testing.F) {
	f.Add([]byte("root:x:0:0::/root:/bin/sh\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			selector := model.TargetSelector{Value: "uid:0"}
			a, ae := host.ResolveTarget(selector, input, []byte("root:x:0:\n"))
			b, be := host.ResolveTarget(selector, input, []byte("root:x:0:\n"))
			if !reflect.DeepEqual(a, b) || (ae == nil) != (be == nil) || !bytes.Equal(input, before) {
				t.Fatal("unstable identity parser")
			}
			if ae == nil && a.IsValid() != nil {
				t.Fatal("invalid identity accepted")
			}
			_, _ = host.ResolveTarget(selector, []byte("root:x:0:0::/root:/bin/sh\n"), input)
		})
	})
}

func FuzzSubIDs(f *testing.F) {
	f.Add([]byte("alice:100000:20\n1000:200000:30\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			_, _ = host.ParseMappingCapabilities(platform.CapabilityAttribute{Present: true, Bytes: input})
			target := model.UserIdentity{UID: 1000, Username: "alice"}
			a, ae := host.ParseSubIDs(input, target)
			b, be := host.ParseSubIDs(input, target)
			if !reflect.DeepEqual(a, b) || (ae == nil) != (be == nil) || !bytes.Equal(before, input) {
				t.Fatal("unstable allocation parser")
			}
			if ae == nil && model.SubIDRanges(a.Ranges).IsValid() != nil {
				t.Fatal("invalid allocation accepted")
			}
		})
	})
}
