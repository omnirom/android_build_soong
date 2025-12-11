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

package release_config_lib

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	rc_proto "android/soong/cmd/release_config/release_config_proto"

	"google.golang.org/protobuf/proto"
)

// A single release_config_map.textproto and its associated data.
// Used primarily for debugging.
type ReleaseConfigMap struct {
	// The path to this release_config_map file.
	path string

	// Data received
	proto rc_proto.ReleaseConfigMap

	// Map of name:contribution for release config contributions.
	ReleaseConfigContributions map[string]*ReleaseConfigContribution

	// Flags declared this directory's flag_declarations/*.textproto
	FlagArtifactsForDecls FlagArtifacts

	// Containers used in FlagDeclarations.
	BuildFlagContainersMap map[string]bool

	// Potential aconfig and build flag contributions in this map directory.
	// This is used to detect errors.
	FlagValueDirs map[string][]string

	// The index for this ReleaseConfigMap
	DirIndex int
}

type ReleaseConfigDirMap map[string]int

// The generated release configs.
type ReleaseConfigs struct {
	// Ordered list of release config maps processed.
	ReleaseConfigMaps []*ReleaseConfigMap

	// Aliases
	Aliases map[string]*string

	// Dictionary of flag_name:FlagDeclaration, with no overrides applied.
	FlagArtifacts FlagArtifacts

	// Containers used by build flags.
	BuildFlagContainers []string

	// Generated release configs artifact
	Artifact *rc_proto.ReleaseConfigsArtifact

	// Dictionary of name:ReleaseConfig
	// Use `GetReleaseConfigs(name)` to get a release config.
	ReleaseConfigs map[string]*ReleaseConfig

	// Map of directory to *ReleaseConfigMap
	releaseConfigMapsMap map[string]*ReleaseConfigMap

	// The list of config directories used.
	configDirs []string

	// A map from the config directory to its order in the list of config
	// directories.
	configDirIndexes ReleaseConfigDirMap

	// The TARGET_BUILD_VARIANT.
	targetBuildVariant string

	// True if we should allow a missing primary release config.  In this
	// case, we will substitute `trunk_staging` values, but the release
	// config will not be in ALL_RELEASE_CONFIGS_FOR_PRODUCT.
	allowMissing bool

	// Hash of all the paths used and their contents.
	FilesUsedHash []byte
}

func (configs *ReleaseConfigs) WriteInheritanceGraph(outFile string) error {
	if configs.Artifact == nil {
		return fmt.Errorf("all_release_configs artifact has not been generated yet")
	}
	data := []string{}
	usedAliases := make(map[string]bool)
	priorStages := make(map[string][]string)
	for _, config := range configs.ReleaseConfigs {
		if config.Name == "root" {
			continue
		}
		var fillColor string
		inherits := []string{}
		for _, inherit := range config.InheritNames {
			if inherit == "root" {
				continue
			}
			data = append(data, fmt.Sprintf(`"%s" -> "%s"`, config.Name, inherit))
			inherits = append(inherits, inherit)
			// If inheriting an alias, add a link from the alias to that release config.
			if name, found := configs.Aliases[inherit]; found {
				if !usedAliases[inherit] {
					usedAliases[inherit] = true
					data = append(data, fmt.Sprintf(`"%s" -> "%s"`, inherit, *name))
					data = append(data,
						fmt.Sprintf(`"%s" [ label="%s\ncurrently: %s" shape=oval ]`,
							inherit, inherit, *name))
				}
			}
		}
		// Add links for all of the advancement progressions.
		for priorStage := range config.PriorStagesMap {
			data = append(data, fmt.Sprintf(`"%s" -> "%s" [ style=dashed color="#81c995" ]`,
				priorStage, config.Name))
			priorStages[config.Name] = append(priorStages[config.Name], priorStage)
		}
		label := config.Name
		if len(inherits) > 0 {
			label += "\\ninherits: " + strings.Join(inherits, " ")
		}
		if len(config.OtherNames) > 0 {
			label += "\\nother names: " + strings.Join(config.OtherNames, " ")
		}
		switch config.Name {
		case *configs.Artifact.ReleaseConfig.Name:
			// The active release config has a light blue fill.
			fillColor = `fillcolor="#d2e3fc" `
		case "trunk", "trunk_staging":
			// Certain workflow stages have a light green fill.
			fillColor = `fillcolor="#ceead6" `
		default:
			// Look for "next" and "*_next", make them light green as well.
			for _, n := range config.OtherNames {
				if n == "next" || strings.HasSuffix(n, "_next") {
					fillColor = `fillcolor="#ceead6" `
				}
			}
		}
		data = append(data,
			fmt.Sprintf(`"%s" [ label="%s" %s]`, config.Name, label, fillColor))
	}
	slices.Sort(data)
	data = append([]string{
		"digraph {",
		"graph [ ratio=.5 ]",
		"node [ shape=box style=filled fillcolor=white colorscheme=svg fontcolor=black ]",
	}, data...)
	data = append(data, "}")
	return os.WriteFile(outFile, []byte(strings.Join(data, "\n")), 0644)
}

