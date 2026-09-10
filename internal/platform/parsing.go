package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	defaultLineBytes   = 64 * 1024
	defaultRecords     = 8192
	defaultDiagnostics = 32
)

// ParserLimits bounds each parsing operation independently of the reader.
type ParserLimits struct{ InputBytes, LineBytes, MountLineBytes, Records, Diagnostics int }

func DefaultParserLimits() ParserLimits {
	return ParserLimits{InputBytes: defaultFileBytes, LineBytes: defaultLineBytes, MountLineBytes: mountinfoMaxLineBytes,
		Records: defaultRecords, Diagnostics: defaultDiagnostics}
}
func (l ParserLimits) validate() error {
	for _, n := range []int{l.InputBytes, l.LineBytes, l.MountLineBytes, l.Records, l.Diagnostics} {
		if n <= 0 || n > int(^uint(0)>>1)/2 {
			return errors.New("platform: parser limits must be positive and below half MaxInt")
		}
	}
	return nil
}

// ParseDiagnostic contains no raw input, only the record location and reason.
type ParseDiagnostic struct {
	Line  int
	Cause error
}

// ParseError retains a bounded list while counting all omitted diagnostics.
type ParseError struct {
	Source             string
	Diagnostics        []ParseDiagnostic
	Omitted            int
	malformed, limited bool
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("platform: parse %s: %d diagnostics (%d omitted)", e.Source, len(e.Diagnostics), e.Omitted)
}
func (e *ParseError) Is(target error) bool {
	return target == ErrIncomplete || (target == ErrMalformed && e.malformed) || (target == ErrLimitExceeded && e.limited)
}
func (e *ParseError) Unwrap() []error {
	causes := make([]error, 0, len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		causes = append(causes, d.Cause)
	}
	return causes
}
func (e *ParseError) add(line int, err error, limit int) {
	e.malformed = e.malformed || errors.Is(err, ErrMalformed)
	e.limited = e.limited || errors.Is(err, ErrLimitExceeded)
	if len(e.Diagnostics) < limit {
		e.Diagnostics = append(e.Diagnostics, ParseDiagnostic{Line: line, Cause: err})
	} else {
		e.Omitted++
	}
}

func parseRecords[T any](ctx context.Context, data []byte, readErr error, source string, limits ParserLimits,
	parse func(string) (T, error)) ([]T, error) {
	var out []T
	if len(data) > limits.InputBytes {
		data = data[:limits.InputBytes]
		readErr = errors.Join(readErr, &LimitError{Resource: "parser input bytes", Limit: limits.InputBytes})
	}
	if readErr != nil {
		// Without clean EOF, an unterminated suffix is not a complete record.
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	diagnostics := &ParseError{Source: source}
	lineNumber := 0
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			readErr = errors.Join(readErr, err)
			break
		}
		lineNumber++
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			index = len(data)
		}
		line := data[:index]
		data = data[min(index+1, len(data)):]
		if len(line) > limits.LineBytes {
			diagnostics.add(lineNumber, &LimitError{Resource: "line bytes", Limit: limits.LineBytes}, limits.Diagnostics)
			continue
		}
		text := string(line)
		if strings.TrimSpace(text) == "" {
			continue
		}
		entry, err := parse(strings.TrimLeft(text, " \t"))
		if err != nil {
			diagnostics.add(lineNumber, err, limits.Diagnostics)
			continue
		}
		if len(out) == limits.Records {
			diagnostics.add(lineNumber, &LimitError{Resource: "records", Limit: limits.Records}, limits.Diagnostics)
			break
		}
		out = append(out, entry)
	}
	var parseErr error
	if len(diagnostics.Diagnostics) > 0 {
		parseErr = diagnostics
	}
	return out, errors.Join(parseErr, incomplete(readErr), incomplete(ctx.Err()))
}

func decimal(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func identifier(text string) bool {
	if text == "" || text[0] < 'a' || text[0] > 'z' {
		return false
	}
	for _, ch := range text {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}
