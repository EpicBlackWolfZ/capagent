package host

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"io/fs"
)

const namespacedCapabilityRevision = 3
const capabilityRevisionShift = 24
const capabilityEffectiveFlag = 1
const capSetUID = uint32(1 << 7)
const capSetGID = uint32(1 << 6)
const capabilityV1Bytes, capabilityV2Bytes, capabilityV3Bytes = 12, 20, 24
const mappingExecutableBits = 0o111

func ParseMappingCapabilities(attribute platform.CapabilityAttribute) (*model.MappingCapabilities, error) {
	out := &model.MappingCapabilities{Present: attribute.Present}
	if !attribute.Present {
		return out, nil
	}
	data := attribute.Bytes
	if len(data) < capabilityV1Bytes {
		return nil, platform.ErrMalformed
	}
	magic := binary.LittleEndian.Uint32(data)
	revision := magic >> capabilityRevisionShift
	expected := map[uint32]int{1: capabilityV1Bytes, 2: capabilityV2Bytes, 3: capabilityV3Bytes}[revision]
	if len(data) != expected || magic&0x00ffffff > capabilityEffectiveFlag {
		return nil, platform.ErrMalformed
	}
	out.Revision = revision
	out.Effective = magic&capabilityEffectiveFlag != 0
	permitted := binary.LittleEndian.Uint32(data[4:])
	inheritable := binary.LittleEndian.Uint32(data[8:])
	out.InheritableSetUID = inheritable&capSetUID != 0
	out.InheritableSetGID = inheritable&capSetGID != 0
	out.SetUID = permitted&capSetUID != 0
	out.SetGID = permitted&capSetGID != 0
	if revision == namespacedCapabilityRevision {
		id := binary.LittleEndian.Uint32(data[20:])
		if id == ^uint32(0) {
			return nil, platform.ErrMalformed
		}
		out.RootID = &id
	}
	return out, nil
}
func observeMappingHelper(ctx context.Context, env platform.Environment, obs *model.Observation, name string) model.MappingHelper {
	h := model.MappingHelper{Name: name}
	for _, prefix := range []string{"/usr/bin/", "/usr/local/bin/", "/bin/"} {
		h.Path = prefix + name
		info, err := env.Files().Stat(h.Path[1:])
		recordSource(obs, h.Path, missingIsKnown(err))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return h
		}
		h.Present = ptr(true)
		h.Regular = ptr(info.Mode().IsRegular())
		h.SetUID = ptr(info.Mode()&fs.ModeSetuid != 0)
		h.SetGID = ptr(info.Mode()&fs.ModeSetgid != 0)
		mode := uint32(info.Mode().Perm())
		h.Mode = &mode
		if owner, known := platform.OwnershipOf(info); known {
			h.UID = &owner.UID
			h.GID = &owner.GID
		}
		if !*h.Regular || info.Mode().Perm()&mappingExecutableBits == 0 {
			h.Usable = ptr(false)
			return h
		}
		access, accessErr := env.Files().ExecutableAccess(ctx, h.Path[1:])
		recordSource(obs, h.Path+" execute-access", accessErr)
		if accessErr == nil {
			h.Executable = &access
			if !access {
				h.Usable = ptr(false)
			}
		}
		inspectMappingPrivilege(ctx, env, obs, &h)
		return h
	}
	h.Path = ""
	h.Present = ptr(false)
	h.Usable = ptr(false)
	return h
}
func inspectMappingPrivilege(ctx context.Context, env platform.Environment, obs *model.Observation, h *model.MappingHelper) {
	attribute, err := env.Files().FileCapabilities(ctx, h.Path[1:])
	if err == nil {
		h.Capabilities, err = ParseMappingCapabilities(attribute)
	}
	recordSource(obs, h.Path+" file-capabilities", err)
	mount, mountErr := env.Files().StatFS(ctx, h.Path[1:])
	recordSource(obs, h.Path+" mount-policy", mountErr)
	if mountErr == nil && mount.FlagsKnown {
		h.NoSUID = &mount.NoSUID
		h.NoExec = &mount.NoExec
	}
	if env.Host() != nil {
		nnp, nnpErr := env.Host().NoNewPrivileges()
		recordSource(obs, "process no-new-privileges", nnpErr)
		if nnpErr == nil {
			h.NoNewPrivileges = &nnp
		}
	}
	classifyMappingPrivilege(h)
	// Metadata never proves namespace, capability-bounding-set, LSM or mapping authorization.
	if h.Usable == nil {
		hostWarning(obs, "mapping_execution_unverified")
	}
}
func classifyMappingPrivilege(h *model.MappingHelper) {
	if h.NoExec != nil && *h.NoExec {
		h.Usable = ptr(false)
	}
	if h.NoSUID != nil && *h.NoSUID || h.NoNewPrivileges != nil && *h.NoNewPrivileges {
		h.PrivilegeBlocked = ptr(true)
	}
}
