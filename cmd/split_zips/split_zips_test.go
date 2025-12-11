package main

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"android/soong/third_party/zip"
)

// TestProcessAndSplit_BasicSplit tests a simple scenario.
func TestProcessAndSplit_BasicSplit(t *testing.T) {
	zip1Entries := []ZipEntry{
		{Name: "file1.txt", Content: "content1"},
		{Name: "file2.txt", Content: "content2"},
	}
	zip1SoongReader, err := createInMemorySoongZip(zip1Entries)
	if err != nil {
		t.Fatalf("Failed to create zip1: %v", err)
	}
	zip2Entries := []ZipEntry{
		{Name: "file3.txt", Content: "content3"},
		{Name: "file4.txt", Content: "content4"},
	}
	zip2SoongReader, err := createInMemorySoongZip(zip2Entries)
	if err != nil {
		t.Fatalf("Failed to create zip2: %v", err)
	}
	readers := []zipReader{
		{reader: zip1SoongReader, zipPath: "zip1.zip"}, // reader field expects *zip.Reader
		{reader: zip2SoongReader, zipPath: "zip2.zip"},
	}
	writer1, writer1Buf := createTestAppZipWriter()
	writer2, writer2Buf := createTestAppZipWriter()
	writers := []zipWriter{writer1, writer2}

	processAndSplit(readers, writers)
	writer1.writer.Close()
	writer2.writer.Close()

	expectedSplit1Files := []string{"file1.txt", "file2.txt"}
	expectedSplit2Files := []string{"file3.txt", "file4.txt"}

	actualSplit1Files, err := getFilenamesFromSoongZipBuffer(writer1Buf)
	if err != nil {
		t.Fatalf("Error reading split1 output: %v", err)
	}
	actualSplit2Files, err := getFilenamesFromSoongZipBuffer(writer2Buf)
	if err != nil {
		t.Fatalf("Error reading split2 output: %v", err)
	}

	if !reflect.DeepEqual(actualSplit1Files, expectedSplit1Files) {
		t.Errorf("Split 1 files mismatch:\nExpected: %v\nActual:   %v", expectedSplit1Files, actualSplit1Files)
	}
	if !reflect.DeepEqual(actualSplit2Files, expectedSplit2Files) {
		t.Errorf("Split 2 files mismatch:\nExpected: %v\nActual:   %v", expectedSplit2Files, actualSplit2Files)
	}
}

// TestProcessAndSplit_SingleOutput tests splitting into a single output zip.
func TestProcessAndSplit_SingleOutput(t *testing.T) {
	zipEntries := []ZipEntry{
		{Name: "a.txt", Content: "a"},
		{Name: "b.txt", Content: "b"},
	}
	zip1SoongReader, _ := createInMemorySoongZip(zipEntries)
	readers := []zipReader{{reader: zip1SoongReader, zipPath: "zip1.zip"}}
	writer, writerBuf := createTestAppZipWriter()
	writers := []zipWriter{writer}

	processAndSplit(readers, writers)
	writer.writer.Close()

	expectedFiles := []string{"a.txt", "b.txt"}
	actualFiles, _ := getFilenamesFromSoongZipBuffer(writerBuf)
	if !reflect.DeepEqual(actualFiles, expectedFiles) {
		t.Errorf("Single output files mismatch:\nExpected: %v\nActual:   %v", expectedFiles, actualFiles)
	}
}

// TestProcessAndSplit_UnevenSplit tests splitting when files don't divide evenly.
func TestProcessAndSplit_UnevenSplit(t *testing.T) {
	zipEntries := []ZipEntry{
		{Name: "f1.txt", Content: "1"},
		{Name: "f2.txt", Content: "2"},
		{Name: "f3.txt", Content: "3"},
		{Name: "f4.txt", Content: "4"},
		{Name: "f5.txt", Content: "5"},
	}
	soongReader, _ := createInMemorySoongZip(zipEntries)
	readers := []zipReader{{reader: soongReader, zipPath: "zip.zip"}}
	writer1, writer1Buf := createTestAppZipWriter()
	writer2, writer2Buf := createTestAppZipWriter()
	writers := []zipWriter{writer1, writer2}

	processAndSplit(readers, writers)
	writer1.writer.Close()
	writer2.writer.Close()

	expectedSplit1 := []string{"f1.txt", "f2.txt", "f3.txt"}
	expectedSplit2 := []string{"f4.txt", "f5.txt"}

	actualSplit1, _ := getFilenamesFromSoongZipBuffer(writer1Buf)
	actualSplit2, _ := getFilenamesFromSoongZipBuffer(writer2Buf)

	if !reflect.DeepEqual(actualSplit1, expectedSplit1) {
		t.Errorf("Uneven Split 1 files mismatch:\nExpected: %v\nActual:   %v", expectedSplit1, actualSplit1)
	}
	if !reflect.DeepEqual(actualSplit2, expectedSplit2) {
		t.Errorf("Uneven Split 2 files mismatch:\nExpected: %v\nActual:   %v", expectedSplit2, actualSplit2)
	}
}

