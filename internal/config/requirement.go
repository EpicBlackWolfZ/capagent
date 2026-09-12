// Package config parses bounded evaluation inputs without host access.
package config

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"

	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const (
	MaxRequirementBytes = 64 * 1024
	// JSON containers include the arrays around child nodes.
	MaxRequirementDepth = 2 * requirement.MaxDepth
)

func ParseRequirement(data []byte) (*requirement.Node, error) {
	if err := CheckJSON(data, MaxRequirementBytes, MaxRequirementDepth); err != nil {
		return nil, err
	}
	var node *requirement.Node
	if err := json.Unmarshal(data, &node, json.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid requirement JSON")
	}
	if err := requirement.Validate(node); err != nil {
		return nil, err
	}
	return node, nil
}

// CheckJSON bounds work before allocating a typed tree. The diagnostic never
// contains raw input (which may include credentials or command data).
func CheckJSON(data []byte, maxBytes, maxDepth int) error {
	if len(data) > maxBytes {
		return errors.New("JSON byte limit exceeded")
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		_, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("invalid JSON document")
		}
		if decoder.StackDepth() > maxDepth {
			return errors.New("JSON depth limit exceeded")
		}
	}
}
