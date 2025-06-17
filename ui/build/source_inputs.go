package build

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"android/soong/shared"
	"android/soong/ui/metrics"
)

func sortedStringSetKeys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func addSlash(str string) string {
	if len(str) == 0 {
		return ""
	}
	if str[len(str)-1] == '/' {
		return str
	}
	return str + "/"
}

func hasPrefixStrings(str string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(str, prefix) {
			return true
		}
	}
	return false
}

// Output DIST_DIR/source_inputs.txt.gz, which will contain a listing of the files
// in the source tree (not including in the out directory) that were declared as ninja
// inputs to the build that was just done.
func runSourceInputs(ctx Context, config Config) {
	ctx.BeginTrace(metrics.RunSoong, "runSourceInputs")
	defer ctx.EndTrace()

	success := false
	outputFilename := shared.JoinPath(config.RealDistDir(), "source_inputs.txt.gz")

	outputFile, err := os.Create(outputFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "source_files_used: unable to open file for write: %s\n", outputFilename)
		return
	}
	defer func() {
		outputFile.Close()
		if !success {
			os.Remove(outputFilename)
		}
	}()

	output := gzip.NewWriter(outputFile)
	defer output.Close()

	// Skip out dir, both absolute and relative. There are some files
	// generated during analysis that ninja thinks are inputs not intermediates.
	absOut, _ := filepath.Abs(config.OutDir())
	excludes := []string{
		addSlash(config.OutDir()),
		addSlash(absOut),
	}

	goals := config.NinjaArgs()

	result := make(map[string]bool)
	for _, goal := range goals {
		inputs, err := runNinjaInputs(ctx, config, goal)
		if err != nil {
			fmt.Fprintf(os.Stderr, "source_files_used: %v\n", err)
			return
		}

		for _, filename := range inputs {
			if !hasPrefixStrings(filename, excludes) {
				result[filename] = true
			}
		}
	}

	for _, filename := range sortedStringSetKeys(result) {
		output.Write([]byte(filename))
		output.Write([]byte("\n"))
	}

	output.Flush()
	success = true
}
