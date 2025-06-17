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
	"strings"
	"sync"

	"github.com/google/blueprint"
)

var phonyMapOnceKey = NewOnceKey("phony")

type phonyMap map[string]Paths

var phonyMapLock sync.Mutex

type ModulePhonyInfo struct {
	Phonies map[string]Paths
}

var ModulePhonyProvider = blueprint.NewProvider[ModulePhonyInfo]()

func getSingletonPhonyMap(config Config) phonyMap {
	return config.Once(phonyMapOnceKey, func() interface{} {
		return make(phonyMap)
	}).(phonyMap)
}

func addSingletonPhony(config Config, name string, deps ...Path) {
	phonyMap := getSingletonPhonyMap(config)
	phonyMapLock.Lock()
	defer phonyMapLock.Unlock()
	phonyMap[name] = append(phonyMap[name], deps...)
}

type phonySingleton struct {
	phonyMap  phonyMap
	phonyList []string
}

var _ SingletonMakeVarsProvider = (*phonySingleton)(nil)

func (p *phonySingleton) GenerateBuildActions(ctx SingletonContext) {
	p.phonyMap = getSingletonPhonyMap(ctx.Config())
	ctx.VisitAllModuleProxies(func(m ModuleProxy) {
		if info, ok := OtherModuleProvider(ctx, m, ModulePhonyProvider); ok {
			for k, v := range info.Phonies {
				p.phonyMap[k] = append(p.phonyMap[k], v...)
			}
		}
	})

	p.phonyList = SortedKeys(p.phonyMap)
	for _, phony := range p.phonyList {
		p.phonyMap[phony] = SortedUniquePaths(p.phonyMap[phony])
	}

	if !ctx.Config().KatiEnabled() {
		// In soong-only builds, the phonies can conflict with dist targets that will
		// be generated in the packaging step. Instead of emitting a blueprint/ninja phony directly,
		// create a makefile that defines the phonies that will be included in the packaging step.
		// Make will dedup the phonies there.
		var buildPhonyFileContents strings.Builder
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
