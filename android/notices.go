// Copyright 2019 Google Inc. All rights reserved.
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
	"path/filepath"
	"strings"

	"github.com/google/blueprint/proptools"
)

func modulesOutputDirs(ctx BuilderContext, modules ...ModuleProxy) []string {
	dirs := make([]string, 0, len(modules))
	for _, module := range modules {
		paths, err := outputFilesForModule(ctx, module, "")
		if err != nil {
			continue
		}
		for _, path := range paths {
			if path != nil {
				dirs = append(dirs, filepath.Dir(path.String()))
			}
		}
	}
	return SortedUniqueStrings(dirs)
}

type BuilderAndOtherModuleProviderContext interface {
	BuilderContext
	OtherModuleProviderContext
}

func modulesLicenseMetadata(ctx OtherModuleProviderContext, modules ...ModuleProxy) Paths {
	result := make(Paths, 0, len(modules))
	mctx, isMctx := ctx.(ModuleContext)
	for _, module := range modules {
		var mf Path
		if isMctx && EqualModules(mctx.Module(), module) {
			mf = mctx.LicenseMetadataFile()
		} else {
			mf = OtherModuleProviderOrDefault(ctx, module, InstallFilesProvider).LicenseMetadataFile
		}
		if mf != nil {
			result = append(result, mf)
		}
	}
	return result
}

// All the information we need from a particular module to build its notice file entry.
// Can be passed through providers, unlike the module itself.
type NoticeModuleInfo struct {
	Name                string
	OutputDirs          []string
	LicenseMetadataFile Path
}

type NoticeModuleInfos []NoticeModuleInfo

func (i *NoticeModuleInfos) OutputDirs() []string {
	var result []string
	for _, info := range *i {
		result = append(result, info.OutputDirs...)
	}
	return result
}

func (i *NoticeModuleInfos) LicenseMetadataFiles() Paths {
	result := make(Paths, 0, len(*i))
	for _, info := range *i {
		if info.LicenseMetadataFile != nil {
			result = append(result, info.LicenseMetadataFile)
		}
	}
	return result
}

func GetNoticeModuleInfo(ctx BuilderAndOtherModuleProviderContext, m ModuleProxy) NoticeModuleInfo {
	var licenseMetadataFile Path
	licenseMetadataFiles := modulesLicenseMetadata(ctx, m)
	if len(licenseMetadataFiles) > 1 {
		panic("Didn't expect more than 1 licence metadata file")
	} else if len(licenseMetadataFiles) > 0 {
		licenseMetadataFile = licenseMetadataFiles[0]
	}
	return NoticeModuleInfo{
		Name:                m.Name(),
		OutputDirs:          modulesOutputDirs(ctx, m),
		LicenseMetadataFile: licenseMetadataFile,
	}
}

func GetNoticeModuleInfos(ctx BuilderAndOtherModuleProviderContext, modules []ModuleProxy) NoticeModuleInfos {
	result := make(NoticeModuleInfos, 0, len(modules))
	for _, m := range modules {
		result = append(result, GetNoticeModuleInfo(ctx, m))
	}
	return result
}

// buildNoticeOutputFromLicenseMetadata writes out a notice file.
func buildNoticeOutputFromLicenseMetadata(
	ctx BuilderAndOtherModuleProviderContext, tool, ruleName string, outputFile WritablePath,
	libraryName string, extraArgs BuildNoticeFromLicenseDataArgs, modules NoticeModuleInfos) {
	depsFile := outputFile.ReplaceExtension(ctx, strings.TrimPrefix(outputFile.Ext()+".d", "."))
	if len(modules) == 0 {
		panic(fmt.Errorf("%s %q needs a module to generate the notice for", ruleName, libraryName))
	}
	if libraryName == "" {
		libraryName = modules[0].Name
	}
	rule := NewRuleBuilder(pctx, ctx)

	// Arguments that will go into the response file.
	var rspArgs []string

	for _, sp := range extraArgs.StripPrefix {
		rspArgs = append(rspArgs, "--strip_prefix", sp)
	}
	for _, sp := range modules.OutputDirs() {
		rspArgs = append(rspArgs, "--strip_prefix", sp)
	}

	// The difference between nil and empty slice matters here, to allow for empty filter sets
	if extraArgs.Filter != nil {
		rspArgs = append(rspArgs, "--filter")
		for _, f := range extraArgs.Filter {
			rspArgs = append(rspArgs, "--filter_to", f)
		}
	}
	for _, r := range extraArgs.Replace.Args() {
		rspArgs = append(rspArgs, "--replace", r)
	}

	// Add input files.
	for _, f := range modules.LicenseMetadataFiles() {
		rspArgs = append(rspArgs, f.String())
	}

	rspFile := outputFile.ReplaceExtension(ctx, strings.TrimPrefix(outputFile.Ext()+".rsp", "."))
	WriteFileRule(ctx, rspFile, strings.Join(rspArgs, "\n"))

	cmd := rule.Command().
		BuiltTool(tool).
		FlagWithOutput("-o ", outputFile).
		FlagWithDepFile("-d ", depsFile)

	if libraryName != "" {
		cmd.FlagWithArg("--product ", proptools.ShellEscapeIncludingSpaces(libraryName))
	}
	if extraArgs.Title != "" {
		cmd.FlagWithArg("--title ", proptools.ShellEscapeIncludingSpaces(extraArgs.Title))
	}

	cmd.Flag("@" + rspFile.String())

	cmd.Implicits(modules.LicenseMetadataFiles())
	cmd.Implicit(rspFile)

	rule.Build(ruleName, "container notice file")
}

