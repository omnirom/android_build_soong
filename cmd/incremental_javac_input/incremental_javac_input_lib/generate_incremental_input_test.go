package incremental_javac_input_lib

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	fid_lib "android/soong/cmd/find_input_delta/find_input_delta_lib"
	dependency_proto "go.dependencymapper/protoimpl"
	"google.golang.org/protobuf/proto"
)

// --- Tests for `getUsages` ---
func TestGetUsages(t *testing.T) {
	usageMap := map[string]UsageMap{
		"file1.java":   {Usages: []string{"file3.java", "file4.java"}},
		"file2.java":   {Usages: []string{"file3.java"}},
		"file3.java":   {Usages: []string{}},
		"fileAll.java": {Usages: []string{}, IsDependencyToAll: true},
		"FooClass":     {Usages: []string{"file1.java", "file4.java"}},
		"BarClass":     {Usages: []string{"file3.java"}},
		"BazClass":     {Usages: []string{}},
	}

	testCases := []struct {
		name            string
		modifiedFiles   []string
		deletedFiles    []string
		modifiedClasses []string
		deletedClasses  []string
		expected        []string
		expectedAll     bool
	}{
		{
			name:            "Basic",
			modifiedFiles:   []string{"file1.java"},
			deletedFiles:    []string{"file2.java"},
			modifiedClasses: []string{},
			deletedClasses:  []string{},
			expected:        []string{"file1.java", "file3.java", "file4.java"}, // file3 is used by both
			expectedAll:     false,
		},
		{
			name:            "Empty",
			modifiedFiles:   []string{},
			deletedFiles:    []string{},
			modifiedClasses: []string{},
			deletedClasses:  []string{},
			expected:        nil,
			expectedAll:     false,
		},
		{
			name:            "NonExistentFile",
			modifiedFiles:   []string{"nonexistent.java"},
			deletedFiles:    []string{},
			modifiedClasses: []string{},
			deletedClasses:  []string{},
			expected:        []string{"nonexistent.java"},
			expectedAll:     false,
		},
		{
			name:            "DependencyToAll",
			modifiedFiles:   []string{"file1.java"},
			deletedFiles:    []string{"fileAll.java"},
			modifiedClasses: []string{},
			deletedClasses:  []string{},
			expected:        nil,
			expectedAll:     true,
		},
		{
			name:            "SingleClassChange",
			modifiedFiles:   []string{},
			deletedFiles:    []string{},
			modifiedClasses: []string{"FooClass"},
			deletedClasses:  []string{},
			expected:        []string{"file1.java", "file4.java"},
			expectedAll:     false,
		},
		{
			name:            "TwoClassChange",
			modifiedFiles:   []string{},
			deletedFiles:    []string{},
			modifiedClasses: []string{"FooClass", "BarClass"},
			deletedClasses:  []string{},
			expected:        []string{"file1.java", "file3.java", "file4.java"},
			expectedAll:     false,
		},
		{
			name:            "DeleteClassChange",
			modifiedFiles:   []string{},
			deletedFiles:    []string{},
			modifiedClasses: []string{"BarClass"},
			deletedClasses:  []string{},
			expected:        []string{"file3.java"},
			expectedAll:     false,
		},
		{
			name:            "NoDependenciesClassChange",
			modifiedFiles:   []string{},
			deletedFiles:    []string{},
			modifiedClasses: []string{"BazClass"},
			deletedClasses:  []string{},
			expected:        nil,
			expectedAll:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, all := getUsages(usageMap, tc.modifiedFiles, tc.deletedFiles, tc.modifiedClasses, tc.deletedClasses)
			if all != tc.expectedAll {
				t.Errorf("getUsages() all sources; expected %v, got %v", tc.expectedAll, all)
			}

			// Sort for consistent comparison, as map iteration order isn't guaranteed
			sort.Strings(actual)
			sort.Strings(tc.expected)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("getUsages(); expected %v, got %v", tc.expected, actual)
			}
		})
	}
}

