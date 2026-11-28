package cli

import (
	"fmt"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/effect/fileset"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestLockUnconfirmedRetirementTellsUserToPreserveEvidence(t *testing.T) {
	err := fmt.Errorf("lock failed: %w", fileset.ErrFileSetRetirementUnconfirmed)
	guidance := lockMetadataGuidance(workflowlock.MetadataRecoveryStatusOf(err), "", "")
	if len(guidance) != 1 || guidance[0].Kind() != clipresent.GuidanceInstruction ||
		!strings.Contains(guidance[0].Message(), "preserve any remaining recovery files") {
		t.Fatalf("expected preservation advice without a retry command, got %+v", guidance)
	}
}
