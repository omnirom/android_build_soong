// Copyright 2025 Google Inc. All rights reserved.
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

package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// `m update-api` copies files that are generated during the build back into the source tree.
// We want the source tree to be read-only during the build, so the real build will just build
// the generated files under out/, and then soong-ui will do the copying to the source tree after
// the build here.
func runUpdateApi(ctx Context, config Config) {
	wantedModules := make(map[string]bool)
	wantAllModules := false
	for _, ninjaArg := range config.NinjaArgs() {
		if ninjaArg == "update-api" {
			wantAllModules = true
			break
		}
		if strings.HasSuffix(ninjaArg, "-update-current-api") {
			wantedModules[strings.TrimSuffix(ninjaArg, "-update-current-api")] = true
		}
	}
	if !wantAllModules && len(wantedModules) == 0 {
		return
	}

	updateApiFile := filepath.Join(config.OutDir(), "soong", "update_api.txt")
	contents, err := os.ReadFile(updateApiFile)
	if err != nil {
		ctx.Fatalf("Failed to read %s: %s", updateApiFile, err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines)%5 != 0 {
		ctx.Fatalf("Invalid update api file: %s", updateApiFile)
	}
	seenDsts := make(map[string]string)
	for i := 0; i < len(lines); i += 5 {
		if wantAllModules || wantedModules[lines[i]] {
			generatedApi := lines[i+1]
			sourceApi := lines[i+2]
			generatedRemoved := lines[i+3]
			sourceRemoved := lines[i+4]
			copyUpdateApiFile(ctx, seenDsts, generatedApi, sourceApi)
			copyUpdateApiFile(ctx, seenDsts, generatedRemoved, sourceRemoved)
		}
	}
}

func copyUpdateApiFile(ctx Context, seen map[string]string, generated, source string) {
	if g, ok := seen[source]; ok {
		// Multiple modules/variants are trying to copy to copy to the same source file.
		// If their contents are the same, ignore it, otherwise throw an error.
		if areFilesSame(ctx, generated, g) {
			return
		}
		ctx.Fatalf("Multiple update-api files copy to %s: %s and %s", source, generated, g)
	}
	seen[source] = generated
	copyFileIfChanged(ctx, generated, source)
}

func areFilesSame(ctx Context, a, b string) bool {
	aContents, err := os.ReadFile(a)
	if err != nil {
		ctx.Fatalf("Failed to read %s", a)
	}
	bContents, err := os.ReadFile(b)
	if err != nil {
		ctx.Fatalf("Failed to read %s", b)
	}
	return bytes.Equal(aContents, bContents)
}

func runUpdateAidlApi(ctx Context, config Config) {
	wantUpdateModules := make(map[string]bool)
	wantFreezeModules := make(map[string]bool)
	allFreezes := false
	for _, ninjaArg := range config.NinjaArgs() {
		if ninjaArg == "aidl-freeze-api" {
			allFreezes = true
		}
		if strings.HasSuffix(ninjaArg, "-update-api") {
			wantUpdateModules[strings.TrimSuffix(ninjaArg, "-update-api")] = true
		} else if strings.HasSuffix(ninjaArg, "-freeze-api") {
			wantFreezeModules[strings.TrimSuffix(ninjaArg, "-freeze-api")] = true
		}
	}
	if !allFreezes && len(wantUpdateModules) == 0 && len(wantFreezeModules) == 0 {
		return
	}

	// Remove the transitive freeze file because it will be filled out while running other freeze
	// scripts.
	transitiveFreezeFile := filepath.Join(config.OutDir(), "soong", "aidl_transitive_freeze_apis.txt")
	if err := os.Remove(transitiveFreezeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		ctx.Fatalf("Failed to remove %s: %s", transitiveFreezeFile, err)
	}

	updateApiFile := filepath.Join(config.OutDir(), "soong", "aidl_update_api.txt")
	contents, err := os.ReadFile(updateApiFile)
	if err != nil {
		ctx.Fatalf("Failed to read %s: %s", updateApiFile, err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines)%4 != 0 {
		ctx.Fatalf("Invalid aidl update api file: %s", updateApiFile)
	}
	ranFreezeScripts := make(map[string]bool)

	runFreezeScript := func(freezeScript string) {
		cmd := exec.Command(freezeScript)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			ctx.Fatalf("freeze api failed")
		}
		ranFreezeScripts[freezeScript] = true
	}

	for i := 0; i < len(lines); i += 4 {
		if wantUpdateModules[lines[i]] {
			updateScript := lines[i+2]
			cmd := exec.Command(updateScript)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				ctx.Fatalf("update api failed")
			}
		}
		if wantFreezeModules[lines[i]] || (allFreezes && lines[i+1] == "true") {
			freezeScript := lines[i+3]
			runFreezeScript(freezeScript)
		}
	}

	// Freeze scripts can request that other freeze scripts from their transitive deps be run.
	// Each freeze script should only be run once, so they can't just run their deps' scripts
	// directly or else some scripts may be run multiple times. Instead, they dump the transitive
	// scripts they want to run into aidl_transitive_freeze_apis.txt, which we read here and run
	// the scripts if they haven't been run yet.
	newFreezes := len(ranFreezeScripts) > 0
	for newFreezes {
		contents, err := os.ReadFile(transitiveFreezeFile)
		if errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			ctx.Fatalf("Failed to read %s: %s", transitiveFreezeFile, err)
		}
		transitiveFreezes := strings.Split(strings.TrimSpace(string(contents)), "\n")

		// Delete the file so we don't waste time rereading old lines that we've already processed.
		if err := os.Remove(transitiveFreezeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			ctx.Fatalf("Failed to remove %s: %s", transitiveFreezeFile, err)
		}

		newFreezes = false
		for _, freezeScript := range transitiveFreezes {
			if !ranFreezeScripts[freezeScript] {
				newFreezes = true
				runFreezeScript(freezeScript)
			}
		}
	}
}
