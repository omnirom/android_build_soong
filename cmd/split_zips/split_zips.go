package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"android/soong/response"
	"android/soong/third_party/zip"
	"github.com/google/blueprint/pathtools"
)

var (
	rspFile = flag.String("i", "", "RSP file containing a whitespace-separated list of input ZIPs")
	filter  multiFlag
)

func init() {
	flag.Var(&filter, "f", "optional filter pattern")
}

type zipReader struct {
	reader  *zip.Reader
	zipPath string
}

type zipWriter struct {
	writer     *zip.Writer
	zipBytes   *bytes.Buffer
	outputPath string
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: split_zips -i input.rsp -f \"glob\" output1.zip [output2.zip ...]")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Splits the combined content of input zips (from RSP file) into the specified output zip files.")
		fmt.Fprintln(os.Stderr, "<-f> uses the rules at https://godoc.org/github.com/google/blueprint/pathtools/#Match to filter content.")
		fmt.Fprintln(os.Stderr, "Mod time of output zip is set same as that of the first entry in zip.")
		fmt.Fprintln(os.Stderr, "Directory entries in input zips are skipped.")
		fmt.Fprintln(os.Stderr, "Duplicate files in input zips are not allowed.")
	}

	flag.Parse()

	outputZipPaths := flag.Args() // Get positional arguments for output zip paths

	if *rspFile == "" {
		log.Println("Error: Input RSP file (-i) is required.")
		flag.Usage()
		os.Exit(1)
	}
	if len(outputZipPaths) == 0 {
		log.Println("Error: At least one output ZIP file path must be provided.")
		flag.Usage()
		os.Exit(1)
	}

	log.SetFlags(log.Lshortfile)

	rsp, err := os.Open(*rspFile)
	if err != nil {
		fmt.Println("err: ", err)
		panic(err)
	}
	inputZips, err := response.ReadRspFile(rsp)
	if err != nil {
		panicOnError(err)
	}
	if len(inputZips) == 0 {
		panicOnError(fmt.Errorf("no input zips found"))
	}

	var zipReaders []zipReader
	var zipWriters []zipWriter

	for _, inputZip := range inputZips {
		reader, err := zip.OpenReader(inputZip)
		if err != nil {
			panicOnError(fmt.Errorf("warning: Failed to open input ZIP %s: %v", inputZip, err))
		}
		zipReaders = append(zipReaders, zipReader{
			reader:  &reader.Reader,
			zipPath: inputZip,
		})
		defer reader.Close()
	}
	for _, outputZipPath := range outputZipPaths {
		outputBuf := &bytes.Buffer{}
		zipWriters = append(zipWriters, zipWriter{
			writer:     zip.NewWriter(outputBuf),
			zipBytes:   outputBuf,
			outputPath: outputZipPath,
		})
	}

	processAndSplit(zipReaders, zipWriters)

	// Write the outputs from buffer
	for _, zipW := range zipWriters {
		zipW.writer.Close()
		writeIfChanged(zipW.outputPath, zipW.zipBytes)
	}
}

func processAndSplit(readers []zipReader, writers []zipWriter) {
	numSplits := len(writers)
	var allFileEntries []*zip.File
	filesSeenInAllZips := make(map[string]string)

	for _, zipR := range readers {
		for _, f := range zipR.reader.File {
			if filter != nil {
				if match, err := filter.Match(filepath.Base(f.Name)); err != nil {
					panicOnError(err)
				} else if !match {
					continue
				}
			}
			if _, seen := filesSeenInAllZips[f.Name]; !seen {
				filesSeenInAllZips[f.Name] = zipR.zipPath
				// Directories are skipped.
				if !f.FileInfo().IsDir() {
					allFileEntries = append(allFileEntries, f)
				}
			} else {
				if !f.FileInfo().IsDir() {
					panicOnError(fmt.Errorf("info: Duplicate file '%s' found in ZIP(s) '%s' and '%s'", f.Name, filesSeenInAllZips[f.Name], zipR.zipPath))
				}
			}
		}
	}

	if len(allFileEntries) == 0 {
		panicOnError(fmt.Errorf("no processable files found in input ZIPs"))
	}

	if numSplits > len(allFileEntries) {
		panicOnError(fmt.Errorf("number of splits '%d' more than total files '%d' in input zip(s)", numSplits, len(allFileEntries)))
	}

	// Determine how many actual files go into each split (approximately)
	filesPerSplitTarget := (len(allFileEntries) + numSplits - 1) / numSplits // Ceiling division
	currentOutputIndex := 0
	filesInCurrentSplit := 0

	for _, f := range allFileEntries {
		// If we reach max capacity for a split, use the next output
		if filesInCurrentSplit >= filesPerSplitTarget && currentOutputIndex < numSplits {
			currentOutputIndex++
			filesInCurrentSplit = 0
		}

		// Get the current zip writer
		currentWriter := writers[currentOutputIndex]
		if currentWriter.writer == nil {
			panicOnError(fmt.Errorf("zip Writer is nil"))
		}

		// f is from allFileEntries, its expected that its original reader is not
		// closed at this point. If it is, error will be thrown.
		err := currentWriter.writer.CopyFrom(f, f.Name)
		if err != nil {
			panicOnError(err)
		}

		filesInCurrentSplit++
	}
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, " ")
}

func (m *multiFlag) Set(s string) error {
	*m = append(*m, s)
	return nil
}

func (m *multiFlag) Match(s string) (bool, error) {
	if m == nil {
		return false, nil
	}
	for _, f := range *m {
		if match, err := filepath.Match(f, s); err != nil {
			return false, err
		} else if match {
			return true, nil
		}
	}
	return false, nil
}

func writeIfChanged(outputFile string, buffer *bytes.Buffer) {
	if err := pathtools.WriteFileIfChanged(outputFile, buffer.Bytes(), 0666); err != nil {
		panicOnError(err)
	}
}

func panicOnError(err error) {
	fmt.Println("err: ", err)
	panic(err)
}
