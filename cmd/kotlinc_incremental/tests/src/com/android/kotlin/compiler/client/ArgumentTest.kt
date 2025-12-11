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

package com.android.kotlin.compiler.client

import com.google.common.truth.Truth.assertThat
import java.io.File
import java.nio.file.attribute.PosixFilePermission
import kotlin.io.path.setPosixFilePermissions
import org.junit.Assert.assertThrows
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class ArgumentTest {

    private val opts = ClientOptions()

    @get:Rule val tFolder = TemporaryFolder()

    @Before fun setup() {}

    @Test
    fun testSourcesArgument_NoAdditional() {
        val dda = SourcesArgument()
        assertThat(dda.matches("--")).isTrue()
        dda.parse("--", emptyList<String>().iterator(), opts)
        assertThat(opts.sources.size).isEqualTo(0)
    }

    @Test
    fun testSourcesArgument_OneArgument() {
        val dda = SourcesArgument()
        val arg1 = "foo"
        dda.parse("--", listOf(arg1).iterator(), opts)
        assertThat(opts.sources.size).isEqualTo(1)
        assertThat(opts.sources.get(0)).isEqualTo(arg1)
    }

    @Test
    fun testSourcesArgument_MultiArgument() {
        val dda = SourcesArgument()
        // Test a variety of argument formats, even though we treat them all as source.
        val args = listOf("foo", "bar", "-cp", "this:is:a:classpath", "-single", "-with-arg", "arg")
        dda.parse("--", args.iterator(), opts)
        assertThat(opts.sources.size).isEqualTo(args.size)
        assertThat(opts.sources).isEqualTo(args)
    }

    @Test
    fun testBuildFileArgument() {
        val bfa = BuildFileArgument()
        val buildFile = File("tests/resources/test_build.xml")
        val arg = "-build-file=" + buildFile.absoluteFile
        bfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(opts.buildFileLocation).isEqualTo(buildFile.absolutePath)
    }

    @Test
    fun testBuildFileArgument_SetsOptions() {
        val bfa = BuildFileArgument()
        val buildFile = File("tests/resources/test_build.xml")
        val arg = "-build-file=" + buildFile.absoluteFile
        bfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(opts.buildFileLocation).isEqualTo(buildFile.absolutePath)
        assertThat(opts.buildFileClassPaths).containsExactly("a.jar", "b.jar")
        assertThat(opts.buildFileJavaSources).containsExactly("c.java", "d.java")
        assertThat(opts.buildFileSources).containsExactly("e.kt", "f.kt")
        assertThat(opts.buildFileModuleName).isEqualTo("test_module")
        assertThat(opts.outputDirName).isEqualTo("output")
    }

    @Test
    fun testBuildFileArgument_NoArgument() {
        val bfa = BuildFileArgument()
        val arg = "-build-file="
        bfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThrows(IllegalStateException::class.java, { opts.buildFileLocation })
        assertThat(bfa.error).isNotEmpty()
    }

    @Test
    fun testLogDirArgument() {
        val lfa = LogDirArgument()
        val logDirLocation = tFolder.root.absolutePath
        val arg = "-log-dir=$logDirLocation"
        assertThat(lfa.matches(arg)).isTrue()
        lfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.logDirLocation).isEqualTo(logDirLocation)
        assertThat(lfa.error).isNull()
    }

    @Test
    fun testLogDirArgument_NoArgument() {
        val lfa = LogDirArgument()
        val arg = "-log-dir="
        lfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThrows(IllegalStateException::class.java, { opts.logDirLocation })
        assertThat(lfa.error).isNotEmpty()
    }

    @Test
    fun testLogDirArgument_NoWritePermissions() {
        val lfa = LogDirArgument()
        val readOnlyDirectory = tFolder.newFolder("read-only")
        // Remove write permissions
        readOnlyDirectory
            .toPath()
            .setPosixFilePermissions(
                setOf(
                    PosixFilePermission.OWNER_READ,
                    PosixFilePermission.OWNER_EXECUTE,
                    PosixFilePermission.GROUP_READ,
                    PosixFilePermission.GROUP_EXECUTE,
                    PosixFilePermission.OTHERS_READ,
                    PosixFilePermission.OTHERS_EXECUTE,
                )
            )

        val logDirLocation = readOnlyDirectory.absolutePath
        val arg = "-log-dir=$logDirLocation"
        lfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThrows(IllegalStateException::class.java, { opts.logDirLocation })
        assertThat(lfa.error).isNotEmpty()
    }

    @Test
    fun testRunFilesArgument() {
        val rfa = RunFilesArgument()
        val runFilesLocation = tFolder.root.absolutePath
        val arg = "-run-files-path=$runFilesLocation"
        assertThat(rfa.matches(arg)).isTrue()
        rfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.runFilesLocation).isEqualTo(runFilesLocation)
        assertThat(rfa.error).isNull()
    }

    @Test
    fun testRunFilesArgument_NoArgument() {
        val rfa = RunFilesArgument()
        val arg = "-run-files-path="
        rfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThrows(IllegalStateException::class.java, { opts.runFilesLocation })
        assertThat(rfa.error).isNotEmpty()
    }

    @Test
    fun testRunFilesArgument_NoWritePermissions() {
        val rfa = RunFilesArgument()
        val readOnlyDirectory = tFolder.newFolder("read-only")
        // Remove write permissions
        readOnlyDirectory
            .toPath()
            .setPosixFilePermissions(
                setOf(
                    PosixFilePermission.OWNER_READ,
                    PosixFilePermission.OWNER_EXECUTE,
                    PosixFilePermission.GROUP_READ,
                    PosixFilePermission.GROUP_EXECUTE,
                    PosixFilePermission.OTHERS_READ,
                    PosixFilePermission.OTHERS_EXECUTE,
                )
            )

        val runFilesLocation = readOnlyDirectory.absolutePath
        val arg = "-run-files-path=$runFilesLocation"

        rfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThrows(IllegalStateException::class.java, { opts.runFilesLocation })
        assertThat(rfa.error).isNotEmpty()
    }

    @Test
    fun testBuildHistoryFileArgument() {
        val bfa = BuildHistoryFileArgument()
        val tFile = tFolder.newFile("build")
        val arg = "-build-history=" + tFile.absolutePath
        bfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(opts.buildHistoryFileName).isEqualTo(tFile.absolutePath)
    }

    @Test
    fun testBuildHistoryFileArgument_NoArgument() {
        val bfa = BuildHistoryFileArgument()
        val arg = "-build-history="
        bfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(bfa.error).isNotEmpty()
    }

    @Test
    fun testRootDirArgument() {
        val rfa = RootDirArgument()
        val rootDirLocation = tFolder.root.absolutePath
        val arg = "-root-dir=$rootDirLocation"
        assertThat(rfa.matches(arg)).isTrue()
        rfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.rootDirLocation).isEqualTo(rootDirLocation)
        assertThat(rfa.error).isNull()
    }

    @Test
    fun testRootDirArgument_NoArgument() {
        val rfa = RootDirArgument()
        val arg = "-root-dir="
        rfa.parse(arg, emptyList<String>().iterator(), opts)
        assertThrows(IllegalStateException::class.java, { opts.rootDirLocation })
        assertThat(rfa.error).isNotEmpty()
    }

    @Test
    fun testRootDirArgument_NoWritePermissions() {
        val rfa = RootDirArgument()
        val readOnlyDirectory = tFolder.newFolder("read-only")
        // Remove write permissions
        readOnlyDirectory
            .toPath()
            .setPosixFilePermissions(
                setOf(
                    PosixFilePermission.OWNER_READ,
                    PosixFilePermission.OWNER_EXECUTE,
                    PosixFilePermission.GROUP_READ,
                    PosixFilePermission.GROUP_EXECUTE,
                    PosixFilePermission.OTHERS_READ,
                    PosixFilePermission.OTHERS_EXECUTE,
                )
            )

        val rootDirLocation = readOnlyDirectory.absolutePath
        val arg = "-root-dir=$rootDirLocation"

        rfa.parse(arg, emptyList<String>().iterator(), opts)

        assertThrows(IllegalStateException::class.java, { opts.rootDirLocation })
        assertThat(rfa.error).isNotEmpty()
    }

    @Test
    fun testWorkingDirArgument() {
        val wda = WorkingDirArgument()
        val arg = "-working-dir=FOOBAR"
        assertThat(wda.matches(arg)).isTrue()
        wda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.workingDirName).isEqualTo("FOOBAR")
        assertThat(wda.error).isNull()
    }

    @Test
    fun testWorkingDirArgument_NoArgument() {
        val wda = WorkingDirArgument()
        val arg = "-working-dir="
        wda.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testWorkingDirArgument_PathTraversal() {
        val wda = WorkingDirArgument()
        val arg = "-working-dir=../FOOBAR"
        wda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testOutputDirArgument() {
        val oda = OutputDirArgument()
        val arg = "-output-dir=FOOBAR"
        assertThat(oda.matches(arg)).isTrue()
        oda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.outputDirName).isEqualTo("FOOBAR")
        assertThat(oda.error).isNull()
    }

    @Test
    fun testOutputDirArgument_NoArgument() {
        val wda = OutputDirArgument()
        val arg = "-output-dir="
        wda.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testOutputDirArgument_PathTraversal() {
        val wda = OutputDirArgument()
        val arg = "-output-dir=../FOOBAR"
        wda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testBuildDirArgument() {
        val oda = BuildDirArgument()
        val arg = "-build-dir=FOOBAR"
        assertThat(oda.matches(arg)).isTrue()
        oda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.buildDirName).isEqualTo("FOOBAR")
        assertThat(oda.error).isNull()
    }

    @Test
    fun testBuildDirArgument_NoArgument() {
        val wda = BuildDirArgument()
        val arg = "-build-dir="
        wda.parse(arg, emptyList<String>().iterator(), opts)
        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testBuildDirArgument_PathTraversal() {
        val wda = BuildDirArgument()
        val arg = "-build-dir=../FOOBAR"
        wda.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(wda.error).isNotEmpty()
    }

    @Test
    fun testClassPathArgument() {
        val cpa = ClassPathArgument()
        val paths = listOf("foo", "bar", "baz")
        val arg = "-classpath=" + paths.joinToString(":")
        assertThat(cpa.matches(arg)).isTrue()
        cpa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.classPath).isEqualTo(paths)
    }

    @Test
    fun testClassPathArgument_NoArgument() {
        val cpa = ClassPathArgument()
        val arg = "-classpath="
        cpa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.classPath).isEmpty()
        assertThat(cpa.error).isNotEmpty()
    }

    @Test
    fun testPluginArgument() {
        val pa = PluginArgument()
        val arg = "-Xplugin=foo"
        assertThat(pa.matches(arg)).isTrue()
        pa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.classPath).contains("foo")
        assertThat(opts.passThroughArgs).contains("-Xplugin=foo")
    }

    @Test
    fun testPluginArgument_NoArgument() {
        val pa = PluginArgument()
        val arg = "-Xplugin="
        pa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.classPath).isEmpty()
        assertThat(opts.passThroughArgs).isEmpty()
        assertThat(pa.error).isNotEmpty()
    }

    @Test
    fun testPluginArgument_FirstInClassPath() {
        val pa = PluginArgument()
        val arg = "-Xplugin=foo"

        opts.classPath.addAll(listOf("a", "b", "c"))
        assertThat(opts.classPath.first()).isNotEqualTo("foo")

        assertThat(pa.matches(arg)).isTrue()
        pa.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.classPath.first()).isEqualTo("foo")
        assertThat(opts.passThroughArgs).contains("-Xplugin=foo")
    }

    @Test
    fun testJvmArgument() {
        val jvma = JvmArgument()
        val arg = "-J-option"
        assertThat(jvma.matches(arg)).isTrue()
        jvma.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.jvmArgs).contains("option")
    }

    @Test
    fun testPluginArgument_WithEquals() {
        val jvma = JvmArgument()
        val arg = "-J-option=foo"
        jvma.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.jvmArgs).contains("option=foo")
    }

    @Test
    fun testPluginArgument_DoubleDash() {
        val jvma = JvmArgument()
        val arg = "-J--option=foo"
        jvma.parse(arg, emptyList<String>().iterator(), opts)

        assertThat(opts.jvmArgs).contains("-option=foo")
    }
}
