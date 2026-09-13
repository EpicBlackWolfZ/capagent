package fixture

import (
	"context"
	"errors"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const inspectionCommandCount = 2

// Combined replay has exactly the two reviewed current-user commands. Legacy
// single-command documents retain their original offline-only contract.
func validateInspection(d *Document, services *Services) error {
	current, target := d.Context.Identity.Current, d.Context.Identity.Target
	if current == nil || target == nil || !current.GroupsKnown || !target.GroupsKnown ||
		current.UID != target.UID || current.GID != target.GID ||
		!slices.Equal(current.SupplementaryGroups, target.SupplementaryGroups) {
		return errors.New("inspection fixture requires the current deployment identity")
	}
	want, err := podman.PrepareInspection(context.Background(), services.Environment.Files(), *current,
		services.Command.Path, services.Command.Env)
	if err != nil {
		return err
	}
	if !sameCommand(services.Command, want.Version) || !sameCommand(services.InfoCommand, want.Info) {
		return errors.New("inspection fixture commands must match the local collection policy")
	}
	return nil
}

func sameCommand(a, b platform.CommandSpec) bool {
	return a.Path == b.Path && a.Dir == b.Dir && a.Timeout == b.Timeout && slices.Equal(a.Args, b.Args) &&
		slices.Equal(a.Env.Variables(), b.Env.Variables())
}
