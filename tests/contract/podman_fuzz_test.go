package contract_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

func FuzzPodmanInputs(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"host":{"security":{"rootless":false}}}`,
		`{"store":{"graphRoot":"/","runRoot":null}}`, "podman version 4.9.4-rhel\n", "podman version 5.8.4"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			a, ae := podman.ParseInfo(input)
			b, be := podman.ParseInfo(input)
			v, ve := podman.ParseVersion(input)
			w, we := podman.ParseVersion(input)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) || !reflect.DeepEqual(v, w) ||
				fmt.Sprint(ve) != fmt.Sprint(we) || !bytes.Equal(before, input) {
				t.Fatal("Podman parsing is nondeterministic or mutates input")
			}
		})
	})
}
