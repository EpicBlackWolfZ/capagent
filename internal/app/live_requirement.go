package app

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"path/filepath"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

func applyLiveRequirement(input *Input, opts Options) {
	if opts.Runtime == "" {
		input.Requirement, input.Definitions = nil, []capability.Definition{}
		return
	}
	if opts.Active {
		input.Requirement = &requirement.Node{All: []*requirement.Node{{Capability: capability.PodmanID}, {Capability: capability.PodmanInfoID}}}
	}
	if opts.Active || opts.requirementNode != nil {
		input.Definitions = append(input.Definitions, capability.PodmanInfoDefinition(), capability.NetavarkDefinition())
	}
	if opts.requirementNode != nil {
		input.Requirement = opts.requirementNode
	}
}

// The caller selects the document before identity delegation. The worker gets
// these owned bytes and never reopens the caller's path under another identity.
func readLiveRequirement(ctx context.Context, opts Options, stderr io.Writer) (Options, int) {
	data, err := platform.ReadDocument(ctx, filepath.Dir(opts.Requirement), filepath.Base(opts.Requirement), config.MaxRequirementBytes)
	if err != nil {
		return opts, failure(stderr, ExitExecution, "cannot read requirement document")
	}
	node, err := config.ParseRequirement(data)
	if err != nil {
		return opts, failure(stderr, ExitUsage, "invalid requirement document")
	}
	opts.requirementJSON, opts.requirementNode = data, node
	return opts, 0
}

func bindTargetRequirement(payload *targetPayload) error {
	opts := &payload.Options
	opts.requirementJSON, opts.requirementNode = nil, nil
	if (opts.Requirement != "") != (len(payload.Requirement) != 0) {
		return errors.New("target requirement presence mismatch")
	}
	if len(payload.Requirement) == 0 {
		return nil
	}
	node, err := config.ParseRequirement(payload.Requirement)
	if err != nil {
		return err
	}
	opts.requirementJSON, opts.requirementNode = bytes.Clone(payload.Requirement), node
	return nil
}

func fixtureExplanationRequirement(opts *Options, data jsontext.Value) error {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	node, err := config.ParseRequirement(data)
	if err != nil {
		return err
	}
	opts.requirementNode = node
	return nil
}