// TestProcessAndSplit_SkipsDirectories tests that directory entries are skipped.
func TestProcessAndSplit_SkipsDirectories(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	_, err := zw.CreateHeader(&zip.FileHeader{Name: "dir1/", Method: zip.Store})
	if err != nil {
		t.Fatalf("Failed to create dir header: %v", err)
	}
	f, err := zw.CreateHeader(&zip.FileHeader{Name: "dir1/file_in_dir.txt", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("Failed to create file header: %v", err)
	}
	_, err = io.WriteString(f, "content")
	if err != nil {
		t.Fatalf("Failed to write file content: %v", err)
	}
	zw.Close()
	soongReader, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	readers := []zipReader{{reader: soongReader, zipPath: "zip.zip"}}
	writer, writerBuf := createTestAppZipWriter()
	writers := []zipWriter{writer}

	processAndSplit(readers, writers)
	writer.writer.Close()

	// --- Assert ---
	expectedFiles := []string{"dir1/file_in_dir.txt"}
	actualFiles, _ := getFilenamesFromSoongZipBuffer(writerBuf)

	if !reflect.DeepEqual(actualFiles, expectedFiles) {
		t.Errorf("Directory skip test files mismatch:\nExpected: %v\nActual:   %v", expectedFiles, actualFiles)
	}
}

// TestProcessAndSplit_DuplicateFiles_Panics tests that duplicate files across zips cause a panic.
func TestProcessAndSplit_DuplicateFiles_Panics(t *testing.T) {
	zip1Entries := []ZipEntry{{Name: "common.txt", Content: "v1"}}
	zip2Entries := []ZipEntry{{Name: "common.txt", Content: "v2"}} // Duplicate
	zip1SoongReader, _ := createInMemorySoongZip(zip1Entries)
	zip2SoongReader, _ := createInMemorySoongZip(zip2Entries)

	readers := []zipReader{
		{reader: zip1SoongReader, zipPath: "zip1.zip"},
		{reader: zip2SoongReader, zipPath: "zip2.zip"},
	}
	writer, _ := createTestAppZipWriter()
	writers := []zipWriter{writer}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected processAndSplit to panic on duplicate files, but it did not")
		} else {
			errMsg := fmt.Sprintf("%v", r)
			if !strings.Contains(errMsg, "Duplicate file 'common.txt'") {
				t.Errorf("Panic message did not contain expected duplicate file info. Got: %s", errMsg)
			}
		}
	}()
	processAndSplit(readers, writers)
}

// TestProcessAndSplit_NoProcessableFiles_Panics tests panic when no files match filter or are directories.
func TestProcessAndSplit_NoProcessableFiles_Panics(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	_, _ = zw.CreateHeader(&zip.FileHeader{Name: "onlydir/", Method: zip.Store})
	zw.Close()
	soongReader, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	readers := []zipReader{{reader: soongReader, zipPath: "zip.zip"}}
	writer, _ := createTestAppZipWriter()
	writers := []zipWriter{writer}
	originalFilter := filter
	filter = nil
	defer func() { filter = originalFilter }()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected processAndSplit to panic with no processable files, but it did not")
		} else {
			errMsg := fmt.Sprintf("%v", r)
			if !strings.Contains(errMsg, "no processable files found") {
				t.Errorf("Panic message incorrect. Got: %s", errMsg)
			}
		}
	}()
	processAndSplit(readers, writers)
}

