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
	"reflect"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
	"github.com/google/blueprint/syncmap"
)

// This file implements support for automatically adding dependencies on any module referenced
// with the ":module" module reference syntax in a property that is annotated with `android:"path"`.
// The dependency is used by android.PathForModuleSrc to convert the module reference into the path
// to the output file of the referenced module.

func registerPathDepsMutator(ctx RegisterMutatorsContext) {
	ctx.BottomUp("pathdeps", pathDepsMutator)
}

// The pathDepsMutator automatically adds dependencies on any module that is listed with the
// ":module" module reference syntax in a property that is tagged with `android:"path"`.
func pathDepsMutator(ctx BottomUpMutatorContext) {
	if _, ok := ctx.Module().(DefaultsModule); ok {
		// Defaults modules shouldn't have dependencies added for path properties, they have already been
		// squashed into the real modules.
		return
	}
	if !ctx.Module().Enabled(ctx) {
		return
	}
	props := ctx.Module().base().GetProperties()
	addPathDepsForProps(ctx, props)
}

func addPathDepsForProps(ctx BottomUpMutatorContext, props []interface{}) {
	// Iterate through each property struct of the module extracting the contents of all properties
	// tagged with `android:"path"` or one of the variant-specifying tags.
	var pathProperties []string
	var pathDeviceFirstProperties []string
	var pathDeviceFirstPrefer32Properties []string
	var pathDeviceCommonProperties []string
	var pathCommonOsProperties []string
	var pathHostCommonProperties []string
	var pathHostFirstProperties []string
	var pathHostSecondProperties []string
	for _, ps := range props {
		pathPropertyIndexes := pathPropertyIndexesForPropertyStruct(ps)
		pathProperties = append(pathProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.path)...)
		pathDeviceFirstProperties = append(pathDeviceFirstProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathDeviceFirst)...)
		pathDeviceFirstPrefer32Properties = append(pathDeviceFirstPrefer32Properties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathDeviceFirstPrefer32)...)
		pathDeviceCommonProperties = append(pathDeviceCommonProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathDeviceCommon)...)
		pathCommonOsProperties = append(pathCommonOsProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathCommonOs)...)
		pathHostCommonProperties = append(pathHostCommonProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathHostCommon)...)
		pathHostFirstProperties = append(pathHostFirstProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathHostFirst)...)
		pathHostSecondProperties = append(pathHostSecondProperties, indexedPropertiesForPropertyStruct(ctx, ps, pathPropertyIndexes.pathHostSecond)...)
	}

	// Remove duplicates to avoid multiple dependencies.
	pathProperties = FirstUniqueStrings(pathProperties)
	pathDeviceFirstProperties = FirstUniqueStrings(pathDeviceFirstProperties)
	pathDeviceFirstPrefer32Properties = FirstUniqueStrings(pathDeviceFirstPrefer32Properties)
	pathDeviceCommonProperties = FirstUniqueStrings(pathDeviceCommonProperties)
	pathCommonOsProperties = FirstUniqueStrings(pathCommonOsProperties)
	pathHostCommonProperties = FirstUniqueStrings(pathHostCommonProperties)
	pathHostFirstProperties = FirstUniqueStrings(pathHostFirstProperties)
	pathHostSecondProperties = FirstUniqueStrings(pathHostSecondProperties)

	// Add dependencies to anything that is a module reference.
	for _, s := range pathProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddDependency(ctx.Module(), sourceOrOutputDepTag(m, t), m)
		}
	}
	// For properties tagged "path_device_first", use the first arch device variant when adding
	// dependencies. This allows host modules to have some properties that add dependencies on
	// device modules.
	for _, s := range pathDeviceFirstProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddVariationDependencies(ctx.Config().AndroidFirstDeviceTarget.Variations(), sourceOrOutputDepTag(m, t), m)
		}
	}
	// properties tagged path_device_first_prefer32 get the first 32 bit target if one is available,
	// otherwise they use the first 64 bit target
	if len(pathDeviceFirstPrefer32Properties) > 0 {
		var targets []Target
		if ctx.Config().IgnorePrefer32OnDevice() {
			targets, _ = decodeMultilibTargets("first", ctx.Config().Targets[Android], false)
		} else {
			targets, _ = decodeMultilibTargets("first_prefer32", ctx.Config().Targets[Android], false)
		}
		if len(targets) == 0 {
			ctx.ModuleErrorf("Could not find a first_prefer32 target")
		} else {
			for _, s := range pathDeviceFirstPrefer32Properties {
				if m, t := SrcIsModuleWithTag(s); m != "" {
					ctx.AddVariationDependencies(targets[0].Variations(), sourceOrOutputDepTag(m, t), m)
				}
			}
		}
	}
	// properties tagged "path_device_common" get the device common variant
	for _, s := range pathDeviceCommonProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddVariationDependencies(ctx.Config().AndroidCommonTarget.Variations(), sourceOrOutputDepTag(m, t), m)
		}
	}
	// properties tagged "path_host_common" get the host common variant
	for _, s := range pathHostCommonProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddVariationDependencies(ctx.Config().BuildOSCommonTarget.Variations(), sourceOrOutputDepTag(m, t), m)
		}
	}
	// properties tagged "path_host_first" get the host os variant
	for _, s := range pathHostFirstProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddVariationDependencies(ctx.Config().BuildOSTarget.Variations(), sourceOrOutputDepTag(m, t), m)
		}
	}
	// properties tagged "path_host_second" get the host 2nd os variant
	if len(pathHostSecondProperties) > 0 {
		var targets []Target
		targets, _ = decodeMultilibTargets("32", ctx.Config().Targets[ctx.Config().BuildOS], false)
		if len(targets) == 0 {
			ctx.ModuleErrorf("Could not find a 32 bit host target")
		} else {
			for _, s := range pathHostSecondProperties {
				if m, t := SrcIsModuleWithTag(s); m != "" {
					ctx.AddVariationDependencies(targets[0].Variations(), sourceOrOutputDepTag(m, t), m)
				}
			}
		}
	}

	// properties tagged "path_common_os" get the CommonOs variant
	for _, s := range pathCommonOsProperties {
		if m, t := SrcIsModuleWithTag(s); m != "" {
			ctx.AddVariationDependencies([]blueprint.Variation{
				{Mutator: "os", Variation: "common_os"},
				{Mutator: "arch", Variation: ""},
			}, sourceOrOutputDepTag(m, t), m)
		}
	}
}

