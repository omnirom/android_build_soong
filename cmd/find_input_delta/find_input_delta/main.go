// Copyright 2024 Google Inc. All rights reserved.
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

package main

import (
	"flag"
	"os"
	"regexp"

	fid_lib "android/soong/cmd/find_input_delta/find_input_delta_lib"
)

var fileSepRegex = regexp.MustCompile("[^[:space:]]+")

func main() {
	var top string
	var prior_state_file string
	var new_state_file string
	var target string
	var inputs_file string
	var template string
	var inputs []string
	var inspect bool
	var err error

	flag.StringVar(&top, "top", ".", "path to top of workspace")
	flag.StringVar(&prior_state_file, "prior_state", "", "prior internal state file")
	flag.StringVar(&new_state_file, "new_state", "", "new internal state file")
	flag.StringVar(&target, "target", "", "name of ninja output file for build action")
	flag.StringVar(&inputs_file, "inputs_file", "", "file containing list of input files")
	flag.StringVar(&template, "template", fid_lib.DefaultTemplate, "output template for FileList")
	flag.BoolVar(&inspect, "inspect", false, "whether to inspect file contents")

	flag.Parse()

	if target == "" {
		panic("must specify --target")
	}
	// Drop any extra file names that arrived in `target`.
	target = fileSepRegex.FindString(target)
	if prior_state_file == "" {
		prior_state_file = target + ".pc_state"
	}
	if new_state_file == "" {
		new_state_file = prior_state_file + ".new"
	}

	if err = os.Chdir(top); err != nil {
		panic(err)
	}

	inputs = flag.Args()
	if inputs_file != "" {
		data, err := os.ReadFile(inputs_file)
		if err != nil {
			panic(err)
		}
		inputs = append(inputs, fileSepRegex.FindAllString(string(data), -1)...)
	}

	// Read the prior state
	prior_state, err := fid_lib.LoadState(prior_state_file, fid_lib.OsFs)
	if err != nil {
		panic(err)
	}
	// Create the new state
	new_state, err := fid_lib.CreateState(inputs, inspect, fid_lib.OsFs)
	if err != nil {
		panic(err)
	}
	if err = fid_lib.WriteState(new_state, new_state_file); err != nil {
		panic(err)
	}

	file_list := fid_lib.CompareInternalState(prior_state, new_state, target)

	if err = file_list.Format(os.Stdout, template); err != nil {
		panic(err)
	}

	metrics_dir := os.Getenv("SOONG_METRICS_AGGREGATION_DIR")
	out_dir := os.Getenv("OUT_DIR")
	if metrics_dir != "" {
		if err = file_list.WriteMetrics(metrics_dir, out_dir); err != nil {
			panic(err)
		}
	}
}
