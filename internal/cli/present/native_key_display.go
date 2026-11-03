package clipresent

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/reconcile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func printNativeKeyLocation(output io.Writer, decision reconcile.AggregateSubjectDecision) {
	_, key, native := topologyprojection.HostConfigKeyIdentity(decision.Subject())
	if !native {
		return
	}
	fmt.Fprintf(output, "    destination=%s key: %s\n", Quote(decision.Destination().String()), nativeKeyDisplay(key))
}

func nativeKeyDisplay(key output.KeyPath) string {
	if len(key.String()) > maximumHumanErrorDetailRunes*utf8.UTFMax {
		return errorEvidenceRedactionMarker + errorEvidenceTruncationMarker
	}
	names := key.Segments()
	for index, name := range names {
		bare := name != ""
		for _, char := range name {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
				bare = false
				break
			}
		}
		if !bare {
			names[index] = Quote(name)
		}
	}
	return humanErrorDetail(strings.Join(names, "."))
}
