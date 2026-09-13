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
		"[engine]\nenv=['SECRET=redacted']", "[storage]\ndriver='overlay'", "x={a=1,}", "x=12:30", "x=2026-09-13T12:30Z",
		"x=" + strings.Repeat("[", adversarialNesting) + strings.Repeat("]", adversarialNesting)} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			fuzzStorageProjection(t, input, true)
			fuzzStorageProjection(t, input, false)
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

func fuzzStorageProjection(t *testing.T, input []byte, composefs bool) {
	t.Helper()
	first, firstErr := config.ParseStorageForFields(input, config.StorageFields{Composefs: composefs})
	second, secondErr := config.ParseStorageForFields(input, config.StorageFields{Composefs: composefs})
	if !reflect.DeepEqual(first, second) || fmt.Sprint(firstErr) != fmt.Sprint(secondErr) {
		t.Fatal("storage parser is nondeterministic")
	}
	if firstErr != nil {
		switch firstErr.Error() {
		case "config_malformed", "config_limit", "config_field_invalid", "config_field_unsupported", "config_toml_version_unsupported":
			return
		default:
			t.Fatal("unreviewed storage parser diagnostic")
		}
	}
	a, b := config.ProjectStorage(first, "source"), config.ProjectStorage(second, "source")
	if !reflect.DeepEqual(a, b) {
		t.Fatal("storage projection is nondeterministic")
	}
	for _, value := range []*model.ConfigString{a.GraphRoot, a.RunRoot, a.RootlessStoragePath} {
		if value == nil {
			continue
		}
		env := map[string]string{"HOME": "/home/target", "XDG_DATA_HOME": "/data", "XDG_RUNTIME_DIR": "/runtime"}
		one, errOne := config.ExpandStoragePath(value.Value, env, 1001)
		two, errTwo := config.ExpandStoragePath(value.Value, env, 1001)
		if one != two || fmt.Sprint(errOne) != fmt.Sprint(errTwo) {
			t.Fatal("storage expansion is nondeterministic")
		}
	}
}
