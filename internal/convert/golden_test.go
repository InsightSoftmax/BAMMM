package convert_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/InsightSoftmax/BAMMM/internal/convert"

	// Register every parser and emitter so Convert can resolve all formats.
	_ "github.com/InsightSoftmax/BAMMM/internal/emitter/all"
	_ "github.com/InsightSoftmax/BAMMM/internal/parser/all"
)

// update regenerates the golden files: go test ./internal/convert -update
var update = flag.Bool("update", false, "regenerate golden files")

// repoRoot is the path from this package's directory to the repository root.
const repoRoot = "../.."

type goldenCase struct {
	name   string
	from   string
	to     string
	source string // path relative to repo root
}

// goldenCases covers every conversion whose source and target formats BAMMM can
// currently handle. The golden files are snapshots of the tool's own output —
// they lock behavior against regressions. The hand-crafted conversions/*/target.*
// files remain human documentation, not test oracles.
var goldenCases = []goldenCase{
	{"slurm_to_splat", "slurm", "splat", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_slurm", "slurm", "slurm", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_kueue", "slurm", "kueue", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_armada", "slurm", "armada", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_volcano", "slurm", "volcano", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_pbs", "slurm", "pbs", "conversions/01-slurm-to-volcano/source.sh"},
	{"slurm_to_yunikorn", "slurm", "yunikorn", "conversions/01-slurm-to-volcano/source.sh"},
	{"armada_to_yunikorn", "armada", "yunikorn", "conversions/05-armada-to-slurm/source.yaml"},

	{"pbs_to_splat", "pbs", "splat", "conversions/03-htcondor-to-pbs/target.sh"},
	{"pbs_to_pbs", "pbs", "pbs", "conversions/03-htcondor-to-pbs/target.sh"},
	{"pbs_to_slurm", "pbs", "slurm", "conversions/03-htcondor-to-pbs/target.sh"},

	{"htcondor_to_splat", "htcondor", "splat", "conversions/03-htcondor-to-pbs/source.sub"},
	{"htcondor_to_htcondor", "htcondor", "htcondor", "conversions/03-htcondor-to-pbs/source.sub"},
	{"htcondor_to_pbs", "htcondor", "pbs", "conversions/03-htcondor-to-pbs/source.sub"},
	{"htcondor_to_slurm", "htcondor", "slurm", "conversions/03-htcondor-to-pbs/source.sub"},

	{"volcano_to_splat", "volcano", "splat", "conversions/02-volcano-to-slurm/source.yaml"},
	{"volcano_to_slurm", "volcano", "slurm", "conversions/02-volcano-to-slurm/source.yaml"},
	{"volcano_to_volcano", "volcano", "volcano", "conversions/02-volcano-to-slurm/source.yaml"},
	{"volcano_to_kueue", "volcano", "kueue", "conversions/02-volcano-to-slurm/source.yaml"},

	{"armada_to_splat", "armada", "splat", "conversions/05-armada-to-slurm/source.yaml"},
	{"armada_to_slurm", "armada", "slurm", "conversions/05-armada-to-slurm/source.yaml"},
	{"armada_to_volcano", "armada", "volcano", "conversions/05-armada-to-slurm/source.yaml"},
	{"armada_to_kueue", "armada", "kueue", "conversions/05-armada-to-slurm/source.yaml"},
}

func TestGolden(t *testing.T) {
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(repoRoot, c.source))
			if err != nil {
				t.Fatalf("read source: %v", err)
			}
			got, err := convert.Convert(src, c.from, c.to)
			if err != nil {
				t.Fatalf("convert %s->%s: %v", c.from, c.to, err)
			}

			golden := filepath.Join("testdata", "golden", c.name+convert.Extension(c.to))
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (regenerate with: go test ./internal/convert -update): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s: output changed vs golden.\nRegenerate with: go test ./internal/convert -update\n--- got ---\n%s", c.name, got)
			}
		})
	}
}
