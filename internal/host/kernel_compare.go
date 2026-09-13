package host

import (
	"cmp"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// CompareKernelVersions compares numeric metadata only; vendor backports are not feature evidence.
func CompareKernelVersions(a, b model.KernelVersion) int {
	for _, pair := range [][2]uint32{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if order := cmp.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	return 0
}
