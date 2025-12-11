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

import com.android.kotlin.compiler.cli.Argument
import com.android.kotlin.compiler.cli.NoArgument
import com.android.kotlin.compiler.cli.StringArgument
import com.android.kotlin.compiler.cli.SubdirectoryArgument
import com.android.kotlin.compiler.cli.WritableDirectoryArgument
import java.io.File
import javax.xml.parsers.SAXParserFactory

class Verbose : NoArgument<ClientOptions>() {
    override val argumentName = "verbose"
    override val helpText =
        """
        Outputs additional information during compilation. Quite noisy.
        """
            .trimIndent()

    override fun setOption(option: Boolean, opts: ClientOptions) {
        opts.verbose = option
    }
}

class Debug : NoArgument<ClientOptions>() {
    override val argumentName = "debug"
    override val helpText =
        """
        Outputs additional information during compilation.
        """.trimIndent()

    override fun setOption(option: Boolean, opts: ClientOptions) {
        opts.debug = option
    }
}

class SourcesArgument : Argument<String, ClientOptions>() {
    override val default = null
    override val argumentName = "-"
    override val helpText =
        """
        Everything after this is treated as a source file.
        """.trimIndent()

    override fun matches(arg: String) = arg == "--"

    override fun parse(arg: String, position: Iterator<String>, opts: ClientOptions) {
        position.forEachRemaining { setOption(it, opts) }
    }

    override fun setOption(option: String, opts: ClientOptions) {
        opts.addSource(option)
    }
}

class BuildFileArgument : StringArgument<ClientOptions>() {
    override val default = null

    override val argumentName = "build-file"
    override val helpText =
        """
        Build file containing sources and classpaths to be consumed by kotlinc. See
        -Xbuild-file on kotlinc.
        """
            .trimIndent()

    override fun setOption(option: String, opts: ClientOptions) {
        opts.buildFileLocation = option
        parseBuildFile(opts.buildFile!!, opts)
    }

    private fun parseBuildFile(buildFile: File, opts: ClientOptions) {
        val parser = BuildFileParser()
        val spf = SAXParserFactory.newInstance()
        val saxParser = spf.newSAXParser()
        val xmlReader = saxParser.xmlReader
        xmlReader.contentHandler = parser
        xmlReader.parse(buildFile.absolutePath)

        opts.buildFileModuleName = parser.moduleName
        opts.buildFileClassPaths = parser.classpaths
        opts.buildFileFriendDirs = parser.friendDirs
        opts.buildFileJavaSources = parser.javaSources
        opts.buildFileSources = parser.sources
        if (parser.outputDirName != null) {
            opts.outputDirName = parser.outputDirName!!
        }
    }
}

class XBuildFileArgument : StringArgument<ClientOptions>() {
    override val default = null

    override val argumentName = "Xbuild-file"
    override val helpText = """
        Deprecated: use -build-file
        """.trimIndent()

    override fun setOption(option: String, opts: ClientOptions) {
        error = "Can not parse -Xbuild-file. Please use -build-file."
    }
}

class LogDirArgument : WritableDirectoryArgument<ClientOptions>() {
    override val argumentName = "log-dir"
    override val helpText = """
        Directory to write log output to.
        """.trimIndent()
    override val default = null

    override fun setDirectory(dir: File, opts: ClientOptions) {
        opts.logDir = dir
    }
}

class RunFilesArgument : WritableDirectoryArgument<ClientOptions>() {
    override val argumentName = "run-files-path"
    override val helpText =
        """
        Local directory to place lock files and other process-specific
        metadata.
        """
            .trimIndent()
    override val default = "/tmp"

    override fun setDirectory(dir: File, opts: ClientOptions) {
        opts.runFiles = dir
    }
}

class RootDirArgument : WritableDirectoryArgument<ClientOptions>() {
    override val argumentName = "root-dir"
    override val helpText =
        """
        Base directory for the Kotlin daemon's artifacts.
        Other directories - working-dir, output-dir, and build-dir - are all relative
        to this directory.
        This option is REQUIRED.
        """
            .trimIndent()
    override val default = null

