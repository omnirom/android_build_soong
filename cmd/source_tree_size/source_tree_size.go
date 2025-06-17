package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

var root string
var repo string
var outDir string
var channel chan SourceFile
var waiter sync.WaitGroup
var sourceTree SourceTree

func normalizeOutDir(outDir, root string) string {
	if len(outDir) == 0 {
		return ""
	}
	if filepath.IsAbs(outDir) {
		if strings.HasPrefix(outDir, root) {
			// Absolute path inside root, use it
			return outDir
		} else {
			// Not inside root, we don't care about it
			return ""
		}
	} else {
		// Relative path inside root, make it absolute
		return root + outDir
	}
}

func walkDir(dir string) {
	defer waiter.Done()

	visit := func(path string, info os.FileInfo, err error) error {
		name := info.Name()

		// Repo git projects are symlinks.  A real directory called .git counts as checked in
		// (and is very likely to be wasted space)
		if info.Mode().Type()&os.ModeSymlink != 0 && name == ".git" {
			return nil
		}

		// Skip .repo and out
		if info.IsDir() && (path == repo || path == outDir) {
			return filepath.SkipDir
		}

		if info.IsDir() && path != dir {
			waiter.Add(1)
			go walkDir(path)
			return filepath.SkipDir
		}

		if !info.IsDir() {
			sourcePath := strings.TrimPrefix(path, root)
			file := SourceFile{
				Path:      proto.String(sourcePath),
				SizeBytes: proto.Int32(42),
			}
			channel <- file
		}
		return nil

	}
	filepath.Walk(dir, visit)
}

func main() {
	var outputFile string
	flag.StringVar(&outputFile, "o", "", "The file to write")
	flag.StringVar(&outDir, "out_dir", "out", "The out directory")
	flag.Parse()

	if outputFile == "" {
		fmt.Fprintf(os.Stderr, "source_tree_size: Missing argument: -o\n")
		os.Exit(1)
	}

	root, _ = os.Getwd()
	if root[len(root)-1] != '/' {
		root += "/"
	}

	outDir = normalizeOutDir(outDir, root)
	repo = path.Join(root, ".repo")

	// The parallel scanning reduces the run time by about a minute
	channel = make(chan SourceFile)
	waiter.Add(1)
	go walkDir(root)
	go func() {
		waiter.Wait()
		close(channel)
	}()
	for sourceFile := range channel {
		sourceTree.Files = append(sourceTree.Files, &sourceFile)
	}

	// Sort the results, for a stable output
	sort.Slice(sourceTree.Files, func(i, j int) bool {
		return *sourceTree.Files[i].Path < *sourceTree.Files[j].Path
	})

	// Flatten and write
	buf, err := proto.Marshal(&sourceTree)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't marshal protobuf\n")
		os.Exit(1)
	}
	err = ioutil.WriteFile(outputFile, buf, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}
}
