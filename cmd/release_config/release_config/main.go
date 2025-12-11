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
	"fmt"
	"os"
	"path/filepath"

	rc_lib "android/soong/cmd/release_config/release_config_lib"
)

func main() {
	var top string
	var quiet bool
	var releaseConfigMapPaths rc_lib.StringList
	var mapsFile string
	var targetRelease string
	var outputDir string
	var err error
	var configs *rc_lib.ReleaseConfigs
	var json, pb, textproto, inheritance, container bool
	var hashFile string
	var product, variant string
	var allMake bool
	var useBuildVar, allowMissing bool
	var withVariant bool
	var guard bool

	defaultProduct := os.Getenv("TARGET_PRODUCT")
	defaultRelease := os.Getenv("TARGET_RELEASE")
	if defaultRelease == "" {
		defaultRelease = "trunk_staging"
	}
	defaultVariant := os.Getenv("TARGET_BUILD_VARIANT")
	if defaultVariant == "" {
		defaultVariant = "eng"
	}

	flag.StringVar(&top, "top", ".", "path to top of workspace")
	flag.StringVar(&product, "product", defaultProduct, "TARGET_PRODUCT for the build")
	flag.StringVar(&variant, "variant", defaultVariant, "TARGET_PRODUCT for the build")
	flag.BoolVar(&quiet, "quiet", false, "disable warning messages")
	flag.StringVar(&mapsFile, "maps-file", "", "path to a file containing a list of release_config_map.textproto paths")
	flag.Var(&releaseConfigMapPaths, "map", "path to a release_config_map.textproto. may be repeated")
	flag.StringVar(&targetRelease, "release", defaultRelease, "TARGET_RELEASE for this build")
	flag.BoolVar(&allowMissing, "allow-missing", false, "Use trunk_staging values if release not found")
	flag.StringVar(&outputDir, "out_dir", rc_lib.GetDefaultOutDir(), "basepath for the output. Multiple formats are created")
	flag.StringVar(&hashFile, "hashfile", "", "path in which to write a hash to determine when inputs have changed")
	flag.BoolVar(&textproto, "textproto", false, "write artifacts as text protobuf")
	flag.BoolVar(&json, "json", false, "write artifacts as json")
	flag.BoolVar(&pb, "pb", false, "write artifacts as binary protobuf")
	flag.BoolVar(&allMake, "all_make", false, "write makefiles for all release configs")
	flag.BoolVar(&inheritance, "inheritance", false, "write inheritance graph")
	flag.BoolVar(&useBuildVar, "use_get_build_var", false, "use get_build_var PRODUCT_RELEASE_CONFIG_MAPS")
	flag.BoolVar(&container, "container", false, "generate per-container build_flags.json artifacts")
	flag.BoolVar(&guard, "guard", false, "obsolete")
	flag.BoolVar(&withVariant, "with-variant", false, "include build variant in artifact names")

	flag.Parse()

	if quiet {
		rc_lib.DisableWarnings()
	}

	if mapsFile != "" {
		if len(releaseConfigMapPaths) > 0 {
			panic(fmt.Errorf("Cannot specify both --map and --maps-file"))
		}
		if err := releaseConfigMapPaths.ReadFromFile(mapsFile); err != nil {
			panic(fmt.Errorf("Could not read %s", mapsFile))
		}
	}

	if err = os.Chdir(top); err != nil {
		panic(err)
	}
	err = os.MkdirAll(outputDir, 0775)
	if err != nil {
		panic(err)
	}
	configs, err = rc_lib.ReadReleaseConfigMaps(releaseConfigMapPaths, targetRelease, variant, useBuildVar, allowMissing, false)
	if err != nil {
		panic(err)
	}
	config, err := configs.GetReleaseConfig(targetRelease)
	if err != nil {
		panic(err)
	}

	if hashFile != "" {
		if err := configs.WriteHashFile(hashFile); err != nil {
			panic(err)
		}
	}

	lunchable := func(release string) string {
		if withVariant {
			return fmt.Sprintf("%s-%s-%s", product, release, variant)
		}
		return fmt.Sprintf("%s-%s", product, release)
	}

	makefilePath := filepath.Join(outputDir, fmt.Sprintf("release_config-%s.varmk", lunchable(targetRelease)))
	// Write the makefile where release_config.mk is going to look for it.
	err = config.WriteMakefile(makefilePath, targetRelease, configs)
	if err != nil {
		panic(err)
	}
	if container {
		if err := config.WritePartitionBuildFlags(product, outputDir); err != nil {
			panic(err)
		}
	}
	// All of these artifacts require that we generate **ALL** release configs.
	if allMake || inheritance || json || pb || textproto {
		configs.GenerateAllReleaseConfigs(targetRelease)
		if allMake {
			// Write one makefile per release config, using the canonical release name.
			for _, c := range configs.GetSortedReleaseConfigs() {
				if c.Name != targetRelease && !c.DisallowLunchUse {
					makefilePath = filepath.Join(outputDir, fmt.Sprintf("release_config-%s.varmk", lunchable(c.Name)))
					err = config.WriteMakefile(makefilePath, c.Name, configs)
					if err != nil {
						panic(err)
					}
				}
			}
		}
		if inheritance {
			inheritPath := filepath.Join(outputDir, fmt.Sprintf("inheritance_graph-%s.dot", product))
			if err := configs.WriteInheritanceGraph(inheritPath); err != nil {
				panic(err)
			}
		}
		var product_variant string
		if withVariant {
			product_variant = product + "-" + variant
		} else {
			product_variant = product
		}

		if json {
			if err := configs.WriteArtifact(outputDir, product_variant, "json"); err != nil {
				panic(err)
			}
		}
		if pb {
			if err := configs.WriteArtifact(outputDir, product_variant, "pb"); err != nil {
				panic(err)
			}
		}
		if textproto {
			if err := configs.WriteArtifact(outputDir, product_variant, "textproto"); err != nil {
				panic(err)
			}
		}
	}
}
