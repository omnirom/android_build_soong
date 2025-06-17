// Copyright 2017 Google Inc. All rights reserved.
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
	"reflect"
	"strings"
	"testing"
)

func validateConfigAnnotations(configurable jsonConfigurable) (err error) {
	reflectType := reflect.TypeOf(configurable)
	reflectType = reflectType.Elem()
	for i := 0; i < reflectType.NumField(); i++ {
		field := reflectType.Field(i)
		jsonTag := field.Tag.Get("json")
		// Check for mistakes in the json tag
		if jsonTag != "" && !strings.HasPrefix(jsonTag, ",") {
			if !strings.Contains(jsonTag, ",") {
				// Probably an accidental rename, most likely "omitempty" instead of ",omitempty"
				return fmt.Errorf("Field %s.%s has tag %s which specifies to change its json field name to %q.\n"+
					"Did you mean to use an annotation of %q?\n"+
					"(Alternatively, to change the json name of the field, rename the field in source instead.)",
					reflectType.Name(), field.Name, field.Tag, jsonTag, ","+jsonTag)
			} else {
				// Although this rename was probably intentional,
				// a json annotation is still more confusing than renaming the source variable
				requestedName := strings.Split(jsonTag, ",")[0]
				return fmt.Errorf("Field %s.%s has tag %s which specifies to change its json field name to %q.\n"+
					"To change the json name of the field, rename the field in source instead.",
					reflectType.Name(), field.Name, field.Tag, requestedName)

			}
		}
	}
	return nil
}

type configType struct {
	PopulateMe *bool `json:"omitempty"`
}

func (c *configType) SetDefaultConfig() {
}

// tests that ValidateConfigAnnotation works
func TestValidateConfigAnnotations(t *testing.T) {
	config := configType{}
	err := validateConfigAnnotations(&config)
	expectedError := `Field configType.PopulateMe has tag json:"omitempty" which specifies to change its json field name to "omitempty".
Did you mean to use an annotation of ",omitempty"?
(Alternatively, to change the json name of the field, rename the field in source instead.)`
	if err.Error() != expectedError {
		t.Errorf("Incorrect error; expected:\n"+
			"%s\n"+
			"got:\n"+
			"%s",
			expectedError, err.Error())
	}
}

// run validateConfigAnnotations against each type that might have json annotations
func TestProductConfigAnnotations(t *testing.T) {
	err := validateConfigAnnotations(&ProductVariables{})
	if err != nil {
		t.Error(err.Error())
	}
}

func TestMissingVendorConfig(t *testing.T) {
	c := &config{}
	if c.VendorConfig("test").Bool("not_set") {
		t.Errorf("Expected false")
	}
}

func verifyProductVariableMarshaling(t *testing.T, v ProductVariables) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.variables")
	err := saveToConfigFile(&v, path)
	if err != nil {
		t.Errorf("Couldn't save default product config: %q", err)
	}

	var v2 ProductVariables
	err = loadFromConfigFile(&v2, path)
	if err != nil {
		t.Errorf("Couldn't load default product config: %q", err)
	}
}
func TestDefaultProductVariableMarshaling(t *testing.T) {
	v := ProductVariables{}
	v.SetDefaultConfig()
	verifyProductVariableMarshaling(t, v)
}

func TestBootJarsMarshaling(t *testing.T) {
	v := ProductVariables{}
	v.SetDefaultConfig()
	v.BootJars = ConfiguredJarList{
		apexes: []string{"apex"},
		jars:   []string{"jar"},
	}

	verifyProductVariableMarshaling(t, v)
}

func assertStringEquals(t *testing.T, expected, actual string) {
	if actual != expected {
		t.Errorf("expected %q found %q", expected, actual)
	}
}

func TestReleaseAconfigExtraReleaseConfigs(t *testing.T) {
	testCases := []struct {
		name     string
		flag     string
		expected []string
	}{
		{
			name:     "empty",
			flag:     "",
			expected: []string{},
		},
		{
			name:     "specified",
			flag:     "bar foo",
			expected: []string{"bar", "foo"},
		},
		{
			name:     "duplicates",
			flag:     "foo bar foo",
			expected: []string{"foo", "bar"},
		},
	}

	for _, tc := range testCases {
		fixture := GroupFixturePreparers(
			PrepareForTestWithBuildFlag("RELEASE_ACONFIG_EXTRA_RELEASE_CONFIGS", tc.flag),
		)
		actual := fixture.RunTest(t).Config.ReleaseAconfigExtraReleaseConfigs()
		AssertArrayString(t, tc.name, tc.expected, actual)
	}
}

func TestConfiguredJarList(t *testing.T) {
	list1 := CreateTestConfiguredJarList([]string{"apex1:jarA"})

	t.Run("create", func(t *testing.T) {
		assertStringEquals(t, "apex1:jarA", list1.String())
	})

	t.Run("create invalid - missing apex", func(t *testing.T) {
		defer func() {
			err := recover().(error)
			assertStringEquals(t, "malformed (apex, jar) pair: 'jarA', expected format: <apex>:<jar>", err.Error())
		}()
		CreateTestConfiguredJarList([]string{"jarA"})
	})

	t.Run("create invalid - empty apex", func(t *testing.T) {
		defer func() {
			err := recover().(error)
			assertStringEquals(t, "invalid apex '' in <apex>:<jar> pair ':jarA', expected format: <apex>:<jar>", err.Error())
		}()
		CreateTestConfiguredJarList([]string{":jarA"})
	})

	list2 := list1.Append("apex2", "jarB")
	t.Run("append", func(t *testing.T) {
		assertStringEquals(t, "apex1:jarA,apex2:jarB", list2.String())
	})

	t.Run("append does not modify", func(t *testing.T) {
		assertStringEquals(t, "apex1:jarA", list1.String())
	})

	// Make sure that two lists created by appending to the same list do not share storage.
	list3 := list1.Append("apex3", "jarC")
	t.Run("append does not share", func(t *testing.T) {
		assertStringEquals(t, "apex1:jarA,apex2:jarB", list2.String())
		assertStringEquals(t, "apex1:jarA,apex3:jarC", list3.String())
	})

	list4 := list3.RemoveList(list1)
	t.Run("remove", func(t *testing.T) {
		assertStringEquals(t, "apex3:jarC", list4.String())
	})

	t.Run("remove does not modify", func(t *testing.T) {
		assertStringEquals(t, "apex1:jarA,apex3:jarC", list3.String())
	})

	// Make sure that two lists created by removing from the same list do not share storage.
	list5 := list3.RemoveList(CreateTestConfiguredJarList([]string{"apex3:jarC"}))
	t.Run("remove", func(t *testing.T) {
		assertStringEquals(t, "apex3:jarC", list4.String())
		assertStringEquals(t, "apex1:jarA", list5.String())
	})
}

