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

import org.xml.sax.Attributes
import org.xml.sax.helpers.DefaultHandler

class BuildFileParser : DefaultHandler() {
    val classpaths: List<String>
        get() = _classpaths

    val friendDirs: List<String>
        get() = _friendDirs

    val javaSources: List<String>
        get() = _javaSources

    val moduleName: String?
        get() = _moduleName

    val sources: List<String>
        get() = _sources

    val outputDirName: String?
        get() = _outputDirName

    private val _classpaths = mutableListOf<String>()
    private val _friendDirs = mutableListOf<String>()
    private val _javaSources = mutableListOf<String>()
    private var _moduleName: String? = null
    private val _sources = mutableListOf<String>()
    private var _outputDirName: String? = null

    override fun startElement(
        uri: String?,
        localName: String?,
        qName: String?,
        attributes: Attributes?,
    ) {
        when (qName) {
            "classpath" -> parseClassPath(attributes)
            "friendDir" -> parseFriendDir(attributes)
            "javaSourceRoots" -> parseJavaSourceRoots(attributes)
            "module" -> parseModule(attributes)
            "sources" -> parseSources(attributes)
        }
    }

    private fun parseClassPath(attributes: Attributes?) {
        if (attributes == null) {
            return
        }

        val cp = attributes.getValue("", "path")
        if (cp == null) {
            return
        }
        _classpaths.add(cp)
    }

    private fun parseFriendDir(attributes: Attributes?) {
        if (attributes == null) {
            return
        }

        val path = attributes.getValue("", "path")
        if (path == null) {
            return
        }
        _friendDirs.add(path)
    }

    private fun parseSources(attributes: Attributes?) {
        if (attributes == null) {
            return
        }

        val path = attributes.getValue("", "path")
        if (path == null) {
            return
        }

        _sources.add(path)
    }

    private fun parseJavaSourceRoots(attributes: Attributes?) {
        if (attributes == null) {
            return
        }

        val path = attributes.getValue("", "path")
        if (path == null) {
            return
        }

        _javaSources.add(path)
    }

    private fun parseModule(attributes: Attributes?) {
        if (attributes == null) {
            return
        }

        _moduleName = attributes.getValue("", "name")
        _outputDirName = attributes.getValue("", "outputDir")
    }
}
