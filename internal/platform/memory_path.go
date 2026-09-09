package platform

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// memoryWalk models pathname traversal, not lexical normalization. Stored nodes
// are immutable after insertion; callers may retain a node after releasing RLock.
type memoryWalk struct {
	mem         *MemPlatformReader
	root        string
	current     string
	pending     []string
	hops        int
	limit       int
	followFinal bool
}

func (m *MemPlatformReader) resolvePath(path, root string, followFinal bool, limit int) (*VirtualFile, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w := memoryWalk{mem: m, root: root, current: root, pending: strings.Split(path, "/"), limit: limit, followFinal: followFinal}
	if root == "" {
		w.current = "."
		if filepath.IsAbs(path) {
			w.current = "/"
		}
	}
	return w.resolve()
}

func (w *memoryWalk) resolve() (*VirtualFile, string, error) {
	for len(w.pending) > 0 {
		part := w.pending[0]
		w.pending = w.pending[1:]
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			if part == ".." {
				parent := filepath.Dir(w.current)
				if w.root == "" && !filepath.IsAbs(w.current) && strings.Trim(w.current, "./") == "" {
					parent = filepath.Join(w.current, "..")
				}
				if (w.current == w.root && w.root != "/") || !withinMemoryRoot(parent, w.root) {
					return nil, "", ErrSubpathEscape
				}
				w.current = parent
			}
			continue
		}
		next := filepath.Join(w.current, part)
		node, err := w.lookup(next)
		if err != nil {
			return nil, "", err
		}
		if node.Kind == FileKindSymlink && (w.followFinal || len(w.pending) > 0) {
			if err := w.expand(node.Target); err != nil {
				return nil, "", err
			}
			continue
		}
		if len(w.pending) > 0 && node.Kind != FileKindDirectory {
			return nil, "", syscall.ENOTDIR
		}
		w.current = next
	}
	node, err := w.lookup(w.current)
	return node, w.current, err
}

func (w *memoryWalk) lookup(path string) (*VirtualFile, error) {
	node := w.mem.files[path]
	if node == nil {
		return nil, os.ErrNotExist
	}
	if node.ForcedErr != nil {
		return nil, node.ForcedErr
	}
	return node, nil
}

func (w *memoryWalk) expand(target string) error {
	w.hops++
	if w.hops > w.limit {
		return syscall.ELOOP
	}
	if target == "" {
		return os.ErrNotExist
	}
	if filepath.IsAbs(target) {
		if !withinMemoryRoot(target, w.root) {
			return ErrSubpathEscape
		}
		if w.root == "" {
			w.current = "/"
		} else {
			// Absolute memory targets refer to the backing tree, unlike RESOLVE_IN_ROOT.
			// Strip only the literal root prefix; never clean away target components.
			w.current = w.root
			target = strings.TrimPrefix(target, strings.TrimRight(w.root, "/"))
		}
	}
	w.pending = append(strings.Split(target, "/"), w.pending...)
	return nil
}

func withinMemoryRoot(path, root string) bool {
	if root == "" || root == "/" {
		return true
	}
	return path == root || strings.HasPrefix(path, root+"/")
}
