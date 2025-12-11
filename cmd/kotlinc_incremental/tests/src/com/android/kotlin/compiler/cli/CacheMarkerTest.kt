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
import java.nio.file.attribute.PosixFilePermission
import kotlin.io.path.setPosixFilePermissions
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class CacheMarkerTest {

    private lateinit var cacheMarker: CacheMarker

    @get:Rule val tFolder = TemporaryFolder()

    @Before
    fun setup() {
        cacheMarker = CacheMarker(tFolder.newFolder())
    }

    @Test
    fun testInvalid() {
        assertThat(cacheMarker.isValid()).isFalse()
    }

    @Test
    fun testValidAfterWrite() {
        assertThat(cacheMarker.write()).isTrue()
        assertThat(cacheMarker.isValid()).isTrue()
    }

    @Test
    fun testInvalidAfterRemove() {
        assertThat(cacheMarker.write()).isTrue()
        assertThat(cacheMarker.remove()).isTrue()
        assertThat(cacheMarker.isValid()).isFalse()
    }

    @Test
    fun testUnableToWrite() {
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

        val cacheMarker = CacheMarker(readOnlyDirectory)
        assertThat(cacheMarker.write()).isFalse()
    }

    @Test
    fun testUnableToRemove() {
        // Start writable, but then remove the permissions
        val readOnlyDirectory = tFolder.newFolder("read-only")
        val cacheMarker = CacheMarker(readOnlyDirectory)
        assertThat(cacheMarker.write()).isTrue()
        assertThat(cacheMarker.isValid()).isTrue()
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
        assertThat(cacheMarker.remove()).isFalse()
    }
}