// Write the "all_release_configs" artifact.
//
// The file will be in "{outDir}/all_release_configs-{product}.{format}"
//
// Args:
//
//	outDir string: directory path. Will be created if not present.
//	product string: TARGET_PRODUCT for the release_configs.
//	format string: one of "json", "pb", or "textproto"
//
// Returns:
//
//	error: Any error encountered.
func (configs *ReleaseConfigs) WriteArtifact(outDir, product, format string) error {
	if configs.Artifact == nil {
		return fmt.Errorf("all_release_configs artifact has not been generated yet")
	}
	return WriteMessage(
		configs.AllReleaseConfigsPath(outDir, product, format),
		configs.Artifact)
}

func (configs *ReleaseConfigs) AllReleaseConfigsPath(outDir, product, format string) string {
	return filepath.Join(outDir, fmt.Sprintf("all_release_configs-%s.%s", product, format))
}

func ReleaseConfigsFactory(allowMissing bool, targetBuildVariant string) (c *ReleaseConfigs) {
	configs := ReleaseConfigs{
		Aliases:              make(map[string]*string),
		FlagArtifacts:        make(map[string]*FlagArtifact),
		ReleaseConfigs:       make(map[string]*ReleaseConfig),
		releaseConfigMapsMap: make(map[string]*ReleaseConfigMap),
		configDirs:           []string{},
		configDirIndexes:     make(ReleaseConfigDirMap),
		allowMissing:         allowMissing,
		targetBuildVariant:   targetBuildVariant,
	}
	workflowManual := rc_proto.Workflow(rc_proto.Workflow_MANUAL)
	releaseAconfigValueSets := FlagArtifact{
		FlagDeclaration: &rc_proto.FlagDeclaration{
			Name:        proto.String("RELEASE_ACONFIG_VALUE_SETS"),
			Namespace:   proto.String("build"),
			Description: proto.String("Aconfig value sets assembled by release-config"),
			Workflow:    &workflowManual,
			Containers:  []string{"system", "system_ext", "product", "vendor"},
			Value:       &rc_proto.Value{Val: &rc_proto.Value_UnspecifiedValue{false}},
		},
		DeclarationIndex: -1,
		Traces:           []*rc_proto.Tracepoint{},
	}
	configs.FlagArtifacts["RELEASE_ACONFIG_VALUE_SETS"] = &releaseAconfigValueSets
	return &configs
}

