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

type ImageInterfaceContext interface {
	ArchModuleContext

	Module() Module

	ModuleErrorf(fmt string, args ...interface{})
	PropertyErrorf(property, fmt string, args ...interface{})

	DeviceSpecific() bool
	SocSpecific() bool
	ProductSpecific() bool
	SystemExtSpecific() bool
	Platform() bool

	Config() Config
}

// ImageInterface is implemented by modules that need to be split by the imageTransitionMutator.
type ImageInterface interface {
	// ImageMutatorBegin is called before any other method in the ImageInterface.
	ImageMutatorBegin(ctx ImageInterfaceContext)

	// If ImageMutatorSupported returns false then no image variants will be created.
	ImageMutatorSupported() bool

	// VendorVariantNeeded should return true if the module needs a vendor variant (installed on the vendor image).
	VendorVariantNeeded(ctx ImageInterfaceContext) bool

	// ProductVariantNeeded should return true if the module needs a product variant (installed on the product image).
	ProductVariantNeeded(ctx ImageInterfaceContext) bool

	// CoreVariantNeeded should return true if the module needs a core variant (installed on the system image).
	CoreVariantNeeded(ctx ImageInterfaceContext) bool

	// RamdiskVariantNeeded should return true if the module needs a ramdisk variant (installed on the
	// ramdisk partition).
	RamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// VendorRamdiskVariantNeeded should return true if the module needs a vendor ramdisk variant (installed on the
	// vendor ramdisk partition).
	VendorRamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// DebugRamdiskVariantNeeded should return true if the module needs a debug ramdisk variant (installed on the
	// debug ramdisk partition: $(PRODUCT_OUT)/debug_ramdisk).
	DebugRamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// RecoveryVariantNeeded should return true if the module needs a recovery variant (installed on the
	// recovery partition).
	RecoveryVariantNeeded(ctx ImageInterfaceContext) bool

	// ExtraImageVariations should return a list of the additional variations needed for the module.  After the
	// variants are created the SetImageVariation method will be called on each newly created variant with the
	// its variation.
	ExtraImageVariations(ctx ImageInterfaceContext) []string

	// SetImageVariation is called for each newly created image variant. The receiver is the original
	// module, "variation" is the name of the newly created variant. "variation" is set on the receiver.
	SetImageVariation(ctx ImageInterfaceContext, variation string)
}

const (
	// VendorVariation is the variant name used for /vendor code that does not
	// compile against the VNDK.
	VendorVariation string = "vendor"

	// ProductVariation is the variant name used for /product code that does not
	// compile against the VNDK.
	ProductVariation string = "product"

	// CoreVariation is the variant used for framework-private libraries, or
	// SDK libraries. (which framework-private libraries can use), which
	// will be installed to the system image.
	CoreVariation string = ""

	// RecoveryVariation means a module to be installed to recovery image.
	RecoveryVariation string = "recovery"

	// RamdiskVariation means a module to be installed to ramdisk image.
	RamdiskVariation string = "ramdisk"

	// VendorRamdiskVariation means a module to be installed to vendor ramdisk image.
	VendorRamdiskVariation string = "vendor_ramdisk"

	// DebugRamdiskVariation means a module to be installed to debug ramdisk image.
	DebugRamdiskVariation string = "debug_ramdisk"
)

type imageInterfaceContextAdapter struct {
	IncomingTransitionContext
	kind moduleKind
}

var _ ImageInterfaceContext = (*imageInterfaceContextAdapter)(nil)

func (e *imageInterfaceContextAdapter) Platform() bool {
	return e.kind == platformModule
}

func (e *imageInterfaceContextAdapter) DeviceSpecific() bool {
	return e.kind == deviceSpecificModule
}

func (e *imageInterfaceContextAdapter) SocSpecific() bool {
	return e.kind == socSpecificModule
}

func (e *imageInterfaceContextAdapter) ProductSpecific() bool {
	return e.kind == productSpecificModule
}

func (e *imageInterfaceContextAdapter) SystemExtSpecific() bool {
	return e.kind == systemExtSpecificModule
}

// imageMutatorBeginMutator calls ImageMutatorBegin on all modules that may have image variants.
// This happens right before the imageTransitionMutator runs. It's needed to initialize these
// modules so that they return the correct results for all the other ImageInterface methods,
// which the imageTransitionMutator will call. Transition mutators should also not mutate modules
// (except in their Mutate() function), which this method does, so we run it in a separate mutator
// first.
func imageMutatorBeginMutator(ctx BottomUpMutatorContext) {
	if m, ok := ctx.Module().(ImageInterface); ok && ctx.Os() == Android {
		m.ImageMutatorBegin(ctx)
	}
}

// imageTransitionMutator creates variants for modules that implement the ImageInterface that
// allow them to build differently for each partition (recovery, core, vendor, etc.).
type imageTransitionMutator struct{}

