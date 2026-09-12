package capability

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const MaxGraphEntries = 4096
const MaxGraphReferences = 16384

type Dataset struct {
	RunID        string
	At           time.Time
	Contexts     []model.EvaluationContext
	Observations []model.Observation
	Evidence     []model.Evidence
}

// ValidateDataset validates the graph independently of resolution. Each context
// ID binds exactly one current/target identity and namespace snapshot in a run.
// Input collections are borrowed only for this call.
func ValidateDataset(d Dataset) error {
	if d.RunID == "" || d.At.IsZero() {
		return errors.New("missing evaluation run identity or timestamp")
	}
	if len(d.Contexts)+len(d.Observations)+len(d.Evidence) > MaxGraphEntries {
		return errors.New("evidence graph size limit exceeded")
	}
	contexts, err := indexContexts(d.Contexts)
	if err != nil {
		return err
	}
	observations, err := indexObservations(d.Observations, contexts)
	if err != nil {
		return err
	}
	edges := make(map[string][]string)
	evidence := make(map[string]model.Evidence)
	references := 0
	for _, ev := range d.Evidence {
		if err := validateEvidence(ev); err != nil {
			return err
		}
		if !contexts[ev.Scope.ContextID] {
			return errors.New("evidence references missing context")
		}
		if _, exists := edges[ev.ID]; exists {
			return errors.New("duplicate evidence ID")
		}
		evidence[ev.ID] = ev
		edges[ev.ID] = nil
		references += len(ev.Observations) + len(ev.DependsOn)
		if references > MaxGraphReferences {
			return errors.New("evidence reference limit exceeded")
		}
		if len(ev.Observations) == 0 {
			return errors.New("evidence has no observation")
		}
		if err := validateObservationReferences(ev, observations); err != nil {
			return err
		}
		for _, ref := range ev.DependsOn {
			edges[ev.ID] = append(edges[ev.ID], ref.ID)
		}
	}
	for id, deps := range edges {
		for _, dep := range deps {
			if evidence[dep].Scope != evidence[id].Scope {
				return errors.New("missing or incompatible evidence dependency")
			}
			if evidence[dep].Timestamp.After(evidence[id].Timestamp) ||
				(evidence[id].Completeness == model.Complete && evidence[dep].Completeness != model.Complete) {
				return errors.New("claim relies on incomplete or future evidence dependency")
			}
		}
	}
	_, err = topological(edges)
	return err
}

func indexObservations(input []model.Observation, contexts map[string]bool) (map[string]model.Observation, error) {
	observations := make(map[string]model.Observation)
	facts := make(map[string]bool)
	for _, obs := range input {
		if obs.ID == "" || obs.ProbeID == "" || obs.Timestamp.IsZero() || obs.Scope.IsValid() != nil ||
			!contexts[obs.Scope.ContextID] || obs.Completeness.IsValid() != nil {
			return nil, errors.New("invalid observation metadata or scope")
		}
		if _, exists := observations[obs.ID]; exists {
			return nil, errors.New("duplicate observation ID")
		}
		observations[obs.ID] = obs
		if len(obs.Facts) == 0 {
			return nil, errors.New("observation has no facts")
		}
		for _, fact := range obs.Facts {
			if fact.ID == "" || facts[fact.ID] || fact.Scope != obs.Scope || fact.Timestamp.IsZero() ||
				fact.Timestamp.After(obs.Timestamp) || fact.Source == "" ||
				fact.Completeness.IsValid() != nil {
				return nil, errors.New("invalid or duplicate fact")
			}
			if obs.Completeness == model.Complete && fact.Completeness != model.Complete {
				return nil, errors.New("complete observation relies on incomplete fact")
			}
			facts[fact.ID] = true
			if len(facts) > MaxGraphEntries {
				return nil, errors.New("fact count limit exceeded")
			}
		}
	}
	return observations, nil
}

func validateEvidence(ev model.Evidence) error {
	for _, err := range []error{ev.Validate(), ev.Scope.IsValid(), model.CapabilityID(ev.Claim).Validate(),
		ev.State.IsValid(), ev.Confidence.IsValid(), ev.Completeness.IsValid()} {
		if err != nil {
			return err
		}
	}
	return nil
}

// topological is a bounded deterministic declaration validator, not a worker
// scheduler. Dependency references are unique and must name a declared node.
func topological(edges map[string][]string) ([]string, error) {
	if len(edges) > MaxGraphEntries {
		return nil, errors.New("dependency graph size limit exceeded")
	}
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	counts := make(map[string]int)
	downstream := make(map[string][]string)
	references := 0
	for _, id := range ids {
		seen := make(map[string]bool)
		for _, dep := range edges[id] {
			if _, ok := edges[dep]; !ok || seen[dep] {
				return nil, fmt.Errorf("missing or duplicate dependency of %s", id)
			}
			seen[dep] = true
			counts[id]++
			references++
			if references > MaxGraphReferences {
				return nil, errors.New("dependency reference limit exceeded")
			}
			downstream[dep] = append(downstream[dep], id)
		}
	}
	var ready, order []string
	for _, id := range ids {
		if counts[id] == 0 {
			ready = append(ready, id)
		}
	}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, child := range downstream[id] {
			counts[child]--
			if counts[child] == 0 {
				ready = append(ready, child)
			}
		}
		slices.Sort(ready)
	}
	if len(order) != len(edges) {
		return nil, errors.New("cyclic dependency graph")
	}
	return order, nil
}

func indexContexts(input []model.EvaluationContext) (map[string]bool, error) {
	contexts := make(map[string]bool)
	for _, c := range input {
		if c.ID == "" || contexts[c.ID] {
			return nil, errors.New("missing or duplicate context ID")
		}
		contexts[c.ID] = true
		namespaces := make(map[string]bool)
		for _, ns := range c.Namespaces {
			if ns.Kind == "" || ns.ID == "" || namespaces[ns.Kind] {
				return nil, errors.New("invalid or duplicate namespace observation")
			}
			namespaces[ns.Kind] = true
		}
	}
	return contexts, nil
}

func validateObservationReferences(ev model.Evidence, observations map[string]model.Observation) error {
	seen := make(map[string]bool)
	for _, ref := range ev.Observations {
		obs, ok := observations[ref.ID]
		if !ok || seen[ref.ID] || obs.ProbeID != ref.ProbeID || obs.Scope != ev.Scope || obs.Timestamp.After(ev.Timestamp) {
			return errors.New("invalid observation reference, scope or timestamp")
		}
		seen[ref.ID] = true
		if ev.Completeness == model.Complete && obs.Completeness != model.Complete {
			return errors.New("complete claim relies on incomplete observation")
		}
	}
	return nil
}
