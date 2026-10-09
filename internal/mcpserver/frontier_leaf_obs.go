// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// frontierLeafObs copies classifier-relevant fields from a loaded frontier
// leaf. Consume and sentinel must share this mapping (🎯T431): dropping
// OwnedBy on one path is how an owner-assigned target stayed "unconsumed"
// in the sentinel depth figure after consume had already learned to park it.
func frontierLeafObs(leaf targetfile.FrontierLeaf, engaged bool) poproactive.LeafObs {
	return poproactive.LeafObs{
		ID:              leaf.ID,
		Tags:            leaf.Tags,
		Name:            leaf.Name,
		Context:         leaf.Context,
		Cost:            leaf.Cost,
		SetAsideDeps:    leaf.SetAsideDeps,
		ActiveChildren:  leaf.ActiveChildren,
		ParkedAncestors: leaf.ParkedAncestors,
		OwnedBy:         leaf.OwnedBy,
		OwnedByReason:   leaf.OwnedByReason,
		ForceEngage:     poproactive.IsForceEngageTag(leaf.Tags),
		AlreadyEngaged:  engaged,
	}
}
