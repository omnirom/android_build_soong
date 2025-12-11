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

package android

func init() {
	ctx := InitRegistrationContext
	ctx.RegisterModuleType("system_dev_certificate", systemDevCertificateFactory)
}

type systemDevCertificateModule struct {
	ModuleBase
}

func (s *systemDevCertificateModule) UseGenericConfig() bool {
	return false
}

func (s *systemDevCertificateModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	if ctx.ModuleName() != "system_dev_certificate" || ctx.ModuleDir() != "build/soong" {
		ctx.ModuleErrorf("There can only be one system_dev_certificate module in build/soong")
		return
	}

	pem, pk8 := ctx.Config().DefaultAppCertificate(ctx)
	ctx.SetOutputFiles(Paths{pem}, "pem")
	ctx.SetOutputFiles(Paths{pk8}, "pk8")
}

func systemDevCertificateFactory() Module {
	module := &systemDevCertificateModule{}
	InitAndroidModule(module)
	return module
}
