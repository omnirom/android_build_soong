# Introduction

The Android build system is undergoing significant changes, with the Make build system (using Android.mk files) being deprecated and replaced by a newer system: Soong. This transition is necessary because the Make build system has reached its scaling limits in terms of correctness and performance.

Android.bp files are simple build configurations parsed by the Blueprint framework and Soong. Soong is developed based on the Blueprint framework. The primary goal of this conversion is to improve build times, as the theoretical build time of Android.bp is faster than Android.mk. Consequently, the module build in Android.mk will be deprecated in future Android versions, with Soong modernization as the objective.


# Converting Android.mk to Android.bp

We will go through some topics in the conversion process.

## Basic Conversion Procedure

The conversion of an Android.mk file to an Android.bp file typically follows this general workflow using the `androidmk` helper tool. In general the following steps are followed in the conversion.

* Step 1: Set up the terminal environment and build `androidmk` tool.

    `androidmk` is a command line tools that parses a Android.mk file and tries to
    output an analogous Android.bp files. It can convert most of the simple Android.mk
    files to Android.bp with few or no manual changes.

    ```sh
    cd <root-of-the-tree>
    source build/envsetup.sh
    lunch <lunch-target>
    m androidmk
    ```

* Step 2: Build with the Android.mk
    ```sh
    m <module-name>
    ```

* Step 3: Run the conversion tool `androidmk`
    ```sh
    androidmk <path-to-Android.mk>/Android.mk > <path-to-Android.bp>/Android.bp
    ```

* Step 4: Make necessary manual edits in the Android.bp

    * Address any warnings emitted by the `androidmk` tool.
    * Retain or add a copyright header. If adding a new one, use the current year.

* Step 5: Remove the Android.mk and build with the Android.bp

* Step 6: Validate the conversion by comparing the built artifacts or running unit / functional tests

* Step 7: Save the changes and upload for review

### Example of Android.mk and converted-to Android.bp

Here is an example of Android.mk and the Android.bp it is converted to.

**Android.mk:**
```makefile
LOCAL_PACKAGE_NAME := messagingtests
LOCAL_SRC_FILES := $(call all-java-files-under, src)
LOCAL_INSTRUMENTATION_FOR := messaging
LOCAL_SDK_VERSION := current
LOCAL_COMPATIBILITY_SUITE := general-tests
LOCAL_STATIC_JAVA_LIBRARIES := mockito-target
LOCAL_JAVA_LIBRARIES := android.test.mock.stubs
include $(BUILD_PACKAGE)
```

**Android.bp:**
```
android_test {
    name: "messagingtests",
    srcs: ["src/**/*.java"],
    instrumentation_for: "messaging",
    sdk_version: "current",
    test_suites: ["general-tests"],
    static_libs: ["mockito-target"],
    libs: ["android.test.mock.stubs"],
}
```

## Android.bp module types and examples

Here is a list of examples of commonly used module types in Android development.

### android_app

android_app compiles sources and Android resources into an Android application
package `.apk` file.

```
android_app {
    name: "SampleApp",
    srcs: ["src/**/*.java"],
    resource_dirs: ["res"],
    sdk_version: "current",
    product_specific: true,
}
```

### android_app_import

android_app_import imports a prebuilt apk with additional processing specified
in the module. DPI-specific apk source files can be specified using
dpi_variants.

```
android_app_import {
    name: "example_import",
    apk: "prebuilts/example.apk",
    dpi_variants: {
        mdpi: {
            apk: "prebuilts/example_mdpi.apk",
        },
        xhdpi: {
            apk: "prebuilts/example_xhdpi.apk",
        },
    },
    presigned: true,
}
```

### android_test

android_test compiles test sources and Android resources into an Android
application package `.apk` file and creates an `AndroidTest.xml` file to allow
running the test with `atest` or a `TEST_MAPPING` file.

```
android_test {
    name: "SampleTest",
    srcs: ["src/**/*.java"],
    sdk_version: "current",
    min_sdk_version: "21",
    static_libs: ["androidx.test.runner"],
    certificate: "platform",
    test_suites: ["device-tests"],
    test_options: {
        unit_test: false,
    },
}
```

### android_test_helper_app