// --- Tests for `generateRemovalList` ---
func TestGenerateRemovalList(t *testing.T) {
	usageMap := map[string]UsageMap{
		"file1.java": {GeneratedClasses: []string{"Class1", "Class2"}},
		"file2.java": {GeneratedClasses: []string{"Class3"}},
		"file3.java": {},
	}

	testCases := []struct {
		name     string
		classDir string
		delFiles []string
		expected []string
	}{
		{"Basic", "out/classes", []string{"file1.java", "file2.java"}, []string{"out/classes/Class1", "out/classes/Class2", "out/classes/Class3"}},
		{"Empty", "out/classes", []string{}, nil},
		{"NonExistent", "out/classes", []string{"nonexistent.java"}, nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := generateRemovalList(usageMap, tc.delFiles, tc.classDir)
			sort.Strings(actual)
			sort.Strings(tc.expected)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("generateRemovalList(); expected %v, got %v", tc.expected, actual)
			}
		})
	}
}

// --- Tests for `generateUsageMap` ---
func TestGenerateUsageMap(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Test with a valid proto file.
	protoFile := filepath.Join(tmpDir, "deps.pb")
	createProtoFile(t, protoFile)

	usageMap, err := generateUsageMap(protoFile)
	if err != nil {
		t.Fatalf("generateUsageMap() returned an error: %v", err)
	}

	expectedUsageMap := map[string]UsageMap{
		"file1.java": {FilePath: "file1.java", Usages: []string{}, IsDependencyToAll: false, GeneratedClasses: []string{"ClassA", "ClassB"}},
		"file2.java": {FilePath: "file2.java", Usages: []string{"file1.java"}, IsDependencyToAll: true, GeneratedClasses: []string{"ClassC"}},
		"file3.java": {FilePath: "file3.java", Usages: []string{"file1.java"}, IsDependencyToAll: false, GeneratedClasses: []string{"ClassD"}},
	}

	if !reflect.DeepEqual(usageMap, expectedUsageMap) {
		t.Errorf("generateUsageMap() returned unexpected map.\nGot: %+v\nWant:%+v", usageMap, expectedUsageMap)
	}

	// 2. Test with a non-existent file (should panic)
	t.Run("Panic on non-existent proto", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil { //should have panicked
				t.Errorf("generateUsageMap() did not panic on non-existent proto")
			}
		}()
		_, _ = generateUsageMap("nonexistent.pb")
	})

	// 3. Test with an invalid proto file (should panic)
	invalidProtoFile := filepath.Join(tmpDir, "invalid.pb")
	writeFile(t, invalidProtoFile, "This is not a valid proto file") // Create invalid file
	t.Run("Panic on invalid proto file", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil { //should have panicked
				t.Errorf("generateUsageMap() did not panic on invalid proto")
			}
		}()
		_, _ = generateUsageMap(invalidProtoFile)
	})
}

// --- Tests for `readRspFile` ---
func TestReadRspFile(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name     string
		content  string
		expected []string
	}{
		{"Empty", "", nil},
		{"SingleLine", "file1.java", []string{"file1.java"}},
		{"MultipleLines", "file1.java\nfile2.java\nfile3.java", []string{"file1.java", "file2.java", "file3.java"}},
		{"WithSpaces", "  file1.java  \n file2.java ", []string{"file1.java", "file2.java"}}, //Should be trimmed.
		{"WithEmptyLines", "file1.java\n\nfile2.java", []string{"file1.java", "file2.java"}},
		{"WithCarriageReturn", "file1.java\r\nfile2.java", []string{"file1.java", "file2.java"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rspFile := filepath.Join(tmpDir, "test.rsp")
			writeFile(t, rspFile, tc.content)

			actual := readRspFile(rspFile)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("readRspFile(); expected %v, got %v", tc.expected, actual)
			}
		})
	}

	//Test for panic
	t.Run("Panic on non-existent file", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil { //should have panicked
				t.Errorf("readRspFile() did not panic on non-existent file")
			}
		}()
		_ = readRspFile("nonexistent_file.rsp")
	})
}

