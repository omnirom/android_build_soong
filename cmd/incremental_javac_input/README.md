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

# Incremental Javac Input

[incremental_javac_input] command line tool. This tool can be used to find the correct subset
of java files to be passed for incremental javac compilation.

# Getting Started

## Inputs
* rsp file, containing list of java source files separated by whitespace.
* deps file, containing the dependencies for javac.
* javacTarget path to the output jar of javac
* srcDepsProto, path to a proto file representing dependencies across java source files.
* localHeaderJars rsp file containing space separated header jar path(s) for java sources.
* crossModuleJarList *(optional)* rsp file containing space separated jar path(s) for dependencies
  compiled along side this jar (i.e. kotlin)


## Output
* [javacTarget].inc.rsp file, representing list of java source files for incremental compilation.
* [javacTarget].rem.rsp file, representing the list of .class files whose sources were removed and
  hence should be cleaned.
* [javacTarget].input.pc_state.new temp state file, representing the current state of all java
  sources.
* [javacTarget].deps.pc_state.new temp state file, representing the current state of dependencies.
* [javacTarget].headers.pc_state.new temp state file, representing the current state of java source
  headers.
* [javacTarget].crossModuleDeps.pc_state.new temp state file, representing the current state of
  cross-module dependencies.

## Usage
```
incremental_javac_input \
--srcs <srcRspFile> \
--deps <depsRspFile> \
--javacTarget <javacTargetPath> \
--srcDepsProto <srcDepsProtoPath> \
--localHeaderJars <localHeaderJarsRspFile> \
[--crossModuleJarList <crossModuleJarsRspFile>]
```

## Notes
* This tool internally references the core logic of [find_input_delta] tool.
* All outputs are relative to the javacTarget path
* Same sources, deps, headers when used for different targets will output different results.
* Once javac succeeds, the temp state files should be saved as current state files, to prepare for
  next iteration.