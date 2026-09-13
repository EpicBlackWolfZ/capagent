package contract_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

func FuzzConfigTOML(f *testing.F) {
	const adversarialNesting = 20000
	for _, seed := range []string{"", "[engine]\nruntime='crun'", "[engine]\nconmon_path=['/a',{append=true}]",
		"[engine]\nenv=['SECRET=redacted']", "x={a=1,}", "x=12:30", "x=2026-09-13T12:30Z",
		"x=" + strings.Repeat("[", adversarialNesting) + strings.Repeat("]", adversarialNesting)} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			a, ae := config.ParseEngine(input)
			b, be := config.ParseEngine(input)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) || !bytes.Equal(before, input) {
				t.Fatal("configuration parser is nondeterministic or mutates input")
			}
			if ae != nil {
				switch ae.Error() {
				case "config_malformed", "config_limit", "config_field_invalid", "config_field_unsupported", "config_toml_version_unsupported":
					return
				default:
					t.Fatal("unreviewed parser diagnostic")
				}
			}
			x := config.MergeEngine(model.EngineConfiguration{}, a, "source")
			y := config.MergeEngine(model.EngineConfiguration{}, b, "source")
			if !reflect.DeepEqual(x, y) {
				t.Fatal("configuration merge is nondeterministic")
			}
		})
	})
}
