// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"os"
	"strings"

	cli "github.com/flocko-motion/schemaf/cli"
)

// emptyInputsFile lists the empty inputs of the last `codegen`, one
// warning per line; build (schemaf.sh) prints them. Missing or empty = nothing
// empty. See EXTEND.md › Code Generation › Empty inputs.
const emptyInputsFile = "gen/empty-inputs"

// emptyInputs collects the warnings of the current codegen run.
var emptyInputs []string

// warnEmptyInput prints the warning for an empty input and records it for
// gen/empty-inputs. Empty input is valid: never an error, never a stop.
func warnEmptyInput(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	cli.Warning("%s", msg)
	emptyInputs = append(emptyInputs, msg)
}

// runEmptyInputsGen writes gen/empty-inputs from the warnings recorded since
// the start of the run, and resets the record.
func runEmptyInputsGen() error {
	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}
	var content strings.Builder
	for _, msg := range emptyInputs {
		content.WriteString(msg + "\n")
	}
	emptyInputs = nil
	if err := os.WriteFile(emptyInputsFile, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", emptyInputsFile, err)
	}
	cli.Success("Generated %s", emptyInputsFile)
	return nil
}
