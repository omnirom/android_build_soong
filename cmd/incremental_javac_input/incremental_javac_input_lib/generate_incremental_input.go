// Copyright (C) 2025 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package incremental_javac_input_lib

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	fid_lib "android/soong/cmd/find_input_delta/find_input_delta_lib"
	"github.com/google/blueprint/pathtools"
	dependency_proto "go.dependencymapper/protoimpl"
	"google.golang.org/protobuf/proto"
)

var fileSepRegex = regexp.MustCompile("[^[:space:]]+")

type UsageMap struct {
	FilePath string

	Usages []string

	IsDependencyToAll bool

	GeneratedClasses []string

	CrossModuleClassDependencies []string
}

func GenerateIncrementalInput(classDir, srcs, deps, javacTarget, srcDeps, localHeaderJars, crossModuleJarRsp string) (err error) {
	incInputPath := javacTarget + ".inc.rsp"
	removedClassesPath := javacTarget + ".rem.rsp"
	inputPcState := javacTarget + ".input.pc_state"
	depsPcState := javacTarget + ".deps.pc_state"
	headersPcState := javacTarget + ".headers.pc_state"
	crossModuleDepsPcState := javacTarget + ".crossModuleDeps.pc_state"

	var classesForRemoval []string
	var incAllSources bool

	version := ""
	tools := []string{}
	// Read the srcRspFile contents
	srcList := readRspFile(srcs)
	// run find_input_delta, save [add + ch] as a []string,  and [del] as another []string
	addF, delF, chF := findInputDelta(version, tools, srcList, inputPcState, javacTarget)
	var incInputList []string
	incInputList = append(incInputList, addF...)
	incInputList = append(incInputList, chF...)

	// check if deps have changed
	depsList := readRspFile(deps)
	if addD, delD, chD := findInputDelta(version, tools, depsList, depsPcState, javacTarget); len(addD)+len(delD)+len(chD) > 0 {
		incAllSources = true
	}

	// If we are not doing a partial compile, we can just return the full list.
	// We can do this earlier as well, but allowing findInputDelta to run outputs
	// the changed sources to build metrics as well as maintains the state of
	// inputs we require if the partialCompile is switched on again.
	if !usePartialCompile() {
		// Remove the output class directory to prevent any stale files
		os.RemoveAll(classDir)
		return writeOutput(incInputPath, removedClassesPath, srcList, classesForRemoval)
	}

	// if the output directory of javac which will contain .class files is not present, include all sources
	if !incAllSources && !dirExists(classDir) {
		incAllSources = true
	}

	// if javacTarget does not exist, we can include all sources
	if !incAllSources && !fileExists(javacTarget) {
		incAllSources = true
	}

	// if incInputList has the same size as srcList (all files touched), we can
	// just include all sources
	if !incAllSources && len(incInputList) == len(srcList) {
		incAllSources = true
	}

	// if dependencyMap does not exist, we can include all sources
	if !incAllSources && !fileExists(srcDeps) {
		incAllSources = true
	}

	headersChanged := false
	// if headers do not change, we can just keep the incInputList as is.
	// Read the srcRspFile contents
	headersList := readRspFile(localHeaderJars)
	if addH, delH, chH := findInputDelta(version, tools, headersList, headersPcState, javacTarget); len(addH)+len(delH)+len(chH) > 0 {
		headersChanged = true
	}

	var addCM, delCM, chCM []string
	if fileExists(crossModuleJarRsp) {
		crossModuleDepsList := readRspFile(crossModuleJarRsp)
		addCM, delCM, chCM = findInputContentsDelta(version, tools, crossModuleDepsList, crossModuleDepsPcState, javacTarget)
		addCM = filterClassFiles(addCM)
		delCM = filterClassFiles(delCM)
		chCM = filterClassFiles(chCM)
	}

	// use revDepsMap to find all usages, add them to output, alongside [add + ch] files
	if fileExists(srcDeps) {
		usageMap, _ := generateUsageMap(srcDeps)
		// if including all sources, no need to check the usageMap
		if headersChanged && !incAllSources {
			incInputList, incAllSources = getUsages(usageMap, incInputList, delF, slices.Concat(addCM, chCM), delCM)
		}
		// use usageMap to add all classes that were generated from removed files.
		classesForRemoval = generateRemovalList(usageMap, delF, classDir)
	}

	if incAllSources {
		incInputList = srcList
	}

	// write the output to output path(s)
	return writeOutput(incInputPath, removedClassesPath, incInputList, classesForRemoval)
}

// Returns strings ending in .class
func filterClassFiles(filenames []string) []string {
	var classFiles []string
	for _, filename := range filenames {
		// Convert the filename to lowercase for case-insensitive comparison
		if strings.HasSuffix(filename, ".class") {
			classFiles = append(classFiles, filename)
		}
	}
	return classFiles
}

// Checks if Full Compile is enabled or not
func usePartialCompile() bool {
	usePartialCompileVar := os.Getenv("SOONG_USE_PARTIAL_COMPILE")
	if usePartialCompileVar == "true" {
		return true
	}
	return false
}