// indexedPropertiesForPropertyStruct uses the indexes of properties extract all their values from a
// property struct, returning them as a single slice of strings.
func indexedPropertiesForPropertyStruct(ctx BottomUpMutatorContext, ps interface{}, pathPropertyIndexes [][]int) []string {
	v := reflect.ValueOf(ps)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		panic(fmt.Errorf("type %s is not a pointer to a struct", v.Type()))
	}

	// If the property struct is a nil pointer it can't have any paths set in it.
	if v.IsNil() {
		return nil
	}

	// v is now the reflect.Value for the concrete property struct.
	v = v.Elem()

	ret := make([]string, 0, len(pathPropertyIndexes))

	for _, i := range pathPropertyIndexes {
		var values []reflect.Value
		fieldsByIndex(v, i, &values)
		for _, sv := range values {
			if !sv.IsValid() {
				// Skip properties inside a nil pointer.
				continue
			}

			// If the field is a non-nil pointer step into it.
			if sv.Kind() == reflect.Ptr {
				if sv.IsNil() {
					continue
				}
				sv = sv.Elem()
			}

			// Collect paths from all strings and slices of strings.
			switch sv.Kind() {
			case reflect.String:
				ret = append(ret, sv.String())
			case reflect.Slice:
				ret = append(ret, sv.Interface().([]string)...)
			case reflect.Struct:
				intf := sv.Interface()
				if configurable, ok := intf.(proptools.Configurable[string]); ok {
					ret = append(ret, configurable.GetOrDefault(ctx, ""))
				} else if configurable, ok := intf.(proptools.Configurable[[]string]); ok {
					ret = append(ret, configurable.GetOrDefault(ctx, nil)...)
				} else {
					panic(fmt.Errorf(`field %s in type %s has tag android:"path" but is not a string or slice of strings, it is a %s`,
						v.Type().FieldByIndex(i).Name, v.Type(), sv.Type()))
				}
			default:
				panic(fmt.Errorf(`field %s in type %s has tag android:"path" but is not a string or slice of strings, it is a %s`,
					v.Type().FieldByIndex(i).Name, v.Type(), sv.Type()))
			}
		}
	}

	return ret
}

