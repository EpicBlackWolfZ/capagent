package platform

import (
	"bytes"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
	"reflect"
	"testing"
)

func FuzzTargetRequest(f *testing.F) {
	f.Add([]byte(`{"version":1,"run_id":"run","context_id":"uid:0","target":{"uid":0,"gid":0,"groups_known":true},` +
		`"namespaces":[{"kind":"user","id":"user:[1]"}],"payload":"e30="}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			a, ae := decodeTarget(input)
			b, be := decodeTarget(input)
			if !reflect.DeepEqual(a, b) || (ae == nil) != (be == nil) || !bytes.Equal(input, before) {
				t.Fatal("unstable target decoder")
			}
			if ae == nil && a.validate() != nil {
				t.Fatal("invalid target accepted")
			}
		})
	})
}
