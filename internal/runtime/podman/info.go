// Package podman collects explicitly scoped runtime observations.
package podman

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const maxInfoBytes = 1 << 20
const maxInfoDepth = 32
const executableBits = 0o111

type infoDocument struct {
	Version struct{ Version string }
	Host    struct {
		OCIRuntime         *model.SelectedOCIRuntime   `json:"ociRuntime"`
		Conmon             struct{ Path string }       `json:"conmon"`
		Pasta              struct{ Executable string } `json:"pasta"`
		Slirp              struct{ Executable string } `json:"slirp4netns"`
		NetworkBackend     *string                     `json:"networkBackend"`
		NetworkBackendInfo struct {
			Path string                `json:"path"`
			DNS  struct{ Path string } `json:"dns"`
		} `json:"networkBackendInfo"`
		ServiceIsRemote *bool   `json:"serviceIsRemote"`
		CgroupVersion   *string `json:"cgroupVersion"`
		CgroupManager   *string `json:"cgroupManager"`
		Security        struct {
			Rootless *bool `json:"rootless"`
		} `json:"security"`
	} `json:"host"`
	Store struct {
		GraphOptions    map[string]jsontext.Value `json:"graphOptions"`
		GraphDriverName *string                   `json:"graphDriverName"`
		GraphRoot       *string                   `json:"graphRoot"`
		RunRoot         *string                   `json:"runRoot"`
	} `json:"store"`
}

func ParseInfo(data []byte) (model.PodmanInfo, error) {
	if err := config.CheckJSON(data, maxInfoBytes, maxInfoDepth); err != nil {
		return model.PodmanInfo{}, err
	}
	var document *infoDocument
	if err := json.Unmarshal(data, &document, json.MatchCaseInsensitiveNames(true)); err != nil || document == nil {
		return model.PodmanInfo{}, errors.New("invalid Podman info document")
	}
	var mountProgram struct{ Executable string }
	if value, ok := document.Store.GraphOptions["overlay.mount_program"]; ok {
		if err := json.Unmarshal(value, &mountProgram, json.MatchCaseInsensitiveNames(true)); err != nil {
			return model.PodmanInfo{}, errors.New("invalid Podman storage helper metadata")
		}
	}
	return model.PodmanInfo{StorageMountProgram: mountProgram.Executable,
		Version:        document.Version.Version,
		NetworkBackend: document.Host.NetworkBackend,
		OCIRuntime:     document.Host.OCIRuntime, ConmonPath: document.Host.Conmon.Path,
		AardvarkPath: document.Host.NetworkBackendInfo.DNS.Path, PastaPath: document.Host.Pasta.Executable,
		SlirpPath:  document.Host.Slirp.Executable,
		HelperPath: document.Host.NetworkBackendInfo.Path, CgroupVersion: document.Host.CgroupVersion,
		CgroupManager: document.Host.CgroupManager, Rootless: document.Host.Security.Rootless,
		StorageDriver: document.Store.GraphDriverName, GraphRoot: document.Store.GraphRoot, RunRoot: document.Store.RunRoot,
		ServiceIsRemote: document.Host.ServiceIsRemote}, nil
}