// Returns the list of files that use added, modified, or deleted files.
// Returns whether to include all src Files in incremental src set
func getUsages(usageMap map[string]UsageMap, modifiedFiles, deletedFiles, modifiedClasses, deletedClasses []string) ([]string, bool) {
	usagesSet := make(map[string]bool)

	// First add all the modified files in the output
	for _, incInput := range modifiedFiles {
		usagesSet[incInput] = true
	}
	// Add all the usages of modified + deleted files
	for _, modFile := range slices.Concat(modifiedFiles, deletedFiles, modifiedClasses, deletedClasses) {
		if um, exists := usageMap[modFile]; exists {
			if um.IsDependencyToAll {
				return nil, true
			}
			for _, usage := range um.Usages {
				usagesSet[usage] = true
			}
		}
	}

	var usages []string
	for usage := range usagesSet {
		usages = append(usages, usage)
	}
	return usages, false
}

// Returns the list of class files to be removed, as a result of deleting a source file.
func generateRemovalList(usageMap map[string]UsageMap, delFiles []string, classesDir string) []string {
	var classesForRemoval []string
	for _, delFile := range delFiles {
		if _, exists := usageMap[delFile]; exists {
			for _, generatedClass := range usageMap[delFile].GeneratedClasses {
				classesForRemoval = append(classesForRemoval, filepath.Join(classesDir, generatedClass))
			}
		}
	}
	return classesForRemoval
}

// Generates the usage map, by reading the supplied dependency map as a proto
// Throws error if the map is unparsable.
func generateUsageMap(srcDeps string) (map[string]UsageMap, error) {
	var message = &dependency_proto.FileDependencyList{}

	usageMapSet := make(map[string]UsageMap)

	data, err := os.ReadFile(srcDeps)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		fmt.Println("err: ", err)
		panic(err)
	}
	err = proto.Unmarshal(data, message)
	if err != nil {
		fmt.Println("err: ", err)
		panic(err)
	}
	for _, dep := range message.FileDependency {
		addUsageMapIfNotPresent(usageMapSet, *dep.FilePath)
		for _, depV := range dep.FileDependencies {
			addUsageMapIfNotPresent(usageMapSet, depV)
			updatedUsageMap := usageMapSet[depV]
			updatedUsageMap.Usages = append(updatedUsageMap.Usages, *dep.FilePath)
			usageMapSet[depV] = updatedUsageMap
		}
		// Mixing files and classes in usageMapSet. Is this ok?
		for _, depV := range dep.CrossModuleClassDeps {
			addUsageMapIfNotPresent(usageMapSet, depV)
			updatedUsageMap := usageMapSet[depV]
			updatedUsageMap.Usages = append(updatedUsageMap.Usages, *dep.FilePath)
			usageMapSet[depV] = updatedUsageMap
		}
		updatedUsageMap := usageMapSet[*dep.FilePath]
		updatedUsageMap.IsDependencyToAll = *dep.IsDependencyToAll
		updatedUsageMap.GeneratedClasses = dep.GeneratedClasses
		updatedUsageMap.CrossModuleClassDependencies = dep.CrossModuleClassDeps
		usageMapSet[*dep.FilePath] = updatedUsageMap
	}
	return usageMapSet, nil
}

func addUsageMapIfNotPresent(usageMapSet map[string]UsageMap, key string) {
	if _, exists := usageMapSet[key]; !exists {
		usageMap := UsageMap{
			FilePath:                     key,
			Usages:                       []string{},
			IsDependencyToAll:            false,
			GeneratedClasses:             []string{},
			CrossModuleClassDependencies: []string{},
		}
		usageMapSet[key] = usageMap
	}
}

func fileExists(filePath string) bool {
	if file, err := os.Open(filePath); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err)
	} else {
		file.Close()
	}
	return true
}

func dirExists(dirPath string) bool {
	if _, err := os.Stat(dirPath); err == nil || !os.IsNotExist(err) {
		return true
	}
	return false
}

func readRspFile(rspFile string) (list []string) {
	data, err := os.ReadFile(rspFile)
	if err != nil {
		panic(err)
	}
	list = append(list, fileSepRegex.FindAllString(string(data), -1)...)
	return list
}

// Writes incInput and classesForRemoval to output paths
func writeOutput(incInputPath, removedClassesPath string, incInputList, classesForRemoval []string) (err error) {
	err = pathtools.WriteFileIfChanged(incInputPath, []byte(strings.Join(incInputList, "\n")), 0644)
	if err != nil {
		return err
	}
	return pathtools.WriteFileIfChanged(removedClassesPath, []byte(strings.Join(classesForRemoval, "\n")), 0644)
}

// Computes the diff of the inputs provided, saving the temp state in the
// priorStateFile.
func findInputDelta(version string, tools, inputs []string, priorStateFile, target string) ([]string, []string, []string) {
	return findInputDeltaInternal(version, tools, inputs, priorStateFile, target, false)
}

// Computes the diff of the contents of the inputs provided, saving the temp state in the
// priorStateFile.
func findInputContentsDelta(version string, tools, inputs []string, priorStateFile, target string) ([]string, []string, []string) {
	return findInputDeltaInternal(version, tools, inputs, priorStateFile, target, true)
}

// Computes the diff of the inputs provided, saving the temp state in the
// priorStateFile.
func findInputDeltaInternal(version string, tools, inputs []string, priorStateFile, target string, inspect bool) ([]string, []string, []string) {
	newStateFile := priorStateFile + ".new"
	fileList, err := fid_lib.GenerateFileList(target, priorStateFile, newStateFile, version, tools, inputs, inspect, fid_lib.OsFs)
	if err != nil {
		panic(err)
	}
	return flattenChanges(fileList)
}

// Flattens the output of find_input_delta for javac's consumption.
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
