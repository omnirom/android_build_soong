package incremental_dex_input_lib

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

	soong_zip "android/soong/zip"

	fid_lib "android/soong/cmd/find_input_delta/find_input_delta_lib"
)

// --- Tests for `readRspFile` ---

func TestReadRspFile(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name     string
		content  string
		expected []string
	}{
		{"Empty", "", nil},
		{"SingleLine", "external/kotlinx.serialization/rules/r8.pro", []string{"external/kotlinx.serialization/rules/r8.pro"}},
		{"MultipleLines", "external/kotlinx.serialization/rules/r8.pro\nfoo/bar/baz/guava.jar\nfoo/bar/baz1/guava1.jar", []string{"external/kotlinx.serialization/rules/r8.pro", "foo/bar/baz/guava.jar", "foo/bar/baz1/guava1.jar"}},
		{"WithSpaces", "  foo/bar/baz/guava.jar  \n foo/bar/baz1/guava1.jar ", []string{"foo/bar/baz/guava.jar", "foo/bar/baz1/guava1.jar"}}, //Should be trimmed.
		{"WithEmptyLines", "foo/bar/baz/guava.jar\n\nfoo/bar/baz1/guava1.jar", []string{"foo/bar/baz/guava.jar", "foo/bar/baz1/guava1.jar"}},
		{"WithCarriageReturn", "foo/bar/baz/guava.jar\r\nfoo/bar/baz1/guava1.jar", []string{"foo/bar/baz/guava.jar", "foo/bar/baz1/guava1.jar"}},
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
		Changes: []fid_lib.FileList{
			{
				Name:      "out/soong/dummy/my/fav/code-target.jar",
				Additions: []string{"foo/bar/added.class", "foo/bar/added$1.class"},
				Deletions: []string{"foo/bar/deleted.class"},
				Changes: []fid_lib.FileList{
					{Name: "foo/bar/changed.class"},
					{Name: "foo/bar/changed$1.class"},
				},
			},
		},
	}

	expectedAdditions := []string{"foo/bar/added.class", "foo/bar/added$1.class"}
	expectedDeletions := []string{"foo/bar/deleted.class"}
	expectedChanges := []string{"out/soong/dummy/my/fav/code-target.jar", "foo/bar/changed.class", "foo/bar/changed$1.class"}

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

// --- Tests for `getAllPackages` ---
func TestGetAllPackages(t *testing.T) {
	testCases := []struct {
		name             string
		entries          map[string][]byte
		expectedPackages []string // Must be sorted
	}{
		{
			name: "Basic Functionality",
			entries: map[string][]byte{
				"com/example/Main.class":        nil,
				"org/gradle/Utils.class":        nil,
				"com/example/data/Model.class":  nil,
				"META-INF/MANIFEST.MF":          []byte("Manifest-Version: 1.0"),
				"config.properties":             []byte("key=value"),
				"com/example/another/One.class": nil,
			},
			expectedPackages: []string{"com/example", "com/example/another", "com/example/data", "org/gradle"},
		},
		{
			name:             "Empty JAR",
			entries:          map[string][]byte{},
			expectedPackages: []string{},
		},
		{
			name: "No Class Files",
			entries: map[string][]byte{
				"META-INF/MANIFEST.MF": []byte("Manifest-Version: 1.0"),
				"resource.txt":         []byte("some data"),
				"some/dir/config.xml":  nil,
			},
			expectedPackages: []string{},
		},
		{
			name: "Root Class Files",
			entries: map[string][]byte{
				"RootClass.class":       nil,
				"AnotherRoot.class":     nil,
				"com/example/App.class": nil,
				"org/myapp/Start.class": nil,
			},
			expectedPackages: []string{".", "com/example", "org/myapp"},
		},
		{
			name: "Duplicate Packages",
			entries: map[string][]byte{
				"com/example/ClassA.class": nil,
				"com/example/ClassB.class": nil,
				"org/utils/Helper1.class":  nil,
				"org/utils/Helper2.class":  nil,
				"com/example/ClassC.class": nil,
			},
			expectedPackages: []string{"com/example", "org/utils"},
		},
		{
			name: "Nested Packages",
			entries: map[string][]byte{
				"com/example/util/io/Reader.class":  nil,
				"com/example/util/net/Client.class": nil,
				"com/example/App.class":             nil,
			},
			expectedPackages: []string{"com/example", "com/example/util/io", "com/example/util/net"},
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.name, func(t *testing.T) {
			jarFileName := strings.ReplaceAll(strings.ToLower(tc.name), " ", "_") + ".jar"
			jarPath := createTestJar(t, jarFileName, tc.entries)

			actualPackages := getAllPackages(jarPath)

			// --- Assertion using standard testing package ---
			if !reflect.DeepEqual(tc.expectedPackages, actualPackages) {
				t.Errorf("Test case '%s' failed:\nExpected: %v\nActual:   %v",
					tc.name, tc.expectedPackages, actualPackages)
			}
			if len(tc.expectedPackages) != len(actualPackages) {
				t.Errorf("Test case '%s' failed: Expected length %d, got %d",
					tc.name, len(tc.expectedPackages), len(actualPackages))
			}
		})
	}
}

