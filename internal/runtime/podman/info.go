// Package podman collects explicitly scoped runtime observations.
package podman

import (
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
		NetworkBackend     *string `json:"networkBackend"`
		NetworkBackendInfo struct {
			Path string `json:"path"`
		} `json:"networkBackendInfo"`
		ServiceIsRemote *bool   `json:"serviceIsRemote"`
		CgroupVersion   *string `json:"cgroupVersion"`
		CgroupManager   *string `json:"cgroupManager"`
		Security        struct {
			Rootless *bool `json:"rootless"`
		} `json:"security"`
	} `json:"host"`
	Store struct {
		GraphDriverName *string `json:"graphDriverName"`
		GraphRoot       *string `json:"graphRoot"`
		RunRoot         *string `json:"runRoot"`
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
	return model.PodmanInfo{Version: document.Version.Version, NetworkBackend: document.Host.NetworkBackend,
		HelperPath: document.Host.NetworkBackendInfo.Path, CgroupVersion: document.Host.CgroupVersion,
		CgroupManager: document.Host.CgroupManager, Rootless: document.Host.Security.Rootless,
		StorageDriver: document.Store.GraphDriverName, GraphRoot: document.Store.GraphRoot, RunRoot: document.Store.RunRoot,
		ServiceIsRemote: document.Host.ServiceIsRemote}, nil
}