func (configs *ReleaseConfigs) GetSortedReleaseConfigs() (ret []*ReleaseConfig) {
	for _, config := range configs.ReleaseConfigs {
		ret = append(ret, config)
	}
	slices.SortFunc(ret, func(a, b *ReleaseConfig) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return ret
}

func ReleaseConfigMapFactory(protoPath string, idx int) (m *ReleaseConfigMap, err error) {
	m = &ReleaseConfigMap{
		path:                       protoPath,
		ReleaseConfigContributions: make(map[string]*ReleaseConfigContribution),
		DirIndex:                   idx,
		FlagArtifactsForDecls:      make(FlagArtifacts),
		BuildFlagContainersMap:     make(map[string]bool),
	}
	if protoPath == "" {
		return m, nil
	}
	LoadMessage(protoPath, &m.proto)
	if m.proto.DefaultContainers == nil {
		return nil, fmt.Errorf("Release config map %s lacks default_containers", protoPath)
	}
	for _, container := range m.proto.DefaultContainers {
		if !validContainer(container) {
			return nil, fmt.Errorf("Release config map %s has invalid container %s", protoPath, container)
		}
		m.BuildFlagContainersMap[container] = true
	}
	return m, nil
}

// Find the top of the release config contribution directory.
// Returns the parent of the flag_declarations and flag_values directories.
func (configs *ReleaseConfigs) GetDirIndex(path string) (int, error) {
	for p := path; p != "."; p = filepath.Dir(p) {
		if idx, ok := configs.configDirIndexes[p]; ok {
			return idx, nil
		}
	}
	return -1, fmt.Errorf("Could not determine release config directory from %s", path)
}

// Determine the default directory for writing a flag value.
//
// Returns the path of the highest-Indexed one of:
//   - Where the flag is declared
//   - Where the release config is first declared
//   - The last place the value is being written.
func (configs *ReleaseConfigs) GetFlagValueDirectory(config *ReleaseConfig, flag *FlagArtifact) (string, error) {
	current, err := configs.GetDirIndex(*flag.Traces[len(flag.Traces)-1].Source)
	if err != nil {
		return "", err
	}
	index := max(flag.DeclarationIndex, config.DeclarationIndex, current)
	return configs.configDirs[index], nil
}

// Return the (unsorted) release configs contributed to by `dir`.
func EnumerateReleaseConfigs(dir string) ([]string, error) {
	var ret []string
	err := WalkTextprotoFiles(dir, "release_configs", func(path string, d fs.DirEntry, err error) error {
		// Strip off the trailing `.textproto` from the name.
		name := filepath.Base(path)
		ret = append(ret, name[:len(name)-10])
		return err
	})
	return ret, err
}

type declReq struct {
	Map  *ReleaseConfigMap
	Path *string
}

type declInfo struct {
	Req      *declReq
	Artifact *FlagArtifact
}

type contribReq struct {
	Map  *ReleaseConfigMap
	Path *string
}

type contribInfo struct {
	Req     *contribReq
	Contrib *ReleaseConfigContribution
}

type valueReq struct {
	Map        *ReleaseConfigMap
	ConfigName *string
	Path       *string
}

type valueInfo struct {
	Req   *valueReq
	Value *FlagValue
}

type fileInfo struct {
	decl    *declInfo
	contrib *contribInfo
	value   *valueInfo
}

type loadContext struct {
	declarationsOnly bool
	errorsChan       chan error
	errorsWg         sync.WaitGroup

	// WaitGroup for threads processing a directory.
	dirReaderWg sync.WaitGroup

	// Requests to read a flag_declarations/ file and send it to declHandlerChan.
	declReaderChan chan *declReq
	declReaderWg   sync.WaitGroup
	//declHandlerChan chan *declInfo

	// Requests to read a release_configs/ file and send it to contribHandlerChan
	contribReaderChan chan *contribReq
	contribReaderWg   sync.WaitGroup
	//contribHandlerChan chan *contribInfo

	// Requests to read flag_values/ file and send it to valueHandlerChan
	valueReaderChan chan *valueReq
	valueReaderWg   sync.WaitGroup
	//valueHandlerChan chan *valueInfo

	// Reads and processes data from declHandlerChan, contribHandlerChan, and
	// valueHandlerChan.
	infoHandlerWg   sync.WaitGroup
	infoHandlerChan chan *fileInfo
}

func createLoadContext(configs *ReleaseConfigs, declarationsOnly bool) (ctx *loadContext) {
	startFileRecord()
	numCPU := runtime.NumCPU()
	ctx = &loadContext{
		declarationsOnly:  declarationsOnly,
		errorsChan:        make(chan error, 40),
		declReaderChan:    make(chan *declReq, 20),
		contribReaderChan: make(chan *contribReq, 20),
		valueReaderChan:   make(chan *valueReq, 20),
		infoHandlerChan:   make(chan *fileInfo),
	}
	// dirReader funcs are added as we read directories.
	// Create several threads to read flag_declarations, release_configs,
	// and flag_values files.
	for i := 0; i < numCPU; i++ {
		ctx.declReaderWg.Add(1)
		go ctx.declReader()
		ctx.valueReaderWg.Add(1)
		go ctx.valueReader()
		ctx.contribReaderWg.Add(1)
		go ctx.contribReader()
	}
	// Only one thread to process all of the info as it arrives.
	ctx.infoHandlerWg.Add(1)
	go ctx.infoHandler()
	// After we finish collecting all of the textproto files, Finalize() will
	// do validation and set final values based on all the textproto files that
	// were read.
	return ctx
}

func (ctx *loadContext) declReader() {
	defer ctx.declReaderWg.Done()
	for req := range ctx.declReaderChan {
		fa, err := FlagArtifactFactory(*req.Path, req.Map.DirIndex)
		if err != nil {
			ctx.errorsChan <- err
			continue
		}
		// If not given, set Containers to the default for this directory.
		if fa.FlagDeclaration.Containers == nil {
			fa.FlagDeclaration.Containers = req.Map.proto.DefaultContainers
		}
		name := *fa.FlagDeclaration.Name
		if fa.Redacted {
			ctx.errorsChan <- fmt.Errorf("%s may not be redacted by default.", name)
			continue
		}
		ctx.infoHandlerChan <- &fileInfo{decl: &declInfo{Req: req, Artifact: fa}}
	}
}

func (ctx *loadContext) infoHandler() {
	defer ctx.infoHandlerWg.Done()
	for info := range ctx.infoHandlerChan {
		if info.decl != nil {
			m := info.decl.Req.Map
			m.FlagArtifactsForDecls[*info.decl.Artifact.FlagDeclaration.Name] = info.decl.Artifact
			for _, container := range info.decl.Artifact.FlagDeclaration.GetContainers() {
				m.BuildFlagContainersMap[container] = true
			}
		} else if info.contrib != nil {
			// information from a release_configs/ file.
			name := *info.contrib.Contrib.proto.Name
			m := info.contrib.Req.Map
			if def, ok := m.ReleaseConfigContributions[name]; ok {
				// We have already processed flag_values for this release config contribution.
				info.contrib.Contrib.FlagValues = def.FlagValues
			}
			m.ReleaseConfigContributions[name] = info.contrib.Contrib
		} else if info.value != nil {
			// information from a flag_values/ file.
			m := info.value.Req.Map
			// The flag_values/ info may arrive before the release_configs/ info.
			var err error
			rcc, ok := m.ReleaseConfigContributions[*info.value.Req.ConfigName]
			if !ok {
				rcc, err = ReleaseConfigContributionFactory("", m.DirIndex)
				if err != nil {
					ctx.errorsChan <- err
					continue
				}
				m.ReleaseConfigContributions[*info.value.Req.ConfigName] = rcc
			}
			rcc.FlagValues[*info.value.Value.proto.Name] = info.value.Value
		} else {
			ctx.errorsChan <- fmt.Errorf("Unparsable read result: %v", *info)
		}
	}
}

func (ctx *loadContext) contribReader() {
	defer ctx.contribReaderWg.Done()
	for req := range ctx.contribReaderChan {
		rcc, err := ReleaseConfigContributionFactory(*req.Path, req.Map.DirIndex)
		if err != nil {
			ctx.errorsChan <- err
			continue
		}
		ctx.infoHandlerChan <- &fileInfo{contrib: &contribInfo{Req: req, Contrib: rcc}}
	}
}

func (ctx *loadContext) valueReader() {
	defer ctx.valueReaderWg.Done()
	for req := range ctx.valueReaderChan {
		value, err := FlagValueFactory(*req.Path)
		if err != nil {
			ctx.errorsChan <- err
			continue
		}
		ctx.infoHandlerChan <- &fileInfo{value: &valueInfo{Req: req, Value: value}}
	}
}

func (configs *ReleaseConfigs) LoadReleaseConfigMap(ctx *loadContext, path string, ConfigDirIndex int, declarationsOnly bool) (*ReleaseConfigMap, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%s does not exist\n", path)
	}
	m, err := ReleaseConfigMapFactory(path, ConfigDirIndex)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	// Record any aliases, checking for duplicates.
	for _, alias := range m.proto.Aliases {
		name := *alias.Name
		oldTarget, ok := configs.Aliases[name]
		if ok {
			if *oldTarget != *alias.Target {
				ctx.errorsChan <- fmt.Errorf("Conflicting alias declarations: %s vs %s",
					*oldTarget, *alias.Target)
				continue
			}
		}
		configs.Aliases[name] = alias.Target
	}

	// Temporarily allowlist duplicate flag declaration files to prevent
	// more from entering the tree while we work to clean up the duplicates
	// that already exist.
	dupFlagFile := filepath.Join(dir, "duplicate_allowlist.txt")
	data, err := ReadTrackedFile(dupFlagFile)
	if err == nil {
		for _, flag := range strings.Split(string(data), "\n") {
			flag = strings.TrimSpace(flag)
			if strings.HasPrefix(flag, "//") || strings.HasPrefix(flag, "#") {
				continue
			}
			DuplicateDeclarationAllowlist[flag] = true
		}
	}

	err = WalkTextprotoFiles(dir, "flag_declarations", func(path string, d fs.DirEntry, err error) error {
		// Gather up all errors found in flag declarations and report them together, so that it is easier to
		// find all of the duplicate declarations, for example.
		ctx.declReaderChan <- &declReq{
			Map:  m,
			Path: &path,
		}

		// Set the initial value in the flag artifact.
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = WalkTextprotoFiles(dir, "release_configs", func(path string, d fs.DirEntry, err error) error {
		ctx.contribReaderChan <- &contribReq{
			Map:  m,
			Path: &path,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	subDirs := func(subdir string) (ret []string) {
		if flagVersions, err := os.ReadDir(filepath.Join(dir, subdir)); err == nil {
			for _, e := range flagVersions {
				if e.IsDir() && validReleaseConfigName(e.Name()) {
					ret = append(ret, e.Name())
				}
			}
		}
		return
	}
	m.FlagValueDirs = map[string][]string{
		"aconfig":     subDirs("aconfig"),
		"flag_values": subDirs("flag_values"),
	}

	if !declarationsOnly {
		for _, rcName := range m.FlagValueDirs["flag_values"] {
			err := WalkTextprotoFiles(dir, filepath.Join("flag_values", rcName), func(path string, d fs.DirEntry, err error) error {
				ctx.valueReaderChan <- &valueReq{
					Map:        m,
					ConfigName: &rcName,
					Path:       &path,
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}

	configs.releaseConfigMapsMap[dir] = m
	return m, nil
}

func (configs *ReleaseConfigs) GetReleaseConfig(name string) (*ReleaseConfig, error) {
	return configs.getReleaseConfig(name, configs.allowMissing, true)
}

func (configs *ReleaseConfigs) GetReleaseConfigStrict(name string) (*ReleaseConfig, error) {
	return configs.getReleaseConfig(name, false, true)
}

func (configs *ReleaseConfigs) getReleaseConfig(name string, allow_missing bool, generate bool) (*ReleaseConfig, error) {
	trace := []string{name}
	for target, ok := configs.Aliases[name]; ok; target, ok = configs.Aliases[name] {
		name = *target
		trace = append(trace, name)
	}
	if config, ok := configs.ReleaseConfigs[name]; ok {
		var err error
		if generate {
			err = config.GenerateReleaseConfig(configs)
		}
		return config, err
	}
	if allow_missing {
		if config, ok := configs.ReleaseConfigs["trunk_staging"]; ok {
			return config, nil
		}
	}
	return nil, fmt.Errorf("Missing config %s.  Trace=%v", name, trace)
}

func (configs *ReleaseConfigs) GetAllReleaseNames() []string {
	var allReleaseNames []string
	for _, v := range configs.ReleaseConfigs {
		if v.isConfigListable() {
			allReleaseNames = append(allReleaseNames, v.Name)
			allReleaseNames = append(allReleaseNames, v.OtherNames...)
		}
	}
	slices.Sort(allReleaseNames)
	return allReleaseNames
}

func (configs *ReleaseConfigs) GenerateAllReleaseConfigs(targetRelease string) error {
	if configs.Artifact != nil {
		return nil
	}
	releaseConfig, err := configs.getReleaseConfig(targetRelease, configs.allowMissing, false)
	if err != nil {
		return err
	}
	sortedReleaseConfigs := configs.GetSortedReleaseConfigs()
	orc := []*rc_proto.ReleaseConfigArtifact{}

	for _, c := range sortedReleaseConfigs {
		err := c.GenerateReleaseConfig(configs)
		if err != nil {
			return err
		}
		if c.Name != releaseConfig.Name {
			orc = append(orc, c.ReleaseConfigArtifact)
		}
	}

	configs.Artifact = &rc_proto.ReleaseConfigsArtifact{
		ReleaseConfig:       releaseConfig.ReleaseConfigArtifact,
		OtherReleaseConfigs: orc,
		ReleaseConfigMapsMap: func() map[string]*rc_proto.ReleaseConfigMap {
			ret := make(map[string]*rc_proto.ReleaseConfigMap)
			for k, v := range configs.releaseConfigMapsMap {
				ret[k] = &v.proto
			}
			return ret
		}(),
	}
	return nil
}

func (configs *ReleaseConfigs) GenerateReleaseConfigs(targetRelease string) error {
	_, err := configs.GetReleaseConfig(targetRelease)
	return err
}

func ReadReleaseConfigMaps(releaseConfigMapPaths StringList, targetRelease, variant string, useBuildVar, allowMissing, declarationsOnly bool) (*ReleaseConfigs, error) {
	var err error

	if len(releaseConfigMapPaths) == 0 {
		releaseConfigMapPaths, err = GetDefaultMapPaths(useBuildVar)
		if err != nil {
			return nil, err
		}
		if len(releaseConfigMapPaths) == 0 {
			return nil, fmt.Errorf("No maps found")
		}
		if !useBuildVar {
			warnf("No --map argument provided.  Using: --map %s\n", strings.Join(releaseConfigMapPaths, " --map "))
		}
	}

	configs := ReleaseConfigsFactory(allowMissing, variant)
	ctx := createLoadContext(configs, declarationsOnly)

	var loadErrors []error
	ctx.errorsWg.Add(1)
	go func() {
		defer ctx.errorsWg.Done()
		for err := range ctx.errorsChan {
			loadErrors = append(loadErrors, err)
		}
	}()

	mapsRead := make(map[string]bool)
	var idx int
	for _, releaseConfigMapPath := range releaseConfigMapPaths {
		// Maintain an ordered list of release config directories.
		configDir := filepath.Dir(releaseConfigMapPath)
		if mapsRead[configDir] {
			continue
		}
		mapsRead[configDir] = true
		configs.configDirIndexes[configDir] = idx
		configs.configDirs = append(configs.configDirs, configDir)
		// Force the path to be the textproto path, so that both the scl and textproto formats can coexist.
		releaseConfigMapPath = filepath.Join(configDir, "release_config_map.textproto")
		m, err := configs.LoadReleaseConfigMap(ctx, releaseConfigMapPath, idx, declarationsOnly)
		if err != nil {
			ctx.errorsChan <- err
		}
		configs.ReleaseConfigMaps = append(configs.ReleaseConfigMaps, m)
		configs.releaseConfigMapsMap[configDir] = m
		idx += 1
	}

	// Finish starting all of the file reads
	ctx.dirReaderWg.Wait()
	close(ctx.declReaderChan)
	close(ctx.contribReaderChan)
	close(ctx.valueReaderChan)

	// Wait for all file reads to complete
	ctx.declReaderWg.Wait()
	ctx.contribReaderWg.Wait()
	ctx.valueReaderWg.Wait()
	close(ctx.infoHandlerChan)
	// Finish processing info from the reads.
	ctx.infoHandlerWg.Wait()

	configs.Finalize(ctx, targetRelease)
	close(ctx.errorsChan)
	ctx.errorsWg.Wait()
	configs.FilesUsedHash = finishFileRecord()
	if len(loadErrors) > 0 {
		return nil, errors.Join(loadErrors...)
	}

	// Now that we have all of the release config maps, can meld them and generate the active config.
	err = configs.GenerateReleaseConfigs(targetRelease)
	return configs, err
}

func (configs *ReleaseConfigs) Finalize(ctx *loadContext, targetRelease string) error {
	buildFlagContainersMap := make(map[string]bool)
	for _, m := range configs.ReleaseConfigMaps {
		dirName := filepath.Dir(m.path)
		for _, fa := range m.FlagArtifactsForDecls {
			name := *fa.FlagDeclaration.Name
			path := *fa.DeclarationPath
			if def, ok := configs.FlagArtifacts[name]; !ok {
				configs.FlagArtifacts[name] = fa
			} else if !proto.Equal(def.FlagDeclaration, fa.FlagDeclaration) || !DuplicateDeclarationAllowlist[name] {
				ctx.errorsChan <- fmt.Errorf("Duplicate definition of %s in %s and %s", name, path,
					*configs.FlagArtifacts[name].DeclarationPath)
				continue
			} else {
				// Note the second definition in the trace.
				configs.FlagArtifacts[name].Traces = append(configs.FlagArtifacts[name].Traces, fa.Traces...)
			}
			for container := range m.BuildFlagContainersMap {
				buildFlagContainersMap[container] = true
			}
		}

		for name, rcc := range m.ReleaseConfigContributions {
			if _, ok := configs.ReleaseConfigs[name]; !ok {
				configs.ReleaseConfigs[name] = ReleaseConfigFactory(name, m.DirIndex)
				configs.ReleaseConfigs[name].ReleaseConfigType = rcc.proto.GetReleaseConfigType()
			}
			config := configs.ReleaseConfigs[name]
			if config.ReleaseConfigType != *rcc.proto.ReleaseConfigType {
				ctx.errorsChan <- fmt.Errorf("%s mismatching ReleaseConfigType value %s", rcc.path, *rcc.proto.ReleaseConfigType)
				continue
			}

			for _, inh := range rcc.proto.Inherits {
				if !config.inheritNamesMap[inh] {
					config.InheritNames = append(config.InheritNames, inh)
					config.inheritNamesMap[inh] = true
				}
			}
			config.AconfigFlagsOnly = config.AconfigFlagsOnly || rcc.proto.GetAconfigFlagsOnly()
			config.DisallowLunchUse = config.DisallowLunchUse || rcc.proto.GetDisallowLunchUse()
			config.Contributions = append(config.Contributions, rcc)
		}
		// Look for flag values for release configs that are not declared in `release_configs/`.
		for k, names := range m.FlagValueDirs {
			for _, rcName := range names {
				if strings.HasSuffix(rcName, "_ro_snapshot") {
					continue
				}
				rcPath := filepath.Join(dirName, "release_configs", fmt.Sprintf("%s.textproto", rcName))
				if _, err := os.Stat(rcPath); err != nil {
					ctx.errorsChan <- fmt.Errorf("%s exists but %s does not contribute to %s",
						filepath.Join(dirName, k, rcName), dirName, rcName)
				}
			}
		}
	}
	for k := range buildFlagContainersMap {
		configs.BuildFlagContainers = append(configs.BuildFlagContainers, k)
	}
	slices.Sort(configs.BuildFlagContainers)

	// Link all of the aliases.
	otherNames := make(map[string][]string)
	for aliasName, aliasTarget := range configs.Aliases {
		if _, ok := configs.ReleaseConfigs[aliasName]; ok {
			ctx.errorsChan <- fmt.Errorf("Alias %s is a declared release config", aliasName)
		}
		if _, ok := configs.ReleaseConfigs[*aliasTarget]; !ok {
			if _, ok2 := configs.Aliases[*aliasTarget]; !ok2 {
				ctx.errorsChan <- fmt.Errorf("Alias %s points to non-existing config %s", aliasName, *aliasTarget)
			}
		}
		otherNames[*aliasTarget] = append(otherNames[*aliasTarget], aliasName)
	}
	for name, aliases := range otherNames {
		configs.ReleaseConfigs[name].OtherNames = aliases
	}

	return nil
}

// Write the depfile for this release config run.
func (configs *ReleaseConfigs) WriteHashFile(hashFilePath string) error {
	return os.WriteFile(hashFilePath, configs.FilesUsedHash, 0644)
}
