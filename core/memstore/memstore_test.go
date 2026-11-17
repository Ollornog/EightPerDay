// SPDX-License-Identifier: MIT

package memstore_test

import (
	"testing"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/memstore"
	"github.com/Ollornog/EightPerDay/core/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(*testing.T) core.Store { return memstore.New() })
}
