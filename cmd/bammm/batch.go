package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/InsightSoftmax/BAMMM/internal/convert"
)

// inputItem is one file to convert. rel is the path relative to the output
// root, so the input tree is mirrored under --output-dir.
type inputItem struct {
	path string // path to read
	rel  string // destination path (before extension swap), relative to output dir
}

// gatherInputs builds the conversion work list from positional args and an
// optional input directory. batch reports whether this run produces multiple
// outputs (any --input-dir, or more than one positional file).
func gatherInputs(args []string, inputDir, pattern string, recursive bool) (items []inputItem, batch bool, err error) {
	for _, a := range args {
		info, statErr := os.Stat(a)
		if statErr != nil {
			return nil, false, fmt.Errorf("%s: %w", a, statErr)
		}
		if info.IsDir() {
			return nil, false, fmt.Errorf("%s is a directory; use --input-dir to convert a directory", a)
		}
		items = append(items, inputItem{path: a, rel: filepath.Base(a)})
	}
	if inputDir != "" {
		dirItems, walkErr := walkInputDir(inputDir, pattern, recursive)
		if walkErr != nil {
			return nil, false, walkErr
		}
		items = append(items, dirItems...)
	}
	batch = inputDir != "" || len(args) > 1
	return items, batch, nil
}

// walkInputDir collects files under dir, filtered by an optional filename glob,
// recording each file's path relative to dir.
func walkInputDir(dir, pattern string, recursive bool) ([]inputItem, error) {
	var items []inputItem
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !recursive && path != filepath.Clean(dir) {
				return fs.SkipDir
			}
			return nil
		}
		if pattern != "" {
			ok, mErr := filepath.Match(pattern, d.Name())
			if mErr != nil {
				return fmt.Errorf("bad --pattern %q: %w", pattern, mErr)
			}
			if !ok {
				return nil
			}
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		items = append(items, inputItem{path: path, rel: rel})
		return nil
	})
	return items, err
}

// batchFailure records one file that could not be converted.
type batchFailure struct {
	rel string
	err string
}

// batchResult summarizes a bulk conversion.
type batchResult struct {
	converted int
	failures  []batchFailure
}

// runBatch converts every item into outDir, mirroring the input tree and
// swapping each file's extension for the target format's. It continues past
// per-file failures, writing a summary to w, and only returns a non-nil error
// for problems that abort the whole run (e.g. an unwritable output dir).
func runBatch(items []inputItem, from, to, outDir string, w io.Writer) (batchResult, error) {
	var res batchResult
	ext := convert.Extension(to)

	sorted := append([]inputItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].rel < sorted[j].rel })

	for _, it := range sorted {
		data, err := os.ReadFile(it.path)
		if err != nil {
			res.failures = append(res.failures, batchFailure{it.rel, err.Error()})
			continue
		}
		out, err := convert.Convert(data, from, to)
		if err != nil {
			res.failures = append(res.failures, batchFailure{it.rel, err.Error()})
			continue
		}
		outPath := filepath.Join(outDir, swapExt(it.rel, ext))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return res, fmt.Errorf("creating %s: %w", filepath.Dir(outPath), err)
		}
		if err := os.WriteFile(outPath, out, 0o644); err != nil {
			return res, fmt.Errorf("writing %s: %w", outPath, err)
		}
		res.converted++
	}

	fmt.Fprintf(w, "converted %d file(s) → %s\n", res.converted, outDir)
	if len(res.failures) > 0 {
		fmt.Fprintf(w, "%d file(s) failed:\n", len(res.failures))
		for _, f := range res.failures {
			fmt.Fprintf(w, "  %s: %s\n", f.rel, f.err)
		}
	}
	return res, nil
}

// swapExt replaces the file extension of rel with newExt.
func swapExt(rel, newExt string) string {
	return strings.TrimSuffix(rel, filepath.Ext(rel)) + newExt
}
