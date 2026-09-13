package platform

import (
	"context"
	"errors"
	"strings"
)

// NewHostProcfsReader keeps the supplied scoped root as authority and prefixes
// fixed procfs paths without cleaning untrusted subpaths.
func NewHostProcfsReader(files ScopedView) *ProcfsReader {
	return &ProcfsReader{reader: files, prefix: "proc/", limits: DefaultParserLimits()}
}

// DecodeMountPath decodes only kernel mountinfo escapes, without path cleaning.
func DecodeMountPath(raw string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			out.WriteByte(raw[i])
			continue
		}
		const escapeLength = 4
		if i+escapeLength > len(raw) {
			return "", ErrMalformed
		}
		switch raw[i : i+escapeLength] {
		case `\040`:
			out.WriteByte(' ')
		case `\011`:
			out.WriteByte('\t')
		case `\012`:
			out.WriteByte('\n')
		case `\134`:
			out.WriteByte('\\')
		default:
			return "", ErrMalformed
		}
		i += escapeLength - 1
	}
	return out.String(), nil
}
func ParseControllerList(ctx context.Context, data []byte, err error) ([]string, error) {
	return parseControllers(ctx, data, err)
}

// ProtocolEntry records registration in the current network namespace, not socket permission or reachability.
type ProtocolEntry struct{ Name string }

func (p *ProcfsReader) Protocols(ctx context.Context) ([]ProtocolEntry, error) {
	data, err := p.ReadSelf(ctx, "net/protocols")
	entries, parseErr := parseRecords(ctx, data, err, "protocols", p.limits, parseProtocolLine)
	out := make([]ProtocolEntry, 0, len(entries))
	header := false
	for _, entry := range entries {
		if entry.Name == "protocol" {
			header = true
			continue
		}
		out = append(out, entry)
	}
	if !header {
		parseErr = errors.Join(parseErr, ErrIncomplete)
	}
	return out, parseErr
}
func parseProtocolLine(line string) (ProtocolEntry, error) {
	const protocolFields = 8
	fields := strings.Fields(line)
	if len(fields) < protocolFields {
		return ProtocolEntry{}, ErrMalformed
	}
	if fields[0] == "protocol" {
		if strings.Join(fields[:protocolFields], " ") != "protocol size sockets memory press maxhdr slab module" {
			return ProtocolEntry{}, ErrMalformed
		}
		return ProtocolEntry{Name: "protocol"}, nil
	}
	for _, ch := range fields[0] {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			return ProtocolEntry{}, ErrMalformed
		}
	}
	if !decimal(fields[1]) || !decimal(fields[2]) {
		return ProtocolEntry{}, ErrMalformed
	}
	return ProtocolEntry{Name: fields[0]}, nil
}
