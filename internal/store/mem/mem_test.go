package mem

import (
	"testing"

	"github.com/Slicit/componium/internal/store"
	"github.com/Slicit/componium/internal/store/storetest"
)

// The stand-in, held to the same contract as the real one. That is the only
// thing that makes it trustworthy enough for the rest of the suite to use.
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return New() })
}
