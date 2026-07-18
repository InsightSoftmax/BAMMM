package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/InsightSoftmax/BAMMM/internal/convert"
	"github.com/InsightSoftmax/BAMMM/internal/emitter"
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/all"
	"github.com/InsightSoftmax/BAMMM/internal/parser"
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/all"
)

// version is stamped at build time by GoReleaser:
//
//	-ldflags "-X main.version={{.Version}}"
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "bammm",
		Short:        "BAMMM — Batch Automatic Magic Multiplexing Mechanism",
		Long:         "Convert batch job specifications between scheduler formats via the SPLAT intermediate representation.",
		Version:      version,
		SilenceUsage: true,
	}
	root.AddCommand(newConvertCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newFormatsCmd())
	return root
}

func newConvertCmd() *cobra.Command {
	var from, to, inputFile, inputDir, outputDir, pattern string
	var recursive, report bool

	cmd := &cobra.Command{
		Use:   "convert [file...]",
		Short: "Convert one or many job specs from one format to another",
		Long: `Convert translates job specs through the SPLAT intermediate representation.

Single input: pass one file as an argument, via --input, or pipe to stdin;
the result is written to stdout (or --output-dir if given).

Bulk input: pass multiple files/globs, or --input-dir to convert a whole
directory (recursively, mirroring its tree). Bulk runs require --output-dir,
name each output after its source with the target extension, and continue
past per-file failures, printing a summary and exiting non-zero if any failed.

Use --from splat / --to splat to validate or round-trip without converting.`,
		Example: `  bammm convert --from slurm --to volcano job.sh
  bammm convert --from htcondor --to pbs < job.sub
  bammm convert --from slurm --to kueue jobs/*.sh --output-dir out/
  bammm convert --from slurm --to kueue --input-dir corpus/slurm --output-dir out/`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			items, batch, err := gatherInputs(args, inputDir, pattern, recursive)
			if err != nil {
				return err
			}

			var runErr error
			if !batch {
				runErr = convertSingle(cmd, items, inputFile, from, to, outputDir)
			} else if outputDir == "" {
				return fmt.Errorf("--output-dir is required when converting multiple files or --input-dir")
			} else {
				res, err := runBatch(items, from, to, outputDir, cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				if len(res.failures) > 0 {
					runErr = fmt.Errorf("%d of %d file(s) failed to convert", len(res.failures), res.converted+len(res.failures))
				}
			}

			if report {
				if len(items) == 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "--report needs file inputs (not stdin); skipping report")
				} else {
					writeReport(cmd.OutOrStdout(), items, from)
				}
			}
			return runErr
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "source format (see: bammm formats)")
	cmd.Flags().StringVarP(&to, "to", "t", "", "target format (see: bammm formats)")
	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "input file path (default: stdin or positional arg)")
	cmd.Flags().StringVar(&inputDir, "input-dir", "", "convert every file under this directory")
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "write outputs here (required for bulk); mirrors the input tree")
	cmd.Flags().StringVar(&pattern, "pattern", "", "filename glob to filter --input-dir (e.g. '*.sbatch')")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "recurse into subdirectories of --input-dir")
	cmd.Flags().BoolVar(&report, "report", false, "print a SPLAT field-coverage report over the inputs")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// convertSingle handles the one-input case: read from a file/stdin and write to
// stdout, or to a single named file when --output-dir is set.
func convertSingle(cmd *cobra.Command, items []inputItem, inputFile, from, to, outputDir string) error {
	var args []string
	if len(items) == 1 {
		args = []string{items[0].path}
	}
	data, err := readInput(inputFile, args, cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	out, err := convert.Convert(data, from, to)
	if err != nil {
		return err
	}
	if outputDir == "" {
		_, err = fmt.Fprint(cmd.OutOrStdout(), string(out))
		return err
	}

	name := "out" + convert.Extension(to)
	switch {
	case len(items) == 1:
		name = swapExt(items[0].rel, convert.Extension(to))
	case inputFile != "":
		name = swapExt(filepath.Base(inputFile), convert.Extension(to))
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outputDir, err)
	}
	return os.WriteFile(filepath.Join(outputDir, name), out, 0o644)
}

func newFormatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "formats",
		Short: "List all supported input/output formats",
		Run: func(cmd *cobra.Command, _ []string) {
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Source formats (--from):")
			known := parser.Known()
			if len(known) == 0 {
				fmt.Fprintln(w, "  (none registered yet)")
			} else {
				for _, f := range known {
					fmt.Fprintf(w, "  %s\n", f)
				}
			}
			fmt.Fprintln(w, "Target formats (--to):")
			known = emitter.Known()
			if len(known) == 0 {
				fmt.Fprintln(w, "  (none registered yet)")
			} else {
				for _, f := range known {
					fmt.Fprintf(w, "  %s\n", f)
				}
			}
			fmt.Fprintln(w, "  splat  (pass-through; valid for both --from and --to)")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Planned:", strings.Join([]string{
				"lsf", "htcondor", "flux", "yunikorn", "runai",
			}, ", "))
		},
	}
}

func readInput(inputFile string, args []string, stdin io.Reader) ([]byte, error) {
	switch {
	case inputFile != "":
		return os.ReadFile(inputFile)
	case len(args) == 1:
		return os.ReadFile(args[0])
	default:
		return io.ReadAll(stdin)
	}
}
