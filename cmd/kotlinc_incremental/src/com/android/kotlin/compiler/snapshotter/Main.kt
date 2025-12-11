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

import com.android.kotlin.compiler.cli.parseArgs
import java.io.File
import java.net.URLClassLoader
import kotlin.system.exitProcess
import org.jetbrains.kotlin.buildtools.api.CompilationService
import org.jetbrains.kotlin.buildtools.api.ExperimentalBuildToolsApi
import org.jetbrains.kotlin.buildtools.api.jvm.ClassSnapshotGranularity

private val ARGUMENT_PARSERS = listOf(JarArgument(), HelpArgument())

val USAGE_TEXT = """
        Usage: kotlin-jar-snapshotter -jar=<jarfile>
    """.trimIndent()

fun main(args: Array<String>) {
    val opts = SnapshotterOptions()
    ARGUMENT_PARSERS.forEach { it.setupDefault(opts) }

    if (!parseArgs(args, opts, ARGUMENT_PARSERS, System.out, System.err, USAGE_TEXT)) {
        exitProcess(-1)
    }

    if (opts.jarFile == null) {
        println("No jar specified. Exiting.")
        exitProcess(0)
    }

    snapshotJar(opts.jarFile!!)
}

@OptIn(ExperimentalBuildToolsApi::class)
fun snapshotJar(jar: File) {
    val snf = fileToSnapshotFile(jar)

    val loader =
        URLClassLoader(
            // Need to include this code's own jar in the classpath.
            arrayOf(SnapshotterOptions::class.java.protectionDomain?.codeSource?.location)
        )

    val service = CompilationService.loadImplementation(loader)
    val sn = service.calculateClasspathSnapshot(jar, ClassSnapshotGranularity.CLASS_MEMBER_LEVEL)
    sn.saveSnapshot(snf)
}
