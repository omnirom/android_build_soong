// Copyright 2020 Google Inc. All rights reserved.
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

package android

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/google/blueprint"
)

//go:generate go run ../../blueprint/gobtools/codegen/gob_gen.go

type phonyMap map[string]Paths

var phonyMapLock sync.Mutex

// @auto-generate: gob
type PhonyInfo struct {
	Phonies phonyMap
}

var ModulePhonyProvider = blueprint.NewProvider[PhonyInfo]()

var SingletonPhonyProvider = blueprint.NewSingletonProvider[PhonyInfo]()

type phonySingleton struct {
	phonyMap  phonyMap
	phonyList []string
}

var _ SingletonMakeVarsProvider = (*phonySingleton)(nil)

func (p *phonySingleton) GenerateBuildActions(ctx SingletonContext) {
	p.phonyMap = make(phonyMap)
	ctx.VisitAllModuleProxies(func(m ModuleProxy) {
		if info, ok := OtherModuleProvider(ctx, m, ModulePhonyProvider); ok {
			for k, v := range info.Phonies {
				p.phonyMap[k] = append(p.phonyMap[k], v...)
			}
		}
	})

	ctx.VisitAllSingletons(func(s blueprint.SingletonProxy) {
		if info, ok := OtherSingletonProvider(ctx, s, SingletonPhonyProvider); ok {
			for k, v := range info.Phonies {
				p.phonyMap[k] = append(p.phonyMap[k], v...)
			}
		}
	})

	// We will sort phonyList in parallel with other stuff later, but for now copy it into
	// a slice in series so that we don't read and write to phonyMap concurrently.
	p.phonyList = make([]string, 0, len(p.phonyMap))
	for phony := range p.phonyMap {
		p.phonyList = append(p.phonyList, phony)
	}

	type phonyDef struct {
		name string
		deps Paths
	}

	sortChan := make(chan phonyDef, len(p.phonyMap))
	resultsChan := make(chan phonyDef)
	var wg sync.WaitGroup

	// Sorting the phony deps in parallel saves about 2 seconds. Nothing runs in parallel with
	// the phony singleton so it's time off of wall clock.
	for i := 0; i < 2*runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			for toSort := range sortChan {
				toSort.deps = SortedUniquePaths(toSort.deps)
				resultsChan <- toSort
			}
			wg.Done()
		}()
	}

	go func() {
		sort.Strings(p.phonyList)
		wg.Wait()
		close(resultsChan)
	}()

	for phony, deps := range p.phonyMap {
		sortChan <- phonyDef{
			name: phony,
			deps: deps,
		}
	}
	close(sortChan)

	for result := range resultsChan {
		p.phonyMap[result.name] = result.deps
	}

	if !ctx.Config().KatiEnabled() {
		// In soong-only builds, the phonies can conflict with dist targets that will
		// be generated in the packaging step. Instead of emitting a blueprint/ninja phony directly,
		// create a makefile that defines the phonies that will be included in the packaging step.
		// Make will dedup the phonies there.
		phonyFileSize := 0
		for _, phony := range p.phonyList {
			phonyFileSize += 2*len(phony) + 11
			for _, dep := range p.phonyMap[phony] {
				phonyFileSize += len(dep.String()) + 1
			}
		}
		var buildPhonyFileContents strings.Builder
		buildPhonyFileContents.Grow(phonyFileSize)
		for _, phony := range p.phonyList {
			buildPhonyFileContents.WriteString(".PHONY: ")
			buildPhonyFileContents.WriteString(phony)
			buildPhonyFileContents.WriteString("\n")
			buildPhonyFileContents.WriteString(phony)
			buildPhonyFileContents.WriteString(":")
			for _, dep := range p.phonyMap[phony] {
				buildPhonyFileContents.WriteString(" ")
				buildPhonyFileContents.WriteString(dep.String())
			}
			buildPhonyFileContents.WriteString("\n")
		}
		if buildPhonyFileContents.Len() != phonyFileSize {
			panic(fmt.Sprintf("phonyFileSize calculation incorrect, expected %d, actual len: %d", phonyFileSize, buildPhonyFileContents.Len()))
		}
		buildPhonyFile := PathForOutput(ctx, "soong_phony_targets.mk")
		writeValueIfChanged(ctx, absolutePath(buildPhonyFile.String()), buildPhonyFileContents.String())
	}
}

func (p phonySingleton) MakeVars(ctx MakeVarsContext) {
	for _, phony := range p.phonyList {
		ctx.Phony(phony, p.phonyMap[phony]...)
	}
}

func phonySingletonFactory() Singleton {
	return &phonySingleton{}
}

func (p *phonySingleton) IncrementalSupported() bool {
	return true
}
