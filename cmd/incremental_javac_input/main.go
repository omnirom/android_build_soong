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

package main

import (
	"flag"
	"fmt"

	iji_lib "android/soong/cmd/incremental_javac_input/incremental_javac_input_lib"
)

func main() {
	var classDir, srcs, deps, javacTarget, srcDepsProto, localHeaderJars, crossModuleJarRsp string

	flag.StringVar(&classDir, "classDir", "", "dir which will contain compiled java classes")
	flag.StringVar(&srcs, "srcs", "", "rsp file containing java source paths")
	flag.StringVar(&deps, "deps", "", "rsp file enlisting all module deps")
	flag.StringVar(&javacTarget, "javacTarget", "", "javac output")
	flag.StringVar(&srcDepsProto, "srcDepsProto", "", "dependency map between src files in a proto")
	flag.StringVar(&localHeaderJars, "localHeaderJars", "", "rsp file enlisting all local header jars")
	flag.StringVar(&crossModuleJarRsp, "crossModuleJarList", "", "rsp file listing all kotlin jars used for compilation")

	flag.Parse()

	if classDir == "" {
		panic("must specify --classDir")
	}

	if deps == "" {
		panic("must specify --deps")
	}

	if javacTarget == "" {
		panic("must specify --javacTarget")
	}

	if localHeaderJars == "" {
		panic("must specify --localHeaderJars")
	}

	if srcDepsProto == "" {
		panic("must specify --depsProto")
	}

	if srcs == "" {
		panic("must specify --srcs")
	}

	if srcs != "" {
		err := iji_lib.GenerateIncrementalInput(classDir, srcs, deps, javacTarget, srcDepsProto, localHeaderJars, crossModuleJarRsp)
		if err != nil {
			panic("errored")
		}
	} else {
		fmt.Println("No source files provided via --srcs flag.")
	}
}
