// Copyright (C) 2025 The Android Open Source Project
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

package filesystem

import (
	"path/filepath"
	"strconv"
	"strings"

	"android/soong/android"
)

type recoveryBackgroundPictures struct {
	android.ModuleBase
	properties recoveryBackgroundPicturesProperties
}

type recoveryBackgroundPicturesProperties struct {
	Image_width *int64
	Fonts       []string `android:"path"`
	Resources   []string `android:"path"`
}

func RecoveryBackgroundPicturesFactory() android.Module {
	module := &recoveryBackgroundPictures{}
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	module.AddProperties(&module.properties)
	return module
}

func (f *recoveryBackgroundPictures) InstallInRoot() bool {
	return true
}

var (
	recoveryBackgroundTextList = []string{
		"recovery_installing",
		"recovery_installing_security",
		"recovery_erasing",
		"recovery_error",
		"recovery_no_command",
	}
	recoveryWipeDataTextList = []string{
		"recovery_cancel_wipe_data",
		"recovery_factory_data_reset",
		"recovery_try_again",
		"recovery_wipe_data_menu_header",
		"recovery_wipe_data_confirmation",
	}
)

func (f *recoveryBackgroundPictures) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	rule := android.NewRuleBuilder(pctx, ctx).
		Sbox(android.PathForModuleOut(ctx, "images"),
			android.PathForModuleOut(ctx, "gen.sbox.textproto")).
		SandboxInputs()

	fontDir, resDir := f.assembleFontsAndRes(ctx, rule)
	var images android.Paths
	for _, name := range recoveryBackgroundTextList {
		image := f.addImageGenRule(ctx, rule, fontDir, resDir, name, true)
		images = append(images, image)
	}
	for _, name := range recoveryWipeDataTextList {
		image := f.addImageGenRule(ctx, rule, fontDir, resDir, name, false)
		images = append(images, image)
	}

	rule.Build("recovery_background_images", "recovery_background_images")

	for _, image := range images {
		ctx.InstallFile(android.PathForModuleInstall(ctx, "res", "images"), image.Base(), image)
	}
}

// Copies the fonts and resources to a well known location inside the sandbox.
// Returns the assembled font and res dir inside the sandbox.
func (f *recoveryBackgroundPictures) assembleFontsAndRes(ctx android.ModuleContext, rule *android.RuleBuilder) (android.Path, android.Path) {
	fontDir := android.PathForModuleOut(ctx, "images", "fonts")
	fonts := android.PathsForModuleSrc(ctx, f.properties.Fonts)

	// Assemble the fonts at root.
	cmd := rule.Command()
	cmd.Textf("mkdir -p %s", cmd.PathForInput(fontDir)).
		Textf("&& cp -t %s", cmd.PathForInput(fontDir))
	for _, f := range fonts {
		cmd.Input(f)
	}

	// Assemeble the res by preserving rel paths.
	resDir := android.PathForModuleOut(ctx, "images", "res")
	cmd.Textf("&& mkdir -p %s", cmd.PathForInput(resDir))
	for _, res := range android.PathsForModuleSrc(ctx, f.properties.Resources) {
		relDir := filepath.Dir(res.Rel())
		cmd.Textf("&& mkdir -p %s/%s", cmd.PathForInput(resDir), relDir).
			Textf("&& cp %s %s/%s", cmd.PathForInput(res), cmd.PathForInput(resDir), relDir).
			Implicit(res)
	}

	return fontDir, resDir
}

// Adds rules to generate recovery images using RecoveryImageGenerator.jar
// Returns the paths of the generated recovery images.
func (f *recoveryBackgroundPictures) addImageGenRule(ctx android.ModuleContext,
	rule *android.RuleBuilder,
	fontDir android.Path,
	resDir android.Path,
	textName string,
	centerAlign bool) android.WritablePath {
	out := android.PathForModuleOut(ctx, "images", strings.TrimPrefix(textName, "recovery_")+"_text.png")
	generator := ctx.Config().HostJavaToolPath(ctx, "RecoveryImageGenerator.jar")
	cmd := rule.Command()
	cmd.Textf("java -jar").Input(generator).
		FlagWithArg("--image_width ", strconv.FormatInt(*(f.properties.Image_width), 10)).
		FlagWithArg("--text_name ", textName).
		FlagWithArg("--font_dir ", cmd.PathForInput(fontDir)).
		FlagWithArg("--resource_dir ", cmd.PathForInput(resDir)).
		Implicits(android.PathsForModuleSrc(ctx, f.properties.Resources))
	if centerAlign {
		cmd.Flag("--center_alignment ")
	}
	cmd.FlagWithOutput("--output_file ", out)

	rule.Command().BuiltTool("zopflipng").
		ImplicitTool(ctx.Config().HostCcSharedLibPath(ctx, "libc++")).
		Textf(" -y --iterations=1 --filters=0 %s %s > /dev/null", cmd.PathForInput(out), cmd.PathForInput(out))

	return out
}
