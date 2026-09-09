package platform

import (
	"errors"
	"os"
	"reflect"
	"syscall"
	"testing"
)

const metadataFailureName = "failure"

func TestCaptureDirectoryFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{syscall.ENOENT, syscall.EACCES, syscall.EIO, syscall.ENOTDIR} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			entries, err := captureDirectory([]string{"z", ".", metadataFailureName, "..", "a"}, func(name string) (os.FileInfo, error) {
				if name == "." || name == ".." {
					t.Fatal("dot entry reached metadata lookup")
				}
				if name == metadataFailureName {
					return nil, &os.PathError{Op: "test", Path: name, Err: failure}
				}
				return &memFileInfo{name: name}, nil
			})
			if failure != syscall.ENOENT {
				if entries != nil || !errors.Is(err, failure) {
					t.Fatalf("got %v, %v; want nil, %v", entries, err, failure)
				}
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || pathErr.Path != metadataFailureName {
					t.Fatalf("missing path context: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if !reflect.DeepEqual(names, []string{"a", "z"}) {
				t.Fatalf("names = %v", names)
			}
		})
	}
}
