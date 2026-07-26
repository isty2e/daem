package diagnose

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
)

func okCheck(name string, detail string) findings.Check {
	return findings.OKCheck(name, detail)
}

func warnCheck(name string, detail string) findings.Check {
	return findings.WarnCheck(name, detail)
}

func errorCheck(name string, detail string) findings.Check {
	return findings.ErrorCheck(name, detail)
}

func resourceIDString(id entity.ID) string {
	return fmt.Sprintf("%s/%s", id.Kind(), id.Name())
}
