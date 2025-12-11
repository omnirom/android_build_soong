// Copyright 2025[ Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This tool finds all subdirectories and files in a list of directories and
// produces a dependency file for use by ninja.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"android/soong/makedeps"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s -o <output> -t <target> <dir> [<dir>...]", os.Args[0])
		flag.PrintDefaults()
	}
	output := flag.String("o", "", "Output file")
	target := flag.String("t", "", "Target name to write into depfile")

	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal("Expected at least one directory as an argument")
	}

	if *output == "" {
		log.Fatal("Expected -o argument")
	}

	if *target == "" {
		log.Fatal("Expected -t argument")
	}

	depsFile := makedeps.Deps{
		Output: *target,
	}

	for _, arg := range flag.Args() {
		deps, err := collectDirectoryDeps(arg)
		if err != nil {
			log.Fatalf("Error collecting deps from %q: %v", arg, err)
		}

		depsFile.Inputs = append(depsFile.Inputs, deps...)
	}

	err := os.WriteFile(*output, depsFile.Print(), 0666)
	if err != nil {
		log.Fatalf("Failed to write output file %q: %v", *output, err)
	}
}

func collectDirectoryDeps(dir string) (deps []string, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		deps = append(deps, path)
		return nil
	})
	return deps, err
}