android_test_helper_app compiles sources and Android resources into an Android
application package `.apk` file that will be used by tests, but does not produce
an `AndroidTest.xml` file so the module will not be run directly as a test.

```
android_test_helper_app {
    name: "FooTestHelper",
    static_libs: [
        "androidx.test.ext.junit",
        "truth",
    ],
    srcs: [
        "helper-app/src/**/*.java",
    ],
    test_suites: [
        "general-tests",
    ],
    manifest: "helper-app/AndroidManifest.xml",
    platform_apis: true,
}
```

## Techniques in Android.mk to Android.bp conversion

There are some commonly used techniques Android.mk to Android.bp conversion.

### genrule

From example(external/guice/Android.bp), we can see how to use genrule in Android.bp.
```
java_binary_host {
    name: "guice_munge",
    srcs: [":guice_munge_srcjar"],
    manifest: ":guice_munge_manifest",
    libs: ["junit"],
}
```

The java_binary_module guice_munge wants to unzip a jar file to get a manifest value. We can define a `genrule` module for the function of copying and renaming.

```
genrule {
    name: "guice_munge_manifest",
    out: ["guice_munge.manifest"],
    srcs: ["lib/build/munge.jar"],
    cmd: "unzip -p -q $(in) META-INF/MANIFEST.MF > $(out)",
}
```

### Conditionals / ifeq statements

There are many kinds of `ifeq / ifneq` conditions in Android.mk files. The major purpose
is to branch different groups of `srcs`, `cflags`, etc.

**NOTE:** It is recommended to clean up Android.mk files and remove obsoleted logic and conditionals before starting conversion.

