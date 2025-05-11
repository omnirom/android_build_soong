package tradefed

import (
	"android/soong/android"

	"github.com/google/blueprint"
)

// Data that test_module_config[_host] modules types will need from
// their dependencies to write out build rules and AndroidMkEntries.
type BaseTestProviderData struct {
	// data files and apps installed for tests, relative to testcases dir.
	TestcaseRelDataFiles []string
	// apk for android_test
	OutputFile android.Path
	// Either handwritten or generated TF xml.
	TestConfig android.Path
	// Other modules we require to be installed to run tests. We expect base to build them.
	HostRequiredModuleNames []string
	RequiredModuleNames     []string
	// List of test suites base uses.
	TestSuites []string
	// True indicates the base modules is built for Host.
	IsHost bool
	// Base's sdk version for AndroidMkEntries, generally only used for Host modules.
	LocalSdkVersion string
	// Base's certificate for AndroidMkEntries, generally only used for device modules.
	LocalCertificate string
	// Indicates if the base module was a unit test.
	IsUnitTest bool
	// The .mk file is used AndroidMkEntries for base (soong_java_prebuilt, etc.)
	MkInclude string
	// The AppClass to use for the AndroidMkEntries for the base.
	MkAppClass string
	// value for LOCAL_MODULE_PATH.  The directory where the module is installed.
	InstallDir android.InstallPath
}

var BaseTestProviderKey = blueprint.NewProvider[BaseTestProviderData]()
