package main

import (
	"fmt"
	"io"
	"os"
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
	root.AddCommand(newFormatsCmd())
	return root
}

func newConvertCmd() *cobra.Command {
	var from, to, inputFile string

	cmd := &cobra.Command{
		Use:   "convert [file]",
		Short: "Convert a job spec from one format to another",
		Long: `Convert translates a job spec through the SPLAT intermediate representation.

Pass the input file as an argument, via --input, or pipe to stdin.
Use --from splat / --to splat to validate or round-trip without converting.`,
		Example: `  bammm convert --from slurm --to volcano job.sh
  bammm convert --from htcondor --to pbs < job.sub
  bammm convert --from armada --to splat source.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readInput(inputFile, args, cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			out, err := convert.Convert(data, from, to)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(out))
			return err
		},
	}

	cmd.Flags().StringVarP(&from, "from", "f", "", "source format (see: bammm formats)")
	cmd.Flags().StringVarP(&to, "to", "t", "", "target format (see: bammm formats)")
	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "input file path (default: stdin or positional arg)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
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
				"slurm", "pbs", "lsf", "htcondor", "flux",
				"volcano", "kueue", "armada", "yunikorn", "runai",
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
