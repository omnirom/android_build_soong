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

package com.android.kotlin.compiler.cli

import java.io.File
import java.io.PrintStream

fun <O : Options> parseArgs(
    args: Array<String>,
    opts: O,
    argumentParsers: List<Argument<out Any, O>>,
    stdoutPrinter: PrintStream,
    stderrPrinter: PrintStream,
    usageText: String,
    additionalHelp: String? = null,
): Boolean {
    var matched: Boolean
    var hasError = false
    var showHelp = args.isEmpty()
    val iter = args.iterator()

    while (iter.hasNext()) {
        val arg = iter.next()
        matched = false
        for (parser in argumentParsers) {
            if (parser.matches(arg)) {
                matched = true
                if (parser is HelpArgument) {
                    showHelp = true
                }
                parser.parse(arg, iter, opts)
                if (parser.error != null) {
                    hasError = true
                    stderrPrinter.println(parser.error)
                    stderrPrinter.println()
                }
                break
            }
        }
        if (!matched) {
            opts.passThroughArgs.add(arg.substring(0))
        }
    }

    if (showHelp) {
        showArgumentHelp(argumentParsers, stdoutPrinter, usageText, additionalHelp)
    }

    return !hasError
}

fun <O : Options> showArgumentHelp(
    argumentParsers: List<Argument<out Any, O>>,
    printer: PrintStream,
    usageText: String,
    additionalHelp: String?,
) {
    var longest = -1
    val padding = 5

    printer.println(usageText)
    printer.println()
    for (parser in argumentParsers) {
        if (parser.argumentName.length > longest) {
            longest = parser.argumentName.length
        }
    }

    val indent = " ".repeat(longest + padding)
    for (parser in argumentParsers) {
        print(("-" + parser.argumentName).padEnd(longest + padding))
        var first = true
        parser.helpText.lines().forEach {
            if (first) {
                printer.println(it)
                first = false
            } else {
                printer.println(indent + it)
            }
        }
        if (parser.default != null) {
            printer.print(indent + "[Default: ")
            if (parser.default is String) {
                printer.println("\"${parser.default}\"]")
            } else {
                printer.println("${parser.default}]")
            }
        }
    }

    if (additionalHelp != null) {
        println()
        println(additionalHelp)
    }
}

abstract class Argument<T, O : Options> {
    abstract val argumentName: String
    abstract val helpText: String
    abstract val default: T?

    var error: String? = null
        protected set

    abstract fun matches(arg: String): Boolean

    abstract fun parse(arg: String, position: Iterator<String>, opts: O)

    abstract fun setOption(option: T, opts: O)

    fun setupDefault(opts: O) {
        if (default != null) {
            setOption(default!!, opts)
        }
    }
}

abstract class NoArgument<O : Options> : Argument<Boolean, O>() {
    override val default = null

    override fun matches(arg: String) = arg == "-$argumentName"

    override fun parse(arg: String, position: Iterator<String>, opts: O) {
        setOption(true, opts)
    }
}

abstract class SingleArgument<T, O : Options> : Argument<T, O>() {

    override fun matches(arg: String) = arg.startsWith("-$argumentName=")

    override fun parse(arg: String, position: Iterator<String>, opts: O) {
        val splits = arg.split("=", limit = 2)
        if (splits.size != 2 || splits[1].isEmpty()) {
            error = "Required argument not supplied for $argumentName"
            return
        }
        val value = stringToType(splits[1])
        setOption(value, opts)
    }

    abstract fun stringToType(arg: String): T
}

abstract class StringArgument<O : Options> : SingleArgument<String, O>() {
    override fun stringToType(arg: String): String {
        return arg
    }
}

abstract class WritableDirectoryArgument<O : Options> : StringArgument<O>() {
    override fun setOption(option: String, opts: O) {
        val e = isValidDirectoryForWriting(option)
        if (e != null) {
            error = "Invalid $argumentName option specified: $e"
        } else {
            setDirectory(File(option), opts)
        }
    }

    abstract fun setDirectory(dir: File, opts: O)
}

class HelpArgument<O : Options> : NoArgument<O>() {
    override val argumentName = "h"

    override val helpText = """
        Outputs this help text.
        """.trimIndent()

    override fun setOption(option: Boolean, opts: O) {}
}

abstract class SubdirectoryArgument<O : Options> : StringArgument<O>() {
    override fun setOption(option: String, opts: O) {
        if (option.isBlank()) {
            error = "Invalid $argumentName option specified: Must be non-empty string."
        } else if (option.contains("..")) {
            error = "Invalid $argumentName option specified: No path traversal allowed."
        } else {
            setSubDirectory(option, opts)
        }
    }

    abstract fun setSubDirectory(dir: String, opts: O)
}

fun isValidDirectoryForWriting(filePath: String): String? {
    try {
        val file = File(filePath)
        if (file.exists()) {
            if (!file.isDirectory) {
                return "Path exists but is not a directory"
            }
            if (!file.canWrite()) {
                return "Directory exists but is not writable"
            }
        } else if (!file.mkdirs()) {
            return "Unable to create directory"
        }

        return null // All checks passed!
    } catch (e: Exception) {
        // Handle exceptions like invalid path characters, no permissions, etc.
        return e.message
    }
}

fun isValidFilePathForWriting(filePath: String): String? {
    if (filePath.isBlank()) {
        return "Empty log-file path"
    }

    try {
        val file = File(filePath)
        val parentDir = file.parentFile ?: return "Invalid parent directory"

        if (!parentDir.exists()) {
            if (!parentDir.mkdirs()) {
                return "Unable to create parent directory"
            }
        } else if (!parentDir.isDirectory) {
            return "Parent directory is not a directory"
        } else if (!parentDir.canWrite()) {
            return "Parent directory is not writable"
        }

        if (file.exists()) {
            if (file.isDirectory) {
                return "File is a directory"
            } else if (!file.canWrite()) {
                return "File exists but is not writable"
            }
        }

        return null // All checks passed!
    } catch (e: Exception) {
        // Handle exceptions like invalid path characters, no permissions, etc.
        return e.message
    }
}