// --- Test Fixture Setup ---
// Struct to hold common test file paths
type testFixture struct {
	t                *testing.T
	tmpDir           string
	DexOutputDir     string
	PackageOutputDir string
	ClassJar         string
	DepsRspFile      string
	DexTargetJar     string
	ClassFile1       string
	ClassFile2       string
	ClassFile3       string
	DepJar           string
}

// newTestFixture creates the temporary directory and necessary files
func newTestFixture(t *testing.T) *testFixture {
	tmpDir := t.TempDir() // Use t.TempDir for automatic cleanup

	// Create dummy files needed for the tests
	fixture := &testFixture{
		t:                t,
		tmpDir:           tmpDir,
		DexOutputDir:     filepath.Join(tmpDir, "dex"),
		PackageOutputDir: filepath.Join(tmpDir, "dex", "packages"),
		ClassJar:         filepath.Join(tmpDir, "javac/classes.jar"),
		DepsRspFile:      filepath.Join(tmpDir, "dex/deps.rsp"),
		DexTargetJar:     filepath.Join(tmpDir, "dex/dex.jar"),
		ClassFile1:       filepath.Join(tmpDir, "javac/classes/com/example/ClassA.class"),
		ClassFile2:       filepath.Join(tmpDir, "javac/classes/com/example/ClassC.class"),
		ClassFile3:       filepath.Join(tmpDir, "javac/classes/org/another/ClassD.class"),
		DepJar:           filepath.Join(tmpDir, "dex/deps.jar"),
	}

	// Create directories and initial file contents
	createDir(t, filepath.Dir(fixture.ClassFile1))
	createDir(t, filepath.Dir(fixture.ClassFile3))
	createDir(t, fixture.DexOutputDir)

	writeFile(t, fixture.ClassFile1, "package com.example; class File1 {}")
	writeFile(t, fixture.ClassFile2, "package com.example; class File2 {}")
	writeFile(t, fixture.ClassFile3, "package org.another; class ClassD {}")

	writeFile(t, fixture.DepJar, "Dep jar")

	writeFile(t, fixture.DepsRspFile, fmt.Sprintf("%s", fixture.DepJar))
	writeFile(t, fixture.DexTargetJar, "Dex Jar")

	return fixture
}

// --- Tests for `generateIncrementalInput` ---

