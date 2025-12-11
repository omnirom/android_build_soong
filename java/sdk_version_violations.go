// Copyright 2025 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package java

var SdkVersionDependencyViolationAllowlist = map[string][]string{
	// go/keep-sorted start case=no block=yes newline_separated=yes
	"ActivityContext": {
		"compatibility-device-util-axt", //  public -> test
		"EventLib",                      //  public -> system
	},

	"adservices-test-utility": {
		"adservices-clients",                   // module-lib -> private
		"compatibility-device-util-axt",        // module-lib -> test
		"modules-utils-testable-device-config", // module-lib -> private
	},

	"adservices-ui-cts-root-test-lib": {
		"compatibility-device-util-axt", // module-lib -> test
	},

	"adservices-ui-test-utility": {
		"adservices-clients",            // module-lib -> private
		"compatibility-device-util-axt", // module-lib -> test
	},

	"bedstead": {
		"bedstead-multiuser", // system -> test
	},

	"bedstead-adb": {
		"Nene", // public -> test
	},

	"bedstead-enterprise": {
		"bedstead-testapps",          // system -> test
		"RemoteAccountAuthenticator", // system -> test
	},

	"BluetoothServices": {
		"TwoPanelSettingsLib", // system -> private
	},

	"CaptivePortalLoginTestLib": {
		"net-tests-utils", // module-lib -> core_platform
	},

	"car-admin-ui-lib": {
		"SettingsLib", // module-lib -> private
	},

	"car-broadcastradio-support": {
		"car-broadcastradio-support-source", // public -> system
	},

	"car-media-common": {
		"car-media-extensions-source", // public -> system
	},

	"car-media-common-no-overlayable": {
		"car-media-extensions-source", // public -> system
	},

	"car-media-common-source": {
		"android.car-system-stubs", // public -> system
	},

	"car-media-extensions": {
		"car-media-extensions-source", // public -> system
	},

	"car-service-test-static-lib": {
		"android.car.builtin.testonly", // module-lib -> core_platform
	},

	"car-telephony-common": {
		"car-telephony-common-source", // public -> system
	},

	"car-uxr-client-lib-source": {
		"android.car-system-stubs", // public -> system
	},

	"CarUiPortraitNotificationRROResLib": {
		"CarNotificationLib", // system -> private
	},

	"CtsNetHttpTestsLib": {
		"net-tests-utils", // module-lib -> core_platform
	},

	"EventLib": {
		"NeneInternal", // system -> test
		"Queryable",    // system -> test
	},

	"HarrierCommonAndroid": {
		"flag-junit",         // public -> module-lib
		"Nene",               // public -> test
		"TestApisReflection", // public -> system
	},

	"HarrierInternal": {
		"compatibility-device-util-axt", // system -> test
		"Nene",                          // system -> test
	},

	"healthfitness-testing-unittest": {
		"healthfitness-testing-shared", // module-lib -> test
		"service-healthfitness.impl",   // module-lib -> system-server
	},

	"Launcher3ResLib": {
		"animationlib",               // public -> system
		"contextualeducationlib",     // public -> system
		"launcher-dagger-qualifiers", // public -> private
		"mechanics",                  // public -> system
		"msdl",                       // public -> system
		"SystemUI-statsd",            // public -> private
		"view_capture",               // public -> private
	},

	"mdd-robolectric-library": {
		"org.apache.http.legacy.stubs.system", // public -> system
	},

	"MotionMechanicsDemoLib": {
		"mechanics",                            // public -> system
		"PlatformComposeSceneTransitionLayout", // public -> private
	},

	"perfetto_src_android_sdk_java_test_perfetto_trace_test_lib": {
		"perfetto_trace_java_protos", // public -> private
	},

	"perfetto_trace_lib": {
		"perfetto_trace_lib_java", // public -> module-lib
	},

	"PlatformComposeSceneTransitionLayoutDemoLib": {
		"PlatformComposeSceneTransitionLayout", // public -> private
	},

	"RemoteFrameworkClasses": {
		"ConnectedAppsSDK", // system -> test
	},

	"SdkWifiTrackerLib": {
		"WifiTrackerLibRes", // system -> private
	},

	"SettingsLib-search": {
		"SettingsLib-search-interface", // system -> private
	},

	"SettingsLibCategory": {
		"SettingsLibMetadata",   // system -> private
		"SettingsLibPreference", // system -> private
	},

	"SettingsLibMainSwitchPreference": {
		"SettingsLibPreference", // system -> private
	},

	"SettingsLibSliderPreference": {
		"SettingsLibPreference", // system -> private
	},

	"SettingsLibUtils": {
		"SettingsLibDataStore", // system -> private
	},

	"Spa-search": {
		"Spa-search-protos-lite", // system -> private
	},

	"stable_cronet_java_tests": {
		"net-tests-utils", // module-lib -> core_platform
	},

	"TestApp": {
		"ConnectedAppsSDK", // system -> test
		"Nene",             // system -> test
		"Queryable",        // system -> test
	},

	"TestApp_TestApps": {
		"ConnectedAppsSDK", // system -> test
	},

	"TetheringApiCurrentLib": {
		"tetheringstatsprotos", // module-lib -> private
	},

	"TetheringApiStableLib": {
		"tetheringstatsprotos", // module-lib -> private
	},

	"tot_cronet_java_tests": {
		"net-tests-utils", // module-lib -> core_platform
	},
	// go/keep-sorted end
}