// --- Tests for `flattenChanges` ---

func TestFlattenChanges(t *testing.T) {
	fileList := &fid_lib.FileList{
		Additions: []string{"add1.java", "add2.java"},
		Deletions: []string{"del1.java"},
		Changes: []fid_lib.FileList{
			{Name: "change1.java"},
			{Name: "change2.java"},
		},
	}

	expectedAdditions := []string{"add1.java", "add2.java"}
	expectedDeletions := []string{"del1.java"}
	expectedChanges := []string{"change1.java", "change2.java"}

	actualAdditions, actualDeletions, actualChanges := flattenChanges(fileList)

	// Sort for consistent comparison
	sort.Strings(expectedAdditions)
	sort.Strings(actualAdditions)
	sort.Strings(expectedDeletions)
	sort.Strings(actualDeletions)
	sort.Strings(expectedChanges)
	sort.Strings(actualChanges)

	if !reflect.DeepEqual(actualAdditions, expectedAdditions) {
		t.Errorf("flattenChanges() additions; expected %v, got %v", expectedAdditions, actualAdditions)
	}
	if !reflect.DeepEqual(actualDeletions, expectedDeletions) {
		t.Errorf("flattenChanges() deletions; expected %v, got %v", expectedDeletions, actualDeletions)
	}
	if !reflect.DeepEqual(actualChanges, expectedChanges) {
		t.Errorf("flattenChanges() changes; expected %v, got %v", expectedChanges, actualChanges)
	}
}

// --- Tests for `GenerateIncrementalInput` ---