func getImageVariations(ctx ImageInterfaceContext) []string {
	var variations []string

	if ctx.Os() != Android {
		return []string{""}
	}
	m, ok := ctx.Module().(ImageInterface)

	if !ok || !m.ImageMutatorSupported() {
		return []string{""}
	}

	// Core variants must be the first.
	if m.CoreVariantNeeded(ctx) {
		variations = append(variations, CoreVariation)
	}
	if m.RamdiskVariantNeeded(ctx) {
		variations = append(variations, RamdiskVariation)
	}
	if m.VendorRamdiskVariantNeeded(ctx) {
		variations = append(variations, VendorRamdiskVariation)
	}
	if m.DebugRamdiskVariantNeeded(ctx) {
		variations = append(variations, DebugRamdiskVariation)
	}
	// Product variants must be followed by the vendor variants because a product_specific
	// module may have "vendor_available: true"
	if m.ProductVariantNeeded(ctx) {
		variations = append(variations, ProductVariation)
	}
	// Vendor variants must be followed by the recovery variants because a vendor module may
	// have "recovery_available: true"
	if m.VendorVariantNeeded(ctx) {
		variations = append(variations, VendorVariation)
	}
	if m.RecoveryVariantNeeded(ctx) {
		variations = append(variations, RecoveryVariation)
	}

	extraVariations := m.ExtraImageVariations(ctx)
	variations = append(variations, extraVariations...)

	if len(variations) == 0 {
		return []string{""}
	}

	return variations
}

func (imageTransitionMutator) Split(ctx BaseModuleContext) []string {
	return getImageVariations(ctx)
}

func (imageTransitionMutator) OutgoingTransition(ctx OutgoingTransitionContext, sourceVariation string) string {
	// Request the appropriate image variation of a dependency from `filesystem`.
	// imageMutator currently does not split ImageInterface modules for every
	// android partition (e.g. there is no system_dlkm variation).
	// For such cases, the default "" variation will be requested.
	if partition, ok := ctx.Module().(PartitionTypeInterface); ok {
		if partition.PartitionType() == VendorVariation {
			return VendorVariation
		} else if partition.PartitionType() == ProductVariation {
			return ProductVariation
		} else if partition.PartitionType() == RecoveryVariation {
			return RecoveryVariation
		} else if partition.PartitionType() == RamdiskVariation {
			return RamdiskVariation
		} else if partition.PartitionType() == VendorRamdiskVariation {
			return VendorRamdiskVariation
		} else if partition.PartitionType() == DebugRamdiskVariation {
			return DebugRamdiskVariation
		} else {
			return CoreVariation // default "" variation
		}
	}
	return sourceVariation
}

func (imageTransitionMutator) IncomingTransition(ctx IncomingTransitionContext, incomingVariation string) string {
	if ctx.Os() != Android {
		return CoreVariation
	}

	m, ok := ctx.Module().(ImageInterface)
	if !ok || !m.ImageMutatorSupported() {
		return CoreVariation
	}
	variations := getImageVariations(&imageInterfaceContextAdapter{
		IncomingTransitionContext: ctx,
		kind:                      determineModuleKind(ctx.Module().base(), ctx),
	})
	// If there's only 1 possible variation, use that. This is a holdover from when blueprint,
	// when adding dependencies, would use the only variant of a module regardless of its variations
	// if only 1 variant existed.
	if len(variations) == 1 {
		// TODO(b/424439549): this fallback has accidentally allowed modules to depend on modules from other
		// images if the dependency only has a single image variation, while the holdover it was trying to
		// emulate would only allow the dependency if it had a single _variant_, meaning no variations from any
		// mutator.  This has allowed vendor modules to depend on core modules if the core module had no
		// other image variations, and for core modules to depend on vendor modules if they set vendor: true
		// and so only have a single vendor variation.  These existing violations will be cleaned up incrementally,
		// and the fallback conditions tightened to prevent backsliding.
		if ctx.Config().GetBuildFlagBool("RELEASE_SOONG_FIX_IMAGE_VARIANT_FALLBACK") {
			// Disable the fallback from vendor to the core variation.
			if incomingVariation == "vendor" && variations[0] == CoreVariation {
				return incomingVariation
			}
		}
		return variations[0]
	}
	return incomingVariation
}

func (imageTransitionMutator) Mutate(ctx BottomUpMutatorContext, variation string) {
	ctx.Module().base().setImageVariation(variation)
	if variations := getImageVariations(ctx); len(variations) > 1 && variation != variations[0] {
		ctx.Module().base().setNonPrimaryImageVariation()
	}
	if m, ok := ctx.Module().(ImageInterface); ok {
		m.SetImageVariation(ctx, variation)
	}
}
