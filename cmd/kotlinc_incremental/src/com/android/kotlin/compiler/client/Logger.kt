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

import org.jetbrains.kotlin.buildtools.api.KotlinLogger

class Logger(private val verbose: Boolean, override val isDebugEnabled: Boolean) : KotlinLogger {
    override fun debug(msg: String) {
        if (isDebugEnabled) {
            println("DEBUG: " + msg)
        }
    }

    override fun error(msg: String, throwable: Throwable?) {
        println("ERROR: " + msg)
        if (throwable != null) {
            println(throwable)
        }
    }

    override fun info(msg: String) {
        if (verbose) {
            println("INFO: " + msg)
        }
    }

    override fun lifecycle(msg: String) {
        println("LIFECYCLE: " + msg)
    }

    override fun warn(msg: String, throwable: Throwable?) {
        println("WARN: " + msg)
        if (throwable != null) {
            println(throwable)
        }
    }
}
