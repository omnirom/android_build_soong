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

import com.google.common.truth.Truth.assertThat
import java.io.ByteArrayOutputStream
import java.io.PrintStream
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class ArgumentTest {

    class TestOpts : Options {
        override val passThroughArgs = mutableListOf<String>()
    }

    private val opts = TestOpts()
    private val stdoutStreamCaptor = ByteArrayOutputStream()
    private val stderrStreamCaptor = ByteArrayOutputStream()

    @get:Rule val tFolder = TemporaryFolder()

    @Before fun setup() {}

    @Test
    fun testParseArgs_NoError() {
        val argParsers = listOf(HelpArgument<TestOpts>())
        val args = arrayOf("-h", "foo", "bar", "baz")

        val result =
            parseArgs(
                args,
                opts,
                argParsers,
                PrintStream(stdoutStreamCaptor),
                PrintStream(stderrStreamCaptor),
                "usage",
            )

        assertThat(result).isTrue()
        assertThat(opts.passThroughArgs).isEqualTo(listOf("foo", "bar", "baz"))
        assertThat(stdoutStreamCaptor.toString()).startsWith("usage\n")
        assertThat(stderrStreamCaptor.toString()).isEmpty()
    }

    @Test
    fun testParseArgs_Error() {
        class ErrorArg : NoArgument<TestOpts>() {
            override val argumentName = "error"
            override val helpText = "This argument forces an error"

            override fun setOption(option: Boolean, opts: TestOpts) {
                error = "A forced error"
            }
        }

        val argParsers = listOf(ErrorArg())
        val args = arrayOf("-error", "foo", "bar", "baz")

        val result =
            parseArgs(
                args,
                opts,
                argParsers,
                PrintStream(stdoutStreamCaptor),
                PrintStream(stderrStreamCaptor),
                "usage",
            )

        assertThat(result).isFalse()
        assertThat(stdoutStreamCaptor.toString()).isEmpty()
        assertThat(stderrStreamCaptor.toString()).isEqualTo(argParsers.get(0).error + "\n\n")
    }

    @Test
    fun testHelpArgument() {
        val ha = HelpArgument<TestOpts>()
        assertThat(ha.matches("-h")).isTrue()
        val args = listOf("foo").iterator()
        ha.parse("-h", args, opts)
        assertThat(args.hasNext()).isTrue()
    }
}