    override fun setDirectory(dir: File, opts: ClientOptions) {
        opts.rootDir = dir
    }
}

class WorkingDirArgument : SubdirectoryArgument<ClientOptions>() {
    override val argumentName = "working-dir"
    override val helpText =
        """
        Stores intermediate steps used specifically for incremental compilation.
        Must be maintained between compilation invocations to see
        incremental speed benefits.
        Relative to root-dir.
        """
            .trimIndent()
    override val default = "work"

    override fun setSubDirectory(dir: String, opts: ClientOptions) {
        opts.workingDirName = dir
    }
}

class OutputDirArgument : SubdirectoryArgument<ClientOptions>() {
    override val argumentName = "output-dir"
    override val helpText =
        """
        Where to output compiler results.
        Relative to root-dir.
        """
            .trimIndent()
    override val default = "output"

    override fun setSubDirectory(dir: String, opts: ClientOptions) {
        opts.outputDirName = dir
    }
}

class BuildDirArgument : SubdirectoryArgument<ClientOptions>() {
    override val argumentName = "build-dir"
    override val helpText =
        """
        TODO: figure out what this is. Notes say:
        "buildDir is the parent of destDir and workingDir"
        """
            .trimIndent()
    override val default = "build"

    override fun setSubDirectory(dir: String, opts: ClientOptions) {
        opts.buildDirName = dir
    }
}

class SourceDeltaArgument : StringArgument<ClientOptions>() {
    override val argumentName = "source-delta-file"
    override val helpText =
        """
        Input file containing a list of added, modified, and deleted source files since the last
        run. Additions and modifications should be the file name preceded by a +. Deletions should
        be the file name preceded by a -. Files should be separated by white space.
    """
            .trimIndent()

    override val default = null

    override fun setOption(option: String, opts: ClientOptions) {
        opts.sourceDeltaFileName = option
    }
}

class BuildHistoryFileArgument : StringArgument<ClientOptions>() {
    override val argumentName = "build-history"
    override val helpText =
        """
        Location of the build-history file used for incremental compilation.
        """
            .trimIndent()
    override val default = "build-history"

    override fun setOption(option: String, opts: ClientOptions) {
        opts.buildHistoryFileName = option
    }
}

class ClassPathArgument : StringArgument<ClientOptions>() {
    override val argumentName = "classpath"
    override val helpText =
        """
        List of directories and JAR/ZIP archives to search for user class files.
        Colon separated: "foo.jar:bar.jar"
        """
            .trimIndent()
    override val default = null

    override fun setOption(option: String, opts: ClientOptions) {
        val paths = option.split(":").filter { !it.isBlank() }
        // TODO: validate paths?
        opts.classPath.addAll(paths)
    }
}

/**
 * Intercepts the -Xplugin argument of kotlinc such that we can prepend them to the front of the
 * classpath.
 *
 * Without this, you can run into a bug where a passed in plugin can cause a plugin implementing the
 * same package+classname to be loaded from a different part of the classpath than is intended.
 */
class PluginArgument : StringArgument<ClientOptions>() {
    override val argumentName = "Xplugin"
    override val helpText =
        """
        Compiler plugins passed to kotlin. See the `-Xplugin` argument of kotlinc.
    """
            .trimIndent()
    override val default = null

    override fun setOption(option: String, opts: ClientOptions) {
        opts.classPath.addFirst(option)
        opts.passThroughArgs.add("-Xplugin=$option")
    }
}

class JvmArgument : Argument<String, ClientOptions>() {
    override val argumentName = "-J<option>"
    override val helpText = """
        Options passed through to the JVM.
        """.trimIndent()
    override val default = null

    override fun matches(arg: String) = arg.startsWith("-J")

    override fun parse(arg: String, position: Iterator<String>, opts: ClientOptions) {
        // Strip off "-J-" so that we're left with just "<option>"
        setOption(arg.substring(3), opts)
    }

    override fun setOption(option: String, opts: ClientOptions) {
        opts.jvmArgs.add(option)
    }
}
