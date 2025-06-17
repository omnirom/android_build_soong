package android

import (
	"fmt"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	InitRegistrationContext.RegisterModuleType("removed_package", removedPackageModuleFactory)
}

type removedPackageModuleProps struct {
	// The error message to display when this module is built. This is optional, there is a
	// reasonable default message.
	Message *string
}

type removedPackageModule struct {
	ModuleBase
	properties removedPackageModuleProps
}

// removed_package will cause a build failure when it's included in PRODUCT_PACKAGES. It's needed
// because currently you can add non-existent packages to PRODUCT_PACKAGES, and the build will
// not notice/complain, unless you opt-into enforcement via $(call enforce-product-packages-exist).
// Opting into the enforcement is difficult in some cases, because a package exists on some source
// trees but not on others. removed_package is an intermediate solution that allows you to remove
// a package and still get an error if it remains in PRODUCT_PACKAGES somewhere.
func removedPackageModuleFactory() Module {
	m := &removedPackageModule{}
	InitAndroidModule(m)
	m.AddProperties(&m.properties)
	return m
}

var removedPackageRule = pctx.AndroidStaticRule("removed_package", blueprint.RuleParams{
	Command: "echo $message && false",
}, "message")

func (m *removedPackageModule) GenerateAndroidBuildActions(ctx ModuleContext) {
	// Unchecked module so that checkbuild doesn't fail
	ctx.UncheckedModule()

	out := PathForModuleOut(ctx, "out.txt")
	message := fmt.Sprintf("%s has been removed, and can no longer be used.", ctx.ModuleName())
	if m.properties.Message != nil {
		message = *m.properties.Message
	}
	ctx.Build(pctx, BuildParams{
		Rule:   removedPackageRule,
		Output: out,
		Args: map[string]string{
			"message": proptools.ShellEscape(message),
		},
	})

	ctx.InstallFile(PathForModuleInstall(ctx, "removed_module"), ctx.ModuleName(), out)
}
