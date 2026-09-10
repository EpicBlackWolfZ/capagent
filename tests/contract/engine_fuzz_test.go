package contract_test

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

const fuzzFilesystem = "filesystem"
const fuzzNodeCount = 32
const fuzzWorkerCount = 4
const fuzzJSONDepth = 64

func FuzzOrchestrator(f *testing.F) {
	for _, seed := range [][]byte{{}, {31, 3, 0, 2, 1}, {8, 2, 3, 0}, {2, 0, 0, 1}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.GraphInput).Run(t, input, func() {
			value := func(i int) int {
				if len(input) == 0 {
					return 0
				}
				return int(input[i%len(input)])
			}
			s := faultScenario{Count: 1 + value(0)%fuzzNodeCount, Width: 1 + value(1)%fuzzWorkerCount}
			s.Width = min(s.Width, s.Count)
			s.Workers, s.FaultAt = s.Width, value(2)%s.Count
			s.Profile = []string{fuzzFilesystem, "command", "scheduler", profileRegistry}[value(3)%fuzzWorkerCount]
			s.Cancel = s.Profile == "scheduler" && value(0)%2 == 1
			for start := 0; start < s.Count; start += s.Width {
				width := min(s.Width, s.Count-start)
				for offset := range width {
					s.Order = append(s.Order, start+(offset+value(start))%width)
				}
			}
			a, b := executeChaosScenario(t, s), executeChaosScenario(t, s)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("same input changed normalized outcomes or logical checkpoints")
			}
		})
	})
}

func FuzzReportJSON(f *testing.F) {
	sch := compileSchema(f)
	for _, seed := range []string{"{}", "null", "{", "{\"schema_version\":1}", "{\"context\":{\"uid\":-1}}", "[[]]"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.ParserInput).Run(t, input, func() {
			before := bytes.Clone(input)
			if !fuzzJSONWithinDepth(input) {
				return
			}
			a, ae := output.Unmarshal(input)
			b, be := output.Unmarshal(input)
			if !reflect.DeepEqual(a, b) || fmt.Sprint(ae) != fmt.Sprint(be) || !bytes.Equal(before, input) {
				t.Fatal("JSON decoder is nondeterministic or mutates input")
			}
			if ae == nil {
				// Validate is deliberately not assumed equivalent to the full schema (#64).
				if (a.Validate() == nil) != (b.Validate() == nil) {
					t.Fatal("validation changed")
				}
				encoded, err := output.MarshalCompact(a)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := output.Unmarshal(encoded)
				if err != nil {
					t.Fatal(err)
				}
				canonical, err := output.MarshalCompact(decoded)
				if err != nil || !bytes.Equal(encoded, canonical) {
					t.Fatal("canonical JSON is not idempotent")
				}
				if !reflect.DeepEqual(a, b) {
					t.Fatal("serialization mutated decoded report")
				}
			}
			// Generated reports stay inside the currently shipped schema contract.
			report := output.NewReport()
			report.Host.OS, report.Host.CgroupVersion = "linux", "unknown"
			report.Context.TargetUser = strings.ToValidUTF8(string(input), "?")
			states := []string{
				string(model.StateSupported), string(model.StateUnsupported), string(model.StateMisconfigured),
				string(model.StateUnavailable), string(model.StateUnknown),
			}
			index := 0
			if len(input) > 0 {
				index = int(input[0]) % len(states)
			}
			report.Capabilities["runtime.podman"] = output.CapabilityReport{
				State: states[index], Confidence: string(model.ConfidenceVerified), Evidence: []string{"z", "a"},
			}
			if err := report.Validate(); err != nil {
				t.Fatal(err)
			}
			encoded, err := output.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateJSON(t, sch, encoded); err != nil {
				t.Fatal(err)
			}
			if report.Capabilities["runtime.podman"].Evidence[0] != "z" {
				t.Fatal("serialization mutates evidence")
			}
			again, err := output.Marshal(report)
			if err != nil || !bytes.Equal(encoded, again) {
				t.Fatal("serialization is not deterministic")
			}
		})
	})
}

func fuzzJSONWithinDepth(input []byte) bool {
	decoder := jsontext.NewDecoder(bytes.NewReader(input))
	for {
		_, err := decoder.ReadToken()
		if err != nil {
			return true
		}
		if decoder.StackDepth() > fuzzJSONDepth {
			return false
		}
	}
}
