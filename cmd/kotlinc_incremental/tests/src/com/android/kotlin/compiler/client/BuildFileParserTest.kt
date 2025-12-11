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
import javax.xml.parsers.SAXParserFactory
import org.junit.Before
import org.junit.Test
import org.xml.sax.XMLReader

class BuildFileParserTest {
    private lateinit var parser: BuildFileParser
    private lateinit var xmlReader: XMLReader
    val buildFile = File("tests/resources/test_build.xml")

    @Before
    fun setup() {
        parser = BuildFileParser()
        val spf = SAXParserFactory.newInstance()
        val saxParser = spf.newSAXParser()
        xmlReader = saxParser.xmlReader
        xmlReader.contentHandler = parser

        xmlReader.parse(buildFile.absolutePath)
    }

    @Test
    fun testParseClasspaths() {
        assertThat(parser.classpaths).containsExactly("a.jar", "b.jar")
    }

    @Test
    fun testParseJavaSources() {
        assertThat(parser.javaSources).containsExactly("c.java", "d.java")
    }

    @Test
    fun testParseSources() {
        assertThat(parser.sources).containsExactly("e.kt", "f.kt")
    }

    @Test
    fun testParserModuleName() {
        assertThat(parser.moduleName).isEqualTo("test_module")
    }

    @Test
    fun testParserOutputDirName() {
        assertThat(parser.outputDirName).isEqualTo("output")
    }
}
