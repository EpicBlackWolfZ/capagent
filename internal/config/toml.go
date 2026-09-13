package config

import (
	"errors"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

const (
	MaxTOMLBytes = 64 * 1024
	MaxTOMLDepth = 32
	MaxTOMLNodes = 4096
)

var (
	ErrConfigMalformed = errors.New("config_malformed")
	ErrConfigLimit     = errors.New("config_limit")
)

// ParseTOML validates bounded syntax before allocating a decoded map. The pinned
// parser also bounds expression construction. Neither error path exposes input,
// decoder diagnostics, unknown field names, or configuration values.
func ParseTOML(data []byte) (map[string]any, error) {
	if len(data) > MaxTOMLBytes {
		return nil, ErrConfigLimit
	}
	var parser unstable.Parser
	parser.Reset(data)
	nodes, tableDepth := 0, 0
	for parser.NextExpression() {
		node := parser.Expression()
		depth := tableDepth
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			depth = 0
			tableDepth = keyLength(node)
		}
		if err := boundTOMLNode(node, depth, &nodes); err != nil {
			return nil, err
		}
		if err := compatibleTOML10(&parser, node); err != nil {
			return nil, err
		}
	}
	if parser.Error() != nil {
		return nil, ErrConfigMalformed
	}
	value := map[string]any{}
	if err := toml.Unmarshal(data, &value); err != nil {
		return nil, ErrConfigMalformed
	}
	return value, nil
}

func keyLength(node *unstable.Node) int {
	length := 0
	keys := node.Key()
	for keys.Next() {
		length++
	}
	return length
}

func boundTOMLNode(node *unstable.Node, depth int, nodes *int) error {
	*nodes++
	if *nodes > MaxTOMLNodes || depth > MaxTOMLDepth {
		return ErrConfigLimit
	}
	switch node.Kind {
	case unstable.Table, unstable.ArrayTable, unstable.KeyValue:
		length := keyLength(node)
		*nodes += length
		if depth+length > MaxTOMLDepth || *nodes > MaxTOMLNodes {
			return ErrConfigLimit
		}
		if node.Kind == unstable.KeyValue {
			return boundTOMLNode(node.Value(), depth+length, nodes)
		}
	case unstable.Array, unstable.InlineTable:
		children := node.Children()
		for children.Next() {
			if err := boundTOMLNode(children.Node(), depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}
