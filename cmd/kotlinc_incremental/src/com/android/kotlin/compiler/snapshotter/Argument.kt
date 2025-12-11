/*
 * Copyright (C) 2025 The Android Open Source Project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package com.android.kotlin.compiler.snapshotter

import com.android.kotlin.compiler.cli.NoArgument
import com.android.kotlin.compiler.cli.StringArgument

class JarArgument : StringArgument<SnapshotterOptions>() {
    override val argumentName = "jar"

    override val helpText = """
        The jar to generate a snapshot of.
        """.trimIndent()

    override val default: String? = null

    override fun setOption(option: String, opts: SnapshotterOptions) {
        opts.jarFileName = option
    }
}

class HelpArgument : NoArgument<SnapshotterOptions>() {
    override val argumentName = "h"

    override val helpText = """
        Outputs this help text.
        """.trimIndent()

    override fun setOption(option: Boolean, opts: SnapshotterOptions) {}
}
