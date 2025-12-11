// Copyright 2025 The Android Open Source Project
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

package rust

import (
	"strings"
	"testing"
)

// Smoke test rust_object_host and also check the emit type is correct.
func TestObjectEmitType(t *testing.T) {
	ctx := testRust(t, `
		rust_object_host {
			name: "foors",
			srcs: ["foo.rs"],
			crate_name: "foo",
		}`)

	libfooRlib := ctx.ModuleForTests(t, "foors", "linux_glibc_x86_64").Rule("rustc")
	if !strings.Contains(libfooRlib.Args["emitType"], "obj") {
		t.Errorf("rust_object_host not emitting type obj")
	}
}
