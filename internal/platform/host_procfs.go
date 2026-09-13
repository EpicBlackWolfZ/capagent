package platform

import (
	"context"
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
