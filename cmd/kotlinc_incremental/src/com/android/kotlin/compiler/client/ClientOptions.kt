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

import com.android.kotlin.compiler.cli.Options
import java.io.File

class ClientOptions : Options {
    var verbose = false
    var debug = false

    val classPath = mutableListOf<String>()
    override val passThroughArgs = mutableListOf<String>()
    val jvmArgs = mutableListOf<String>()
    val _sources = mutableListOf<String>()
    val sources
        get() = _sources

    fun addSource(arg: String) {
        _sources.add(arg)
    }

    private var _rootDir: File? = null
    var rootDir: File
        get() {
            return _rootDir ?: throw IllegalStateException("Can not read rootDir before it is set")
        }
        set(value) {
            _rootDir = value
        }

    var rootDirLocation: String
        get() {
            return _rootDir?.absolutePath
                ?: throw IllegalStateException("Can not read rootDirLocation before it is set")
        }
        set(value) {
            if (value == "") {
                _rootDir = null
            } else {
                _rootDir = File(value)
            }
        }

    var buildDirName: String = "build"
    val buildDir: File
        get() {
            if (_rootDir == null) {
                throw IllegalStateException("Can not read buildDir before rootDir is set")
            }
            if (buildDirName.isEmpty()) {
                throw IllegalStateException("buildDirName may not be empty.")
            }
            return File(rootDir, buildDirName)
        }

    var outputDirName: String = "output"
    val outputDir: File
        get() {
            if (_rootDir == null) {
                throw IllegalStateException("Can not read outputDir before rootDir is set")
            }
            if (outputDirName.isEmpty()) {
                throw IllegalStateException("outputDirName may not be empty.")
            }
            return File(rootDir, outputDirName)
        }

    var workingDirName: String = "work"
    val workingDir: File
        get() {
            if (_rootDir == null) {
                throw IllegalStateException("Can not read workingDir before rootDir is set")
            }
            if (workingDirName.isEmpty()) {
                throw IllegalStateException("workingDirName may not be empty.")
            }
            return File(rootDir, workingDirName)
        }

    var sourceDeltaFileName: String? = null
    val sourceDeltaFile: File?
        get() = if (sourceDeltaFileName != null) File(sourceDeltaFileName!!) else null

    var buildHistoryFileName: String = "build-history"
    val buildHistory: File
        get() {
            if (_rootDir == null) {
                throw IllegalStateException("Can not read buildHistory before rootDir is set")
            }
            if (buildHistoryFileName.isEmpty()) {
                throw IllegalStateException("buildHistoryFileName may not be empty.")
            }
            return File(rootDir, buildHistoryFileName)
        }

    private var _logDir: File? = null
    var logDir: File
        get() {
            return _logDir ?: throw IllegalStateException("Can not read logDir before it is set")
        }
        set(value) {
            _logDir = value
        }

    var logDirLocation: String
        get() {
            return _logDir?.absolutePath
                ?: throw IllegalStateException("Can not read logDirLocation before it is set")
        }
        set(value) {
            if (value == "") {
                _logDir = null
            } else {
                _logDir = File(value)
            }
        }

    private var _runFiles: File? = null
    var runFiles: File
        get() {
            return _runFiles
                ?: throw IllegalStateException("Can not read runFiles before it is set")
        }
        set(value) {
            _runFiles = value
        }

    var runFilesLocation: String
        get() {
            return _runFiles?.absolutePath
                ?: throw IllegalStateException("Can not read runFilesLocation before it is set")
        }
        set(value) {
            if (value == "") {
                _runFiles = null
            } else {
                _runFiles = File(value)
            }
        }

    private var _buildFile: File? = null
    var buildFile: File?
        get() = _buildFile
        set(value) {
            _buildFile = value
        }

    var buildFileLocation: String?
        get() {
            return _buildFile?.absolutePath
                ?: throw IllegalStateException("Can not read buildFileLocation before it is set")
        }
        set(value) {
            if (value == "" || value == null) {
                _buildFile = null
            } else {
                _buildFile = File(value)
            }
        }

    var buildFileModuleName: String? = null

    var buildFileClassPaths: List<String> = emptyList()

    var buildFileFriendDirs: List<String> = emptyList()

    var buildFileJavaSources: List<String> = emptyList()

    var buildFileSources: List<String> = emptyList()

    private var classpathSnapshotDir: String = "cpsnapshot"
    val classpathSnapshot: File
        get() {
            if (_rootDir == null) {
                throw IllegalStateException("Can not read classpathSnapshot before rootDir is set")
            }
            if (classpathSnapshotDir.isEmpty()) {
                throw IllegalStateException("classpathSnapshotDir may not be empty.")
            }
            return File(rootDir, classpathSnapshotDir)
        }
}