func (p partialCompileFlags) updateUseD8(value bool) partialCompileFlags {
	p.Use_d8 = value
	return p
}

func (p partialCompileFlags) updateDisableApiLint(value bool) partialCompileFlags {
	p.Disable_api_lint = value
	return p
}

func (p partialCompileFlags) updateDisableStubValidation(value bool) partialCompileFlags {
	p.Disable_stub_validation = value
	return p
}

func TestPartialCompile(t *testing.T) {
	mockConfig := func(value string) *config {
		c := &config{
			env: map[string]string{
				"SOONG_PARTIAL_COMPILE": value,
			},
		}
		return c
	}
	tests := []struct {
		value      string
		isEngBuild bool
		expected   partialCompileFlags
	}{
		{"", true, defaultPartialCompileFlags},
		{"false", true, partialCompileFlags{}},
		{"true", true, enabledPartialCompileFlags},
		{"true", false, partialCompileFlags{}},
		{"all", true, partialCompileFlags{}.updateUseD8(true).updateDisableApiLint(true).updateDisableStubValidation(true)},

		// This verifies both use_d8 and the processing order.
		{"true,use_d8", true, enabledPartialCompileFlags.updateUseD8(true)},
		{"true,-use_d8", true, enabledPartialCompileFlags.updateUseD8(false)},
		{"use_d8,false", true, partialCompileFlags{}},
		{"false,+use_d8", true, partialCompileFlags{}.updateUseD8(true)},

		// disable_api_lint can be specified with any of 3 options.
		{"false,-api_lint", true, partialCompileFlags{}.updateDisableApiLint(true)},
		{"false,-enable_api_lint", true, partialCompileFlags{}.updateDisableApiLint(true)},
		{"false,+disable_api_lint", true, partialCompileFlags{}.updateDisableApiLint(true)},
		{"false,+api_lint", true, partialCompileFlags{}.updateDisableApiLint(false)},
		{"false,+enable_api_lint", true, partialCompileFlags{}.updateDisableApiLint(false)},
		{"false,-disable_api_lint", true, partialCompileFlags{}.updateDisableApiLint(false)},

		// disable_stub_validation can be specified with any of 3 options.
		{"false,-stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(true)},
		{"false,-enable_stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(true)},
		{"false,+disable_stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(true)},
		{"false,+stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(false)},
		{"false,+enable_stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(false)},
		{"false,-disable_stub_validation", true, partialCompileFlags{}.updateDisableStubValidation(false)},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			config := mockConfig(test.value)
			flags, _ := config.parsePartialCompileFlags(test.isEngBuild)
			if flags != test.expected {
				t.Errorf("expected %v found %v", test.expected, flags)
			}
		})
	}
}

type configTestProperties struct {
	Use_generic_config *bool
}

type configTestModule struct {
	ModuleBase
	properties configTestProperties
}

func (d *configTestModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	deviceName := ctx.Config().DeviceName()
	if ctx.ModuleName() == "foo" {
		if ctx.Module().UseGenericConfig() {
			ctx.PropertyErrorf("use_generic_config", "must not be set for this test")
		}
	} else if ctx.ModuleName() == "bar" {
		if !ctx.Module().UseGenericConfig() {
			ctx.ModuleErrorf("\"use_generic_config: true\" must be set for this test")
		}
	}

	if ctx.Module().UseGenericConfig() {
		if deviceName != "generic" {
			ctx.ModuleErrorf("Device name for this module must be \"generic\" but %q\n", deviceName)
		}
	} else {
		if deviceName == "generic" {
			ctx.ModuleErrorf("Device name for this module must not be \"generic\"\n")
		}
	}
}

func configTestModuleFactory() Module {
	module := &configTestModule{}
	module.AddProperties(&module.properties)
	InitAndroidModule(module)
	return module
}

var prepareForConfigTest = GroupFixturePreparers(
	FixtureRegisterWithContext(func(ctx RegistrationContext) {
		ctx.RegisterModuleType("test", configTestModuleFactory)
	}),
)

func TestGenericConfig(t *testing.T) {
	bp := `
		test {
			name: "foo",
		}

		test {
			name: "bar",
			use_generic_config: true,
		}
	`

	result := GroupFixturePreparers(
		prepareForConfigTest,
		FixtureWithRootAndroidBp(bp),
	).RunTest(t)

	foo := result.Module("foo", "").(*configTestModule)
	bar := result.Module("bar", "").(*configTestModule)

	AssertBoolEquals(t, "Do not use generic config", false, foo.UseGenericConfig())
	AssertBoolEquals(t, "Use generic config", true, bar.UseGenericConfig())
}