// fieldsByIndex is similar to reflect.Value.FieldByIndex, but is more robust: it doesn't track
// nil pointers and it returns multiple values when there's slice of struct.
func fieldsByIndex(v reflect.Value, index []int, values *[]reflect.Value) {
	// leaf case
	if len(index) == 1 {
		if isSliceOfStruct(v) {
			for i := 0; i < v.Len(); i++ {
				*values = append(*values, v.Index(i).Field(index[0]))
			}
		} else {
			// Dereference it if it's a pointer.
			if v.Kind() == reflect.Ptr {
				if v.IsNil() {
					return
				}
				v = v.Elem()
			}
			*values = append(*values, v.Field(index[0]))
		}
		return
	}

	// recursion
	if v.Kind() == reflect.Ptr {
		// don't track nil pointer
		if v.IsNil() {
			return
		}
		v = v.Elem()
	} else if isSliceOfStruct(v) {
		// do the recursion for all elements
		for i := 0; i < v.Len(); i++ {
			fieldsByIndex(v.Index(i).Field(index[0]), index[1:], values)
		}
		return
	}
	fieldsByIndex(v.Field(index[0]), index[1:], values)
	return
}

func isSliceOfStruct(v reflect.Value) bool {
	return v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Struct
}

type pathPropertyIndexes struct {
	path                    [][]int
	pathDeviceFirst         [][]int
	pathDeviceFirstPrefer32 [][]int
	pathDeviceCommon        [][]int
	pathCommonOs            [][]int
	pathHostCommon          [][]int
	pathHostFirst           [][]int
	pathHostSecond          [][]int
}

var pathPropertyIndexesCache syncmap.SyncMap[reflect.Type, *pathPropertyIndexes]

// pathPropertyIndexesForPropertyStruct returns a list of all of the indexes of properties in
// property struct type that are tagged as path properties with `android:"path"` or similar.
// Each index is a []int suitable for passing to reflect.Value.FieldByIndex.  The value is cached
// in a global cache by type.
func pathPropertyIndexesForPropertyStruct(ps interface{}) *pathPropertyIndexes {
	key := reflect.TypeOf(ps)

	if indexes, loaded := pathPropertyIndexesCache.Load(key); loaded {
		return indexes
	}

	indexes := &pathPropertyIndexes{
		path:                    proptools.PropertyIndexesWithTag(ps, "android", "path"),
		pathDeviceFirst:         proptools.PropertyIndexesWithTag(ps, "android", "path_device_first"),
		pathDeviceFirstPrefer32: proptools.PropertyIndexesWithTag(ps, "android", "path_device_first_prefer32"),
		pathDeviceCommon:        proptools.PropertyIndexesWithTag(ps, "android", "path_device_common"),
		pathCommonOs:            proptools.PropertyIndexesWithTag(ps, "android", "path_common_os"),
		pathHostCommon:          proptools.PropertyIndexesWithTag(ps, "android", "path_host_common"),
		pathHostFirst:           proptools.PropertyIndexesWithTag(ps, "android", "path_host_first"),
		pathHostSecond:          proptools.PropertyIndexesWithTag(ps, "android", "path_host_second"),
	}
	indexes, _ = pathPropertyIndexesCache.LoadOrStore(key, indexes)
	return indexes
}
