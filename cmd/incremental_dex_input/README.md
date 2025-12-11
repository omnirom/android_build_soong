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

# Incremental Dex Input

[incremental_dex_input] command line tool. This tool can be used to find the correct subset
of java packages to be passed for incremental dexing

# Getting Started

## Inputs
* class jar, jar file containing java class files to be dexed.
* deps file, containing the dependencies for dex.
* dexTarget path to the output of ninja rule that triggers dex
* outputDir, path to a output dir where dex outputs are placed

## Output
* [dexTarget].rsp file, representing list of all java packages
* [dexTarget].inc.rsp file, representing list of java packages to be incrementally dexed
* [dexTarget].input.pc_state.new temp state file, representing the current state of all dex sources (java class files)
* [dexTarget].deps.pc_state.new temp state file, representing the current state of dex dependencies.

## Usage
```
incremental_dex_input --classesJar [classJar] --dexTarget [dexTargetPath] --deps [depsRspFile] --outputDir [outputDirPath]
```

## Notes
* This tool internally references the core logic of [find_input_delta] tool.
* All outputs are relative to the dexTarget path
* Same class jar, deps, when used for different targets will output *different* results.
* Once dex succeeds, the temp state files should be saved as current state files, to prepare for next iteration.