To convert conditionals in Android.mk, we can use [select statements](/docs/selects.md) or
[soong_config_module_type](/README.md#conditionals).

#### Cleanup conditionals

In general the following cases using `ifeq / ifneq` can be cleaned up before conversion.

* Enable / disable modules
* Check for some specific devices
* Define certain out-of-date flags

##### Example 1: the definition of a module is guarded by `ifeq / ifneq`

A module is defined selectively by checking if its name is listed in
`$(PRODUCT_PACKAGES_DEBUG)` or similar product configuration variables, for which
we can remove the `if` condition before running `androidmk` to convert.

**NOTE:** In Android.bp we should try to keep a module always defined and enabled, and use it in product configuration selectively.

```makefile
ifneq ($(filter Module1,$(PRODUCT_PACKAGES_DEBUG)),)
...
LOCAL_MODULE := Module1
...
endif
```

##### Example 2: module properties are modified by checking EOL `TARGET_DEVICE`

For those device-specific conditions and the devices are already end-of-life,
we can remove conditions and statements since they have never been executed.

```makefile
ifeq ($(TARGET_DEVICE),eol_device1)
  LOCAL_CFLAGS += -DUSE_X_COMMANDS
endif

ifeq ($(TARGET_DEVICE),eol_device2)
  LOCAL_CFLAGS += -DPOLL_CALL_STATE -DUSE_X
endif
```

#### Convert conditionals with `select` statements

For more details of `select` statements, see [select statements](/docs/selects.md).

The supported functions we can `select` on are:

- soong_config_variable
- product_variable
- release_flag

##### Example 1: `soong_config_variable()`

In some product configurations, there is soong_config_variable:

```makefile
$(call soong_config_set,POWERX,DEBUG,release)
```

In Android.bp, use `soong_config_variable` function to get value and set to properties.

```
cc_defaults {
    name: "my_module",
    cppflags: select(soong_config_variable("POWERX", "DEBUG"),{
        "release": ["-Wno-unused-variable"],
        default: [],
    }),
    ...
}
```

##### Example 2: `product_variable()`

```
android_system_image {
    name: "android_img",
    deps: select(product_variable("debuggable"), {
        true: ["adb_keys"],
        default: [],
    }),
}
```

##### Example 3: `release_flag()`

Release flags are defined under build/release. In file RELEASE_PACKAGE_XX_APP.textproto, set release flag "RELEASE_PACKAGE_XX_APP" as "15.0.0".

```
name: "RELEASE_PACKAGE_XX_APP"
value: {
  string_value: "15.0.0"
}
```

In Android.bp, the value of "RELEASE_PACKAGE_XX_APP" can be gotten by `release_flag()`:

```
XX_APP_VERSION = select(release_flag("RELEASE_PACKAGE_XX_APP"), {
    any @ version: version,
})

android_app_import {
    name: "XX_APP",
    arch: {
        arm: {
            apk: XX_APP_VERSION + "/XX_APP_armeabi-v7a.apk",
        },
        arm64: {
            apk: XX_APP_VERSION + "/XX_APP_arm64-v8a.apk",
        },
        x86_64: {
            apk: XX_APP_VERSION + "/XX_APP_x86_64.apk",
        },
    },
    ...
}
```

#### Convert conditionals with `soong_config_module_type`

[select statements](/docs/selects.md) is more recommended in handling conditionals in Android.bp.
If there are cases that select statements have not supported well,
`soong_config_module_type` can be used as an alternative solution, which might need some boilerplate code.

See more details of [soong_config_module_type](/README.md#soong-config-variables) and related module types.

When converting vendor modules that contain conditionals, simple conditionals
can be supported through Soong config variables using soong_config_* modules
that describe the module types, variables and possible values:

```
soong_config_module_type {
   name: "foo_cc_defaults",
   module_type: "cc_defaults",
   config_namespace: "FOO",
   bool_variables: [
       "foo_enable",
   ],
   value_variables: [
       "driver_module_path",
       "driver_module_arg",
   ],
   variables: [
       "board_wlan_device",
   ],
   properties: [
       "cflags",
   ],
}

soong_config_string_variable {
   name: "board_wlan_device",
   values: ["bwd_a", "bwd_b", "bwd_c"],
}
```

This example describes a new `foo_cc_defaults` module type that extends the
`cc_defaults` module type, with four additional conditionals based on variables
`foo_enable`, `driver_module_path`, `driver_module_arg` and `board_wlan_device`
which can affect properties `cflags`. The types of soong variables
control properties in the following ways.

* bool variable (e.g. `foo_enable`): Properties are applied if set to `true`.
* value variable (e.g. `driver_module_path`): (strings or lists of strings)
  The value are directly substituted into properties using `%s`.
* string variable (e.g. `board_wlan_device`): Properties are applied only if
  they match the variable's value.

The values of the variables can be set from a product's configuration file:

```makefile
$(call soong_config_set_bool,FOO,foo_enable,true)
$(call soong_config_set,FOO,driver_module_path,/lib/wlan.ko)
$(call soong_config_set,FOO,board_wlan_device,bwd_a)
```

The `foo_cc_defaults` module type can be used anywhere after the definition in
the file where it is defined, or can be imported into another file with `soong_config_module_type_import`:

```
soong_config_module_type_import {
    from: "device/foo/Android.bp",
    module_types: ["foo_cc_defaults"],
}
```

It can be used like any other module types:

```
foo_cc_defaults {
    name: "foo_defaults",
    cflags: ["-DGENERIC"],
    soong_config_variables: {
        board_wlan_device: {
            bwd_a: {
                cflags: ["-DBWD_A"],
            },
            bwd_b: {
                cflags: ["-DBWD_B"],
            },
            conditions_default: {
                cflags: ["-DBWD_DEFAULT"],
            },
        },
        foo_enable: {
            cflags: ["-DFEATURE"],
            conditions_default: {
                cflags: ["-DFEATURE_DEFAULT"],
            },
        },
        driver_module_path: {
            cflags: ["-DFOO_DRIVER_MODULE_PATH=%s"],
        },
        driver_module_arg: {
            cflags: ["-DFOO_DRIVER_MODULE_ARG=%s"],
        },
    },
}

cc_library {
    name: "lib_foo",
    defaults: ["foo_defaults"],
    srcs: ["*.cpp"],
}
```

#### Namespace Modules (soong_namespace)

Soong enables modules in different directories to have the same name, provided each is declared within a separate namespace.

* A `soong_namespace` declaration assigns the current directory's path as its name automatically.

* `soong_namespace()` must be the first module in an Android.bp file.

* To include specific namespaces in the Make build configuration, list their paths in the `PRODUCT_SOONG_NAMESPACES` variable.

* Module Dependency across Namespaces:
    * Method 1: Import the whole namespace using the imports property within `soong_namespace`.
    * Method 2: Assign the namespace for one module by referencing it directly with `//namespace:module_name`.
* Limitation: `bootstrap_go_package` cannot be used within a soong_namespace.

##### soong_namespace Example

The following example demonstrates the mapping of various TARGET_DEVICEs to
their corresponding prebuilt_app_folders.

The original Android.mk assigned prebuilt_app_folders by ifneq:

```
ifneq ($(filter dev1 dev2, $(TARGET_DEVICE)),)
  prebuilt_app_folder := PROD_SPEC_FOLDER
else
  prebuilt_app_folder := MOBILE_VERSION_FOLDER
endif
```

For converting to Android.bp, we can use namespace in product configuration to
differ TARGET_DEVICE.

* Step 1: Add `PRODUCT_SOONG_NAMESPACES` in produce configuration.

    We can add the bellowing configuration in device-dev1.mk and device-dev2.mk

    ```
    # MyApp
    MyApp_VERSION_DIR := PROD_SPEC_FOLDER
    PRODUCT_SOONG_NAMESPACES += path/MyApp/$(MyApp_VERSION_DIR)
    ```


* Step 2: Set soong_namespace in Android.bp

    Android.bp
    ```
    soong_namespace {}

    android_app_import {
        name: "MyApp",
        apk: "mobile-mdpiArmeabi-v7a-release.apk",
        ...
    },
    ...
    ```

### visibility settings

The default visibility settings is public. The visibility property can limit the
scope that opened for other modules.

#### Example for subpackages

If the "example_license" is defined in //example/Android.bp. Then it is visible
for all the modules defined under subfolder of //example. Such as modules in
//example/test/Android.bp.

```
license {
    name: "example_license",
    visibility: [":__subpackages__"],
    license_kinds: [
        "SPDX-license-identifier-Apache-2.0",
    ],
}
```

#### Example for specific folders

The visibility property can set the module is visible to which folders.

```
filegroup {
    name: "compatibility-tradefed-prebuilt",
    visibility: [
        "//tools/tradefederation/prebuilts/test_harness",
    ],
    srcs: ["compatibility-tradefed.jar"],
}
```

#### Private visibility

`//visibility:private`: Only rules in the module's package (not its subpackages)
can use this module.

## Module and Property Mapping

Most module types or properties in Make can be mapped to a module type or property in Soong. And the mapping is taken care of by the tool `androidmk` which is described in the following sections.

Here are some examples of modules types and properties in Make and their correpsonding entities in Soong.

**Mapping of Make / Soong module types:**

|Android.mk                        |Android.bp           |
|----------------------------------|-------------------- |
|include $(BUILD_SHARED_LIBRARY)   |cc_library_shared { }|
|include $(BUILD_STATIC_LIBRARY)   |cc_library_static {} |
|include $(BUILD_EXECUTABLE)       |cc_binary {}         |
|include $(BUILD_JAVA_LIBRARY)     |java_library {}      |
|include $(BUILD_PACKAGE)          |android_app {}       |
|include $(BUILD_NATIVE_TEST)      |cc_test {}           |
|include $(BUILD_NATIVE_BENCHMARK) |cc_benchmark {}      |
|include $(BUILD_HOST_JAVA_LIBRARY)|java_library_host {} |


**Mapping of Make / Soong modules properties**

|Android.mk             |Android.bp|||
|----------             |----------|-|-|
|**Property**           |**Property**|**Type**|**Example**|
|LOCAL_MODULE           |name|string|name: "zlib_example"|
|LOCAL_PACKAGE_NAME     |name|string|name: "QuickSearchBox"|
|LOCAL_SRC_FILES        |srcs|list|srcs: ["src/test/example.c"]|
|LOCAL_C_INCLUDES       |include_dirs|list|include_dirs: ["include"]|
|LOCAL_CFLAGS|cflags    |list|cflags: ["-Wall", "-Werror"]|
|LOCAL_SHARED_LIBRARIES |shared_libs|list|shared_libs: ["libz"]|
|LOCAL_VENDOR_MODULE    |vendor|bool|vendor: true|
|LOCAL_PRODUCT_MODULE   |product_specific|bool|product_specific: true|
|LOCAL_SYSTEM_EXT_MODULE|system_ext_specific|bool|system_ext_specific: true|