func TestGenerateIncrementalInput(t *testing.T) {
	// Set the environment variable to enable inc-compilation
	t.Setenv("SOONG_USE_PARTIAL_COMPILE", "true")

	// Shared setup for all subtests
	tf := newTestFixture(t)
	// No need for top-level defer os.RemoveAll(tmpDir) because t.TempDir() handles it

	// --- Subtest: Initial Full Compile ---
	t.Run("InitialFullCompile", func(t *testing.T) {
		// Arrange (already done by newTestFixture)

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s\n%s", tf.JavaFile1, tf.JavaFile2, tf.JavaFile3), // All files included initially
			tf.remOutputPath(),
			"", // No removals initially
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One File Modified ---
	t.Run("Incremental_OneFileModified", func(t *testing.T) {
		// Arrange: Modify one file (ensure timestamp changes)
		modifyFile(t, tf.JavaFile3, "Incremental_OneFileModified")

		// Act
		tf.runGenerator()

		// Assert: Only the modified file should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s", tf.JavaFile3),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One File and Header Modified ---
	t.Run("Incremental_FileAndHeaderModified", func(t *testing.T) {
		// Arrange: Modify a different file and the header jar
		modifyFile(t, tf.JavaFile3, "Incremental_FileAndHeaderModified")
		modifyFile(t, tf.HeaderJar, "Incremental_FileAndHeaderModified")

		// Act
		tf.runGenerator()

		// Assert: All source files and their usages should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", tf.JavaFile1, tf.JavaFile3),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - Dependency Change ---
	t.Run("Incremental_DependencyChanged", func(t *testing.T) {
		// Arrange: Modify the DepJar
		modifyFile(t, tf.DepJar, "Incremental_DependencyChanged")

		// Act
		tf.runGenerator()

		// Assert: All source files should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s\n%s", tf.JavaFile1, tf.JavaFile2, tf.JavaFile3),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - File which is dependency to all files is Changed ---
	t.Run("Incremental_DependencyToAllChanged", func(t *testing.T) {
		// Arrange: Modify the DepsRspFile or JavaSrcDeps proto
		modifyFile(t, tf.JavaFile2, "Incremental_DependencyToAllChanged")

		// Act
		tf.runGenerator()

		// Assert: All source files should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s", tf.JavaFile2),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - File which is dependency to all files is changed along with headers---
	t.Run("Incremental_DependencyToAllChangedWithHeaders", func(t *testing.T) {
		// Arrange: Modify the DepsRspFile or JavaSrcDeps proto
		modifyFile(t, tf.JavaFile2, "Incremental_DependencyToAllChangedWithHeader")
		modifyFile(t, tf.HeaderJar, "Incremental_DependencyToAllChangedWithHeader")

		// Act
		tf.runGenerator()

		// Assert: All source files should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s\n%s", tf.JavaFile1, tf.JavaFile2, tf.JavaFile3),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One File Deleted, Header Modified ---
	t.Run("Incremental_FileDeletedHeaderModified", func(t *testing.T) {
		// Arrange: Delete one file and modify header
		deleteFile(t, tf.JavaFile3, tf.SrcRspFile)
		// Modify Headers
		modifyFile(t, tf.HeaderJar, "Incremental_FileDeletedHeaderModified")

		// Act
		tf.runGenerator()

		// Assert: Check usages of deleted file in inc.rsp, and class files
		// corresponding to deleted files in rem.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s", tf.JavaFile1), // usages of deleted file
			tf.remOutputPath(),
			filepath.Join(tf.ClassDir, "org.another.ClassD"), // class name corresponding to the deleted file path
		)
		tf.savePriorState() // Save state if needed for subsequent tests
	})

	// --- Subtest: Incremental - CrossModuleDeps modified ---
	t.Run("Incremental_CrossModuleDepsModified", func(t *testing.T) {
		// Arrange: Modify cross-module jar.
		// There is no way to modify zip files directly. Just move it somewhere and recreate it.
		err := os.Rename(tf.CrossModuleJar, tf.CrossModuleJar+".tmp")
		defer os.Rename(tf.CrossModuleJar+".tmp", tf.CrossModuleJar)
		if err != nil {
			t.Fatalf("Failed to move file %q: %v", tf.CrossModuleJar, err)
		}
		writeZip(t, tf.CrossModuleJar, tf.CrossModuleClass, "changed content")

		// Changing a cross-module jar implies a change in headers
		modifyFile(t, tf.HeaderJar, "Incremental_CrossModuleDepsModified")

		// Act
		tf.runGenerator()

		// Assert: Check usages of deleted file in inc.rsp, and class files
		// corresponding to deleted files in rem.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s", tf.JavaFile1), // usages of modified class
			tf.remOutputPath(),
			"",
		)
		tf.savePriorState() // Save state if needed for subsequent tests
	})
}

func TestGenerateIncrementalInputPartialCompileOff(t *testing.T) {
	// Set the environment variable to disable inc-compilation
	t.Setenv("SOONG_USE_PARTIAL_COMPILE", "")

	// Shared setup for all subtests
	tf := newTestFixture(t)
	// No need for top-level defer os.RemoveAll(tmpDir) because t.TempDir() handles it

	// --- Subtest: Initial Full Compile ---
	t.Run("InitialFullCompile", func(t *testing.T) {
		// Arrange (already done by newTestFixture)

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s\n%s", tf.JavaFile1, tf.JavaFile2, tf.JavaFile3), // All files included initially
			tf.remOutputPath(),
			"", // No removals initially
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One File Modified, should add all files ---
	t.Run("Incremental_OneFileModified_AddsAllFiles", func(t *testing.T) {
		// Arrange: Modify one file (ensure timestamp changes)
		modifyFile(t, tf.JavaFile3, "Incremental_OneFileModified")

		// Act
		tf.runGenerator()

		// Assert: Only the modified file should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s\n%s", tf.JavaFile1, tf.JavaFile2, tf.JavaFile3),
			tf.remOutputPath(),
			"", // No removals
		)
		tf.savePriorState()
	})
}

// --- Test Fixture Setup ---
// Struct to hold common test file paths
type testFixture struct {
	t                      *testing.T
	tmpDir                 string
	ClassDir               string
	SrcRspFile             string
	DepsRspFile            string
	JavacTargetJar         string
	JavaSrcDeps            string
	HeadersRspFile         string
	CrossModuleDepsRspFile string
	JavaFile1              string
	JavaFile2              string
	JavaFile3              string
	DepJar                 string
	HeaderJar              string
	CrossModuleJar         string
	CrossModuleClass       string
}

// newTestFixture creates the temporary directory and necessary files
func newTestFixture(t *testing.T) *testFixture {
	tmpDir := t.TempDir() // Use t.TempDir for automatic cleanup

	// Create dummy files needed for the tests
	fixture := &testFixture{
		t:                      t,
		tmpDir:                 tmpDir,
		ClassDir:               filepath.Join(tmpDir, "classes"),
		SrcRspFile:             filepath.Join(tmpDir, "sources.rsp"),
		DepsRspFile:            filepath.Join(tmpDir, "deps.rsp"),
		JavacTargetJar:         filepath.Join(tmpDir, "output.jar"),
		JavaSrcDeps:            filepath.Join(tmpDir, "srcdeps.pb"), // Example proto file path
		HeadersRspFile:         filepath.Join(tmpDir, "localHeaders.rsp"),
		CrossModuleDepsRspFile: filepath.Join(tmpDir, "crossmoduledeps.rsp"),
		JavaFile1:              filepath.Join(tmpDir, "src/com/example/ClassA.java"),
		JavaFile2:              filepath.Join(tmpDir, "src/com/example/ClassC.java"),
		JavaFile3:              filepath.Join(tmpDir, "src/org/another/ClassD.java"), // Example different package
		DepJar:                 filepath.Join(tmpDir, "deps.jar"),
		HeaderJar:              filepath.Join(tmpDir, "headers.jar"),
		CrossModuleJar:         filepath.Join(tmpDir, "crossmodule.jar"),
		CrossModuleClass:       "Class.class",
	}

	// Create directories and initial file contents
	createDir(t, filepath.Dir(fixture.JavaFile1))
	createDir(t, filepath.Dir(fixture.JavaFile3))
	createDir(t, fixture.ClassDir)

	writeFile(t, fixture.JavaFile1, "package com.example; class File1 {}")
	writeFile(t, fixture.JavaFile2, "package com.example; class File2 {}")
	writeFile(t, fixture.JavaFile3, "package org.another; class ClassD {}")

	writeFile(t, fixture.DepJar, "Dep jar")
	writeFile(t, fixture.HeaderJar, "Header jar")
	writeZip(t, fixture.CrossModuleJar, fixture.CrossModuleClass, "Cross Module Jar class contents")

	writeFile(t, fixture.SrcRspFile, fmt.Sprintf("%s\n%s\n%s", fixture.JavaFile1, fixture.JavaFile2, fixture.JavaFile3))
	writeFile(t, fixture.DepsRspFile, fmt.Sprintf("%s", fixture.DepJar))
	writeFile(t, fixture.HeadersRspFile, fmt.Sprintf("%s", fixture.HeaderJar))
	writeFile(t, fixture.CrossModuleDepsRspFile, fmt.Sprintf("%s", fixture.CrossModuleJar))
	writeFile(t, fixture.JavacTargetJar, "Javac Jar")
	writeFile(t, fixture.JavaSrcDeps, "")
	createProtoFileWithActualPaths(t, fixture.JavaSrcDeps, fixture.JavaFile1, fixture.JavaFile2, fixture.JavaFile3, fixture.CrossModuleClass)

	return fixture
}

// runGenerator calls GenerateIncrementalInput for the testFixture
func (tf *testFixture) runGenerator() {
	// Small delay often needed for filesystem timestamp granularity
	time.Sleep(15 * time.Millisecond)
	err := GenerateIncrementalInput(tf.ClassDir, tf.SrcRspFile, tf.DepsRspFile, tf.JavacTargetJar, tf.JavaSrcDeps, tf.HeadersRspFile, tf.CrossModuleDepsRspFile)
	if err != nil {
		tf.t.Fatalf("GenerateIncrementalInput() returned an error: %v", err)
	}
}

// returns incOutputPath for testFixture
func (tf *testFixture) incOutputPath() string {
	return tf.JavacTargetJar + ".inc.rsp"
}

// returns remOutputPath for testFixture
func (tf *testFixture) remOutputPath() string {
	return tf.JavacTargetJar + ".rem.rsp"
}

// Verifies the test output against expected output
func checkOutput(t *testing.T, incOutputPath, expectedIncContent, remOutputPath, expectedRemContent string) {
	checkFileContent(t, incOutputPath, expectedIncContent)
	checkFileContent(t, remOutputPath, expectedRemContent)
}

// Helper to check if the content of a file matches the expected content (order insensitive)
func checkFileContent(t *testing.T, filePath, expectedContent string) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read output file %q: %v", filePath, err)
	}
	actualContent := strings.TrimSpace(string(contentBytes))

	actualLines := strings.Split(actualContent, "\n")
	expectedLines := strings.Split(expectedContent, "\n")
	sort.Strings(actualLines)
	sort.Strings(expectedLines)
	actualContent = strings.Join(actualLines, "\n")
	expectedContent = strings.Join(expectedLines, "\n")

	if actualContent != expectedContent {
		t.Errorf("Unexpected content in %q.\nGot:\n%s\nWant:\n%s", filePath, actualContent, expectedContent)
	}
}

// Helper to save prior state
func (tf *testFixture) savePriorState() {
	tf.t.Helper()
	// Implement your logic to save the necessary state files
	// e.g., copy *.pc_state.new to *.pc_state
	inputStateNew := tf.JavacTargetJar + ".input.pc_state.new"
	inputState := tf.JavacTargetJar + ".input.pc_state"
	depsStateNew := tf.JavacTargetJar + ".deps.pc_state.new"
	depsState := tf.JavacTargetJar + ".deps.pc_state"
	headerStateNew := tf.JavacTargetJar + ".headers.pc_state.new"
	headerState := tf.JavacTargetJar + ".headers.pc_state"
	crossModuleDepsStateNew := tf.JavacTargetJar + ".crossModuleDeps.pc_state.new"
	crossModuleDepsState := tf.JavacTargetJar + ".crossModuleDeps.pc_state"

	os.Rename(inputStateNew, inputState)
	os.Rename(depsStateNew, depsState)
	os.Rename(headerStateNew, headerState)
	os.Rename(crossModuleDepsStateNew, crossModuleDepsState)
}

// --- File Create/Mod/Delete helpers ---

func createDir(t *testing.T, dirPath string) {
	t.Helper()
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		t.Fatalf("Failed to create directory %q: %v", dirPath, err)
	}
}

func writeFile(t *testing.T, filePath, content string) {
	t.Helper()
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write file %q: %v", filePath, err)
	}
}

func writeZip(t *testing.T, zipPath, filePath, content string) {
	t.Helper()
	zf, err := os.OpenFile(zipPath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		t.Fatalf("Failed to create zip file to write to %q: %v", zipPath, err)
	}

	// Create a new zip archive.
	w := zip.NewWriter(zf)

	f, err := w.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to add file to zip %q: %v", filePath, err)
	}
	_, err = f.Write([]byte(content))
	if err != nil {
		t.Fatalf("Failed to add contents to file inside of zip %q: %v", filePath, err)
	}

	err = w.Close()
	if err != nil {
		t.Fatalf("Failed to close and flush zip file %q: %v", zipPath, err)
	}
}

func modifyFile(t *testing.T, filePath, newContentSuffix string) {
	t.Helper()
	// Append suffix to ensure modification time changes reliably
	contentBytes, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) { // Allow modification even if file was deleted before
		t.Fatalf("Failed to read file for modification %q: %v", filePath, err)
	}
	newContent := string(contentBytes) + "// " + newContentSuffix
	writeFile(t, filePath, newContent)
}

func deleteFile(t *testing.T, filePath, srcRspFilePath string) {
	t.Helper()
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) { // Ignore error if already deleted
		t.Fatalf("Failed to delete file %q: %v", filePath, err)
	}
	// Also update the source rsp file!
	updateSrcRsp(t, srcRspFilePath, filePath, true)
}

func updateSrcRsp(t *testing.T, rspPath, filePath string, remove bool) {
	t.Helper()
	contentBytes, err := os.ReadFile(rspPath)
	if err != nil {
		t.Fatalf("Failed to read source rsp file %q: %v", rspPath, err)
	}
	lines := strings.Split(string(contentBytes), "\n")
	var newLines []string
	found := false
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}
		if trimmedLine == filePath {
			found = true
			if !remove { // Keep it if not removing
				newLines = append(newLines, trimmedLine)
			}
		} else {
			newLines = append(newLines, trimmedLine)
		}
	}
	// Add back if it wasn't found and we are not removing (e.g., restoring)
	if !found && !remove {
		newLines = append(newLines, filePath)
	}

	writeFile(t, rspPath, strings.Join(newLines, "\n"))
}

// --- ProtoFile Creation helpers ---

func createProtoFile(t *testing.T, filePath string) string {
	t.Helper()

	dep1 := &dependency_proto.FileDependency{
		FilePath:          proto.String("file1.java"),
		FileDependencies:  []string{"file2.java", "file3.java"},
		IsDependencyToAll: proto.Bool(false),
		GeneratedClasses:  []string{"ClassA", "ClassB"},
	}
	dep2 := &dependency_proto.FileDependency{
		FilePath:          proto.String("file2.java"),
		FileDependencies:  []string{},
		IsDependencyToAll: proto.Bool(true),
		GeneratedClasses:  []string{"ClassC"},
	}
	dep3 := &dependency_proto.FileDependency{
		FilePath:          proto.String("file3.java"),
		FileDependencies:  []string{},
		IsDependencyToAll: proto.Bool(false),
		GeneratedClasses:  []string{"ClassD"},
	}

	message := &dependency_proto.FileDependencyList{
		FileDependency: []*dependency_proto.FileDependency{dep1, dep2, dep3},
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("Failed to marshal proto message: %v", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("Failed to write proto file: %v", err)
	}

	return filePath
}

func createProtoFileWithActualPaths(t *testing.T, protoFilePath, javaFile1, javaFile2, javaFile3, crossModuleClass string) string {
	t.Helper()

	dep1 := &dependency_proto.FileDependency{
		FilePath:             proto.String(javaFile1),
		FileDependencies:     []string{javaFile2, javaFile3},
		IsDependencyToAll:    proto.Bool(false),
		GeneratedClasses:     []string{"src/com/example/ClassA", "src/com/example/ClassB"},
		CrossModuleClassDeps: []string{crossModuleClass},
	}
	dep2 := &dependency_proto.FileDependency{
		FilePath:             proto.String(javaFile2),
		FileDependencies:     []string{},
		IsDependencyToAll:    proto.Bool(true),
		GeneratedClasses:     []string{"src/com/example/ClassC"},
		CrossModuleClassDeps: []string{},
	}
	dep3 := &dependency_proto.FileDependency{
		FilePath:             proto.String(javaFile3),
		FileDependencies:     []string{},
		IsDependencyToAll:    proto.Bool(false),
		GeneratedClasses:     []string{"org.another.ClassD"},
		CrossModuleClassDeps: []string{},
	}

	message := &dependency_proto.FileDependencyList{
		FileDependency: []*dependency_proto.FileDependency{dep1, dep2, dep3},
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("Failed to marshal proto message: %v", err)
	}

	if err := os.WriteFile(protoFilePath, data, 0644); err != nil {
		t.Fatalf("Failed to write proto file: %v", err)
	}

	return protoFilePath
}
