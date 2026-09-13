package config

import (
	"bytes"
	"errors"

	"github.com/pelletier/go-toml/v2/unstable"
)

var ErrConfigSyntaxUnsupported = errors.New("config_toml_version_unsupported")

// The qualified Podman builds use TOML 1.0. The pinned decoder additionally
// accepts 1.1, so reject only its grammar additions over already bounded ASTs.
// This is a compatibility filter, not a second TOML parser. In particular,
// newlines within inline-table values remain legal under TOML 1.0.
func compatibleTOML10(parser *unstable.Parser, node *unstable.Node) error {
	switch node.Kind {
	case unstable.String, unstable.Key:
		if !compatibleString(parser.Raw(node.Raw)) {
			return ErrConfigSyntaxUnsupported
		}
	case unstable.InlineTable:
		if !compatibleInlineTable(parser, node) {
			return ErrConfigSyntaxUnsupported
		}
	case unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		const datePrefixBytes, secondsSeparatorOffset = 11, 5
		value := parser.Raw(node.Raw)
		if node.Kind != unstable.LocalTime {
			if len(value) < datePrefixBytes {
				return ErrConfigMalformed
			}
			value = value[datePrefixBytes:]
		}
		if len(value) <= secondsSeparatorOffset || value[secondsSeparatorOffset] != ':' {
			return ErrConfigSyntaxUnsupported
		}
	}
	children := node.Children()
	for children.Next() {
		if err := compatibleTOML10(parser, children.Node()); err != nil {
			return err
		}
	}
	return nil
}

func compatibleString(raw []byte) bool {
	if len(raw) == 0 || raw[0] != '"' {
		return true
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			if raw[i] == 'e' || raw[i] == 'x' {
				return false
			}
		}
	}
	return true
}

func compatibleInlineTable(parser *unstable.Parser, node *unstable.Node) bool {
	data := parser.Data()
	offset := int(node.Raw.Offset) + 1
	children := node.Children()
	for children.Next() {
		child := children.Node()
		if bytes.ContainsAny(data[offset:child.Raw.Offset], "\r\n#") {
			return false
		}
		offset = int(child.Raw.Offset + child.Raw.Length)
	}
	for offset < len(data) && (data[offset] == ' ' || data[offset] == '\t') {
		offset++
	}
	return offset < len(data) && data[offset] == '}'
}
