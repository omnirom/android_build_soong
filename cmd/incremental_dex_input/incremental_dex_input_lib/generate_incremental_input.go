package incremental_dex_input_lib

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	fid_lib "android/soong/cmd/find_input_delta/find_input_delta_lib"
)

var fileSepRegex = regexp.MustCompile("[^[:space:]]+")

func GenerateIncrementalInput(jarFilePath, outputDir, packageOutputDir, dexTarget, deps string) {
	inputPcState := dexTarget + ".input.pc_state"
	depsPcState := dexTarget + ".deps.pc_state"

	packagePaths := getAllPackages(jarFilePath)
	var chPackagePaths []string
	includeAllPackages := false

	version := ""
	tools := []string{}
	addF, delF, chF := findInputDelta(version, tools, []string{jarFilePath}, inputPcState, dexTarget, true)

	depsList := readRspFile(deps)
	addD, delD, chD := findInputDelta(version, tools, depsList, depsPcState, dexTarget, false)

	// If we are not doing a partial compile, we can just return all packages in the incremental list.
	if !usePartialCompile() {
		includeAllPackages = true
		chPackagePaths = packagePaths
	}

	// Changing the dependencies warrants including all packages.
	if !includeAllPackages && len(addD)+len(delD)+len(chD) > 0 {
		includeAllPackages = true
		chPackagePaths = packagePaths
	}

	if !includeAllPackages {
		chPackageSet := make(map[string]bool)
		chPackagePaths = nil
		// We only want to include all packages when there is a modification to the number of packages present.
		// We loosely simulate that by including all packages when there is change in number of classes in the jar.
		if len(addF) > 0 || len(delF) > 0 {
			includeAllPackages = true
			chPackagePaths = packagePaths
		} else {
			for _, ch := range chF {
				// Filter out the files that do not end with ".class"
				if normalizedPath := getPackagePath(ch); normalizedPath != "" {
					chPackageSet[normalizedPath] = true
				}
			}

			for path := range chPackageSet {
				chPackagePaths = append(chPackagePaths, path)
			}
			sort.Strings(chPackagePaths)
		}
	}

	preparePackagePaths(packageOutputDir, packagePaths)

	writePackagePathsToRspFile(dexTarget+".rsp", packagePaths)
	writePackagePathsToRspFile(dexTarget+".inc.rsp", chPackagePaths)
}

// Reads a rsp file and returns the content
func readRspFile(rspFile string) (list []string) {
	data, err := os.ReadFile(rspFile)
	if err != nil {
		panic(err)
	}
	list = append(list, fileSepRegex.FindAllString(string(data), -1)...)
	return list
}

// Checks if Full Compile is enabled or not
func usePartialCompile() bool {
	usePartialCompileVar := os.Getenv("SOONG_USE_PARTIAL_COMPILE")
	if usePartialCompileVar == "true" {
		return true
	}
	return false
}

// Re-creates the package paths.
func preparePackagePaths(packageOutputDir string, packagePaths []string) {
	// Create package directories relative to packageOutputDir
	for _, pkgPath := range packagePaths {
		targetPath := filepath.Join(packageOutputDir, pkgPath)
		if err := os.MkdirAll(targetPath, 0755); err != nil {
			fmt.Println("err: ", err)
			panic(err)
		}
	}
}

// Returns the list of java packages derived from .class files in a jar.
func getAllPackages(jarFilePath string) []string {
	// Open the JAR file for reading.
	r, err := zip.OpenReader(jarFilePath)
	if err != nil {
		panic(err)
	}
	defer r.Close()

	packageSet := make(map[string]bool)

	// Iterate over each file in the JAR archive.
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if packagePath := getPackagePath(file.Name); packagePath != "" {
			packageSet[packagePath] = true
		}
	}

	packagePaths := make([]string, 0, len(packageSet))
	for path := range packageSet {
		packagePaths = append(packagePaths, path)
	}
	sort.Strings(packagePaths)

	return packagePaths
}

// Returns package path, for files ending with .class
func getPackagePath(file string) string {
	if strings.HasSuffix(file, ".class") {
		dirPath := filepath.Dir(file)
		if dirPath != "." {
			return filepath.ToSlash(dirPath)
		}
		// Return `.` if the class does not have a package, i.e. present at the root
		// of the jar.
		return "."
	}
	return ""
}

// writePathsToRspFile writes a slice of strings to a file, one string per line.
func writePackagePathsToRspFile(filePath string, packagePaths []string) {
	// Join the paths with newline characters.
	// Add a final newline for standard text file format.
	content := strings.Join(packagePaths, "\n") + "\n"

	// Write the content to the file.
	err := os.WriteFile(filePath, []byte(content), 0644) // 0644: rw-r--r--
	if err != nil {
		fmt.Println("failed to write rsp file ", filePath, err)
		panic(err)
	}
}

// Computes the diff of the inputs provided, saving the temp state in the
// priorStateFile.
func findInputDelta(version string, tools, inputs []string, priorStateFile, target string, inspect bool) ([]string, []string, []string) {
	newStateFile := priorStateFile + ".new"
	fileList, err := fid_lib.GenerateFileList(target, priorStateFile, newStateFile, version, tools, inputs, inspect, fid_lib.OsFs)
	if err != nil {
		panic(err)
	}
	return flattenChanges(fileList)
}

// Recursively flattens the output of find_input_delta.
func flattenChanges(root *fid_lib.FileList) ([]string, []string, []string) {
	var allAdditions []string
	var allDeletions []string
	var allChangedFiles []string

	for _, addition := range root.Additions {
		allAdditions = append(allAdditions, addition)
	}

	for _, del := range root.Deletions {
		allDeletions = append(allDeletions, del)
	}

	for _, ch := range root.Changes {
		allChangedFiles = append(allChangedFiles, ch.Name)
		recAdd, recDel, recCh := flattenChanges(&ch)
		allAdditions = append(allAdditions, recAdd...)
		allDeletions = append(allDeletions, recDel...)
		allChangedFiles = append(allChangedFiles, recCh...)
	}

	return allAdditions, allDeletions, allChangedFiles
}