// TestProcessAndSplit_TooManySplits_Panics tests panic when numSplits > numFiles.
func TestProcessAndSplit_TooManySplits_Panics(t *testing.T) {
	zipEntries := []ZipEntry{{Name: "file1.txt", Content: "1"}} // Only one file
	soongReader, _ := createInMemorySoongZip(zipEntries)
	readers := []zipReader{{reader: soongReader, zipPath: "zip.zip"}}
	writer1, _ := createTestAppZipWriter()
	writer2, _ := createTestAppZipWriter()
	writers := []zipWriter{writer1, writer2}
	originalFilter := filter
	filter = nil
	defer func() { filter = originalFilter }()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected processAndSplit to panic with too many splits, but it did not")
		} else {
			errMsg := fmt.Sprintf("%v", r)
			if !strings.Contains(errMsg, "number of splits '2' more than total files '1'") {
				t.Errorf("Panic message incorrect. Got: %s", errMsg)
			}
		}
	}()
	processAndSplit(readers, writers)
}

// TestProcessAndSplit_WithFilter tests file filtering.
func TestProcessAndSplit_WithFilter(t *testing.T) {
	zipEntries := []ZipEntry{
		{Name: "image.jpg", Content: "jpg_data"},
		{Name: "document.txt", Content: "txt_data"},
		{Name: "archive.zip", Content: "zip_data"},
	}
	soongReader, _ := createInMemorySoongZip(zipEntries)
	readers := []zipReader{{reader: soongReader, zipPath: "zip.zip"}}
	writer, writerBuf := createTestAppZipWriter()
	writers := []zipWriter{writer}

	originalFilter := filter
	filter = multiFlag{"*.txt", "*.zip"}
	defer func() { filter = originalFilter }()

	processAndSplit(readers, writers)
	writer.writer.Close()

	expectedFiles := []string{"archive.zip", "document.txt"}
	actualFiles, _ := getFilenamesFromSoongZipBuffer(writerBuf)
	if !reflect.DeepEqual(actualFiles, expectedFiles) {
		t.Errorf("Filter test files mismatch:\nExpected: %v\nActual:   %v", expectedFiles, actualFiles)
	}
}

// TestProcessAndSplit_EmptyInputZips_Panics tests panic when input zips are empty.
func TestProcessAndSplit_EmptyInputZips_Panics(t *testing.T) {
	soongReader, _ := createInMemorySoongZip([]ZipEntry{}) // Empty zip
	readers := []zipReader{{reader: soongReader, zipPath: "empty.zip"}}
	writer, _ := createTestAppZipWriter()
	writers := []zipWriter{writer}
	originalFilter := filter
	filter = nil
	defer func() { filter = originalFilter }()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected processAndSplit to panic with empty input zips, but it did not")
		} else {
			errMsg := fmt.Sprintf("%v", r)
			if !strings.Contains(errMsg, "no processable files found") {
				t.Errorf("Panic message incorrect for empty input. Got: %s", errMsg)
			}
		}
	}()
	processAndSplit(readers, writers)
}

type ZipEntry struct {
	Name    string
	Content string
}

// Helper function to create an in-memory ZIP with specified files.
// Each fileContent entry should be a ZipEntry content.
func createInMemorySoongZip(entries []ZipEntry) (*zip.Reader, error) { // Returns *zip.Reader from soong
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	for _, entry := range entries {
		name := filepath.ToSlash(entry.Name)
		fh := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}

		f, err := zipWriter.CreateHeader(fh)
		if err != nil {
			return nil, err
		}
		_, err = io.WriteString(f, entry.Content)
		if err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}

	return zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

// Helper function to create a zipWriter for testing.
func createTestAppZipWriter() (zipWriter, *bytes.Buffer) {
	outputBuf := &bytes.Buffer{}
	return zipWriter{
		writer:     zip.NewWriter(outputBuf),
		zipBytes:   outputBuf,
		outputPath: "test_output.zip",
	}, outputBuf
}

// Helper function to get filenames from a zip.Reader (from its buffer)
func getFilenamesFromSoongZipBuffer(zipBuf *bytes.Buffer) ([]string, error) {
	if zipBuf.Len() == 0 {
		return []string{}, nil
	}
	r, err := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names, nil
}
