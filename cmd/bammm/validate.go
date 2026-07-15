package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/InsightSoftmax/BAMMM/internal/convert"
)

func newValidateCmd() *cobra.Command {
	var from, to, inputFile, inputDir, pattern string
	var recursive bool

	cmd := &cobra.Command{
		Use:   "validate [file...]",
		Short: "Check that job specs parse (and optionally convert) without errors",
		Long: `Validate parses each input as --from and reports whether it is a well-formed
spec BAMMM can ingest. With --to, it also runs the full conversion, so a spec
only passes if it both parses and emits cleanly for that target.

Accepts a single file/stdin, multiple files/globs, or --input-dir (recursive).
Bulk runs continue past failures, print a summary, and exit non-zero if any
spec is invalid.`,
		Example: `  bammm validate --from slurm job.sh
  bammm validate --from slurm --to kueue job.sh
  bammm validate --from slurm --input-dir corpus/slurm --pattern '*.sh'`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			items, batch, err := gatherInputs(args, inputDir, pattern, recursive)
			if err != nil {
				return err
			}

			if !batch {
				return validateSingle(cmd, items, inputFile, from, to)
			}
			res := runValidate(items, from, to, cmd.OutOrStdout())
			if res.invalid > 0 {
				return fmt.Errorf("%d of %d spec(s) invalid", res.invalid, res.valid+res.invalid)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "source format (see: bammm formats)")
	cmd.Flags().StringVarP(&to, "to", "t", "", "also validate conversion to this target format")
	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "input file path (default: stdin or positional arg)")
	cmd.Flags().StringVar(&inputDir, "input-dir", "", "validate every file under this directory")
	cmd.Flags().StringVar(&pattern, "pattern", "", "filename glob to filter --input-dir (e.g. '*.sbatch')")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "recurse into subdirectories of --input-dir")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

// validateBytes returns nil if data parses as `from` (and, when `to` is set,
// also converts to it).
func validateBytes(data []byte, from, to string) error {
	if to == "" {
		_, err := convert.Parse(data, from)
		return err
	}
	_, err := convert.Convert(data, from, to)
	return err
}

func validateSingle(cmd *cobra.Command, items []inputItem, inputFile, from, to string) error {
	var args []string
	if len(items) == 1 {
		args = []string{items[0].path}
	}
	data, err := readInput(inputFile, args, cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	if err := validateBytes(data, from, to); err != nil {
		return fmt.Errorf("invalid: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "valid")
	return nil
}

type validateResult struct {
	valid   int
	invalid int
}

// runValidate validates every item and writes a summary to w.
func runValidate(items []inputItem, from, to string, w io.Writer) validateResult {
	var res validateResult
	type failure struct{ rel, err string }
	var failures []failure

	sorted := append([]inputItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].rel < sorted[j].rel })

	for _, it := range sorted {
		data, err := os.ReadFile(it.path)
		if err == nil {
			err = validateBytes(data, from, to)
		}
		if err != nil {
			res.invalid++
			failures = append(failures, failure{it.rel, err.Error()})
			continue
		}
		res.valid++
	}

	fmt.Fprintf(w, "valid: %d/%d\n", res.valid, res.valid+res.invalid)
	for _, f := range failures {
		fmt.Fprintf(w, "  INVALID %s: %s\n", f.rel, f.err)
	}
	return res
}