type NoticeReplace struct {
	From string
	To   string
}

type NoticeReplaces []NoticeReplace

func (r *NoticeReplaces) Args() []string {
	var result []string
	for _, i := range *r {
		result = append(result, i.From+":::"+i.To)
	}
	return result
}

// Additional optional arguments that can be passed to notice building functions
type BuildNoticeFromLicenseDataArgs struct {
	Title       string
	StripPrefix []string
	Filter      []string
	Replace     NoticeReplaces
}

// BuildNoticeTextOutputFromLicenseMetadata writes out a notice text file based
// on the license metadata files for the input `modules` defaulting to the
// current context module if none given.
func BuildNoticeTextOutputFromLicenseMetadata(
	ctx BuilderAndOtherModuleProviderContext, outputFile WritablePath, ruleName, libraryName string,
	extraArgs BuildNoticeFromLicenseDataArgs, modules ...ModuleProxy) {
	buildNoticeOutputFromLicenseMetadata(ctx, "textnotice", "text_notice_"+ruleName,
		outputFile, libraryName, extraArgs, GetNoticeModuleInfos(ctx, modules))
}

// BuildNoticeHtmlOutputFromLicenseMetadata writes out a notice text file based
// on the license metadata files for the input `modules` defaulting to the
// current context module if none given.
func BuildNoticeHtmlOutputFromLicenseMetadata(
	ctx BuilderAndOtherModuleProviderContext, outputFile WritablePath, ruleName, libraryName string,
	extraArgs BuildNoticeFromLicenseDataArgs, modules ...ModuleProxy) {
	buildNoticeOutputFromLicenseMetadata(ctx, "htmlnotice", "html_notice_"+ruleName,
		outputFile, libraryName, extraArgs, GetNoticeModuleInfos(ctx, modules))
}

// BuildNoticeXmlOutputFromLicenseMetadata writes out a notice text file based
// on the license metadata files for the input `modules` defaulting to the
// current context module if none given.
func BuildNoticeXmlOutputFromLicenseMetadata(
	ctx BuilderAndOtherModuleProviderContext, outputFile WritablePath, ruleName, libraryName string,
	extraArgs BuildNoticeFromLicenseDataArgs, modules ...ModuleProxy) {
	buildNoticeOutputFromLicenseMetadata(ctx, "xmlnotice", "xml_notice_"+ruleName,
		outputFile, libraryName, extraArgs, GetNoticeModuleInfos(ctx, modules))
}

// BuildNoticeTextOutputFromNoticeModuleInfos is the same as
// BuildNoticeTextOutputFromLicenseMetadata, but it accepts NoticeModuleInfos instead of straight
// ModuleProxies. This is useful for when you don't have direct access to the modules you
// want to generate a notice file for, for example a singleton accessing the transitive dependencies
// of a particular module.
func BuildNoticeTextOutputFromNoticeModuleInfos(
	ctx BuilderAndOtherModuleProviderContext, outputFile WritablePath, ruleName, libraryName string,
	extraArgs BuildNoticeFromLicenseDataArgs, moduleInfos NoticeModuleInfos) {
	buildNoticeOutputFromLicenseMetadata(ctx, "textnotice", "text_notice_"+ruleName,
		outputFile, libraryName, extraArgs, moduleInfos)
}