func TestGenerateIncrementalInput(t *testing.T) {
	// Set the environment variable to enable inc-compilation
	t.Setenv("SOONG_USE_PARTIAL_COMPILE", "true")

	// Shared setup for all subtests
	tf := newTestFixture(t)

	// --- Subtest: Initial Full Compile ---
	t.Run("InitialFullCompile", func(t *testing.T) {
		// Arrange
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", "com/example", "org/another"), // All files included initially
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One Class File Modified ---
	t.Run("Incremental_OneFileModified", func(t *testing.T) {
		// Arrange: Modify one file (ensure timestamp changes)
		modifyFile(t, tf.ClassFile1, "Incremental_OneFileModified")
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s", "com/example"),
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - Dependency Change ---
	t.Run("Incremental_DependencyChanged", func(t *testing.T) {
		// Arrange: Modify the DepJar
		modifyFile(t, tf.DepJar, "Incremental_DependencyChanged")
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert: All source files should be in inc.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", "com/example", "org/another"),
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One Class File Added ---
	t.Run("Incremental_FileAdded", func(t *testing.T) {
		// Arrange: Add one class file
		writeFile(t, filepath.Join(tf.tmpDir, "javac/classes/org/another/ClassE.class"), "package org.another; class File4 {}")
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert: Check usages of deleted file in inc.rsp, and class files
		// corresponding to deleted files in rem.rsp
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", "com/example", "org/another"),
		)
		tf.savePriorState() // Save state if needed for subsequent tests
	})
}

// --- Tests for `generateIncrementalInputPartialCompileOff` ---
func TestGenerateIncrementalInputPartialCompileOff(t *testing.T) {
	// Set the environment variable to enable inc-compilation
	t.Setenv("SOONG_USE_PARTIAL_COMPILE", "")

	// Shared setup for all subtests
	tf := newTestFixture(t)

	// --- Subtest: Initial Full Compile ---
	t.Run("InitialFullCompile", func(t *testing.T) {
		// Arrange
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", "com/example", "org/another"), // All files included initially
		)
		tf.savePriorState()
	})

	// --- Subtest: Incremental - One Class File Modified ---
	t.Run("Incremental_OneFileModified", func(t *testing.T) {
		// Arrange: Modify one file (ensure timestamp changes)
		modifyFile(t, tf.ClassFile1, "Incremental_OneFileModified")
		createQualifiedTestJar(t, tf.ClassJar, filepath.Join(tf.tmpDir, "javac", "classes"))

		// Act
		tf.runGenerator()

		// Assert
		checkOutput(
			t,
			tf.incOutputPath(),
			fmt.Sprintf("%s\n%s", "com/example", "org/another"),
		)
		tf.savePriorState()
	})
}

// createQualifiedTestJar creates a jar using soong_zip, mimicking javac
func createQualifiedTestJar(t *testing.T, outputPath, inputDir string) {
	err := soong_zip.Zip(soong_zip.ZipArgs{
		EmulateJar:     true,
		OutputFilePath: outputPath,
		FileArgs:       soong_zip.NewFileArgsBuilder().SourcePrefixToStrip(inputDir).Dir(inputDir).FileArgs(),
	})
	if err != nil {
		t.Fatalf("Error creating jar %s: %v", outputPath, err)
	}
}

// runGenerator calls GenerateIncrementalInput for the testFixture
func (tf *testFixture) runGenerator() {
	// Small delay often needed for filesystem timestamp granularity
	time.Sleep(15 * time.Millisecond)
	GenerateIncrementalInput(tf.ClassJar, tf.DexOutputDir, tf.PackageOutputDir, tf.DexTargetJar, tf.DepsRspFile)
}

// returns incOutputPath for testFixture
func (tf *testFixture) incOutputPath() string {
	return tf.DexTargetJar + ".inc.rsp"
}

// Helper to save prior state
func (tf *testFixture) savePriorState() {
	tf.t.Helper()
	// Implement your logic to save the necessary state files
	// e.g., copy *.pc_state.new to *.pc_state
	inputStateNew := tf.DexTargetJar + ".input.pc_state.new"
	inputState := tf.DexTargetJar + ".input.pc_state"
	depsStateNew := tf.DexTargetJar + ".deps.pc_state.new"
	depsState := tf.DexTargetJar + ".deps.pc_state"

	os.Rename(inputStateNew, inputState)
	os.Rename(depsStateNew, depsState)
}

// Verifies the test output against expected output
func checkOutput(t *testing.T, incOutputPath, expectedIncContent string) {
	contentBytes, err := os.ReadFile(incOutputPath)
	if err != nil {
		t.Fatalf("Failed to read output file %q: %v", incOutputPath, err)
	}
	actualContent := strings.TrimSpace(string(contentBytes))

	actualLines := strings.Split(actualContent, "\n")
	expectedLines := strings.Split(expectedIncContent, "\n")
	sort.Strings(actualLines)
	sort.Strings(expectedLines)
	actualContent = strings.Join(actualLines, "\n")
	expectedIncContent = strings.Join(expectedLines, "\n")

	if actualContent != expectedIncContent {
		t.Errorf("Unexpected content in %q.\nGot:\n%s\nWant:\n%s", incOutputPath, actualContent, expectedIncContent)
	}
}

// createTestJar creates a temporary JAR file for testing purposes.
// entries is a map where key is the entry name (path inside JAR) and value is content.
func createTestJar(t *testing.T, filename string, entries map[string][]byte) string {
	t.Helper() // Mark this as a test helper

	tempDir := t.TempDir() // Automatically cleaned up
	fullPath := filepath.Join(tempDir, filename)

	jarFile, err := os.Create(fullPath)
	if err != nil {
		t.Fatalf("Failed to create test JAR file %s: %v", fullPath, err)
	}
	defer jarFile.Close() // Ensure file is closed

	zipWriter := zip.NewWriter(jarFile)
	defer zipWriter.Close() // Ensure writer is closed

	for name, content := range entries {
		name = filepath.ToSlash(name)
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("Failed to create entry %s in test JAR %s: %v", name, filename, err)
		}
		if content != nil {
			_, err = writer.Write(content)
			if err != nil {
				t.Fatalf("Failed to write content for entry %s in test JAR %s: %v", name, filename, err)
			}
		}
	}

	err = zipWriter.Close()
	if err != nil {
		t.Fatalf("Failed to close zip writer for JAR %s: %v", filename, err)
	}
	err = jarFile.Close()
	if err != nil {
		t.Fatalf("Failed to close JAR file %s: %v", filename, err)
	}

	return fullPath
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
