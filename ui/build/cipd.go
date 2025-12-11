// Copyright 2025 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	cipdProxyPolicyPath = "build/soong/ui/build/cipd_proxy_policy.txtpb"
	cipdProxyUrlKey     = "CIPD_PROXY_URL"
)

type cipdProxy struct {
	cmd      *Cmd
	wg       sync.WaitGroup
	stopping atomic.Bool
	unixsock string
}

func cipdPath(config Config) string {
	return filepath.Join("prebuilts/cipd", config.HostPrebuiltTag(), "cipd")
}

func shouldRunCIPDProxy(config Config) bool {
	if runtime.GOOS == "darwin" {
		// Disable CIPD proxy on Mac until we have it working, see b/425932171.
		return false
	}

	cipdPath := cipdPath(config)
	_, err := os.Stat(cipdPath)
	return err == nil
}

func startCIPDProxyServer(ctx Context, config Config) *cipdProxy {
	ctx.Status.Status("Starting CIPD proxy server...")

	unixsock := absPath(ctx, filepath.Join(config.SoongOutDir(), "cipd_proxy.sock"))
	if _, err := os.Stat(unixsock); err == nil {
		ctx.Verbosef("%s was not cleaned up from the last build, deleting it.", unixsock)
		if err := os.Remove(unixsock); err != nil {
			log.Fatal(err)
		}
	}

	cipdArgs := []string{
		"proxy", "-proxy-policy", cipdProxyPolicyPath,
		"-log-level", "warning",
		"-unix-socket", unixsock,
	}
	adcFlagAdded := false

	// Determine RBE authentication mechanism and propagate to CIPD flags.
	// Some build configurations like ABFS may disable RBE for compilation while
	// still relying on RBE auth config being present.
	authType, authValue := config.rbeAuth()
	switch authType {
	case "RBE_credential_file":
		cipdArgs = append(cipdArgs, "-service-account-json", authValue)
	case "RBE_credentials_helper", "RBE_use_google_prod_creds":
		helperPath := filepath.Join(config.rbeDir(), "credshelper")

		var credHelperArgsParts []string
		// RBE_credentials_helper_args contains space-separated arguments for the helper
		// and need to be formatted as repeated 'args:"..."' for the -credential-helper spec.
		// e.g. "--f=foo --b=bar" -> 'args:"--f=foo" args:"--b=bar"'.
		if rbeArgsStr, ok := config.environ.Get("RBE_credentials_helper_args"); ok && rbeArgsStr != "" {
			argList := strings.Fields(rbeArgsStr)
			for _, arg := range argList {
				credHelperArgsParts = append(credHelperArgsParts, fmt.Sprintf("args:%q", arg))
			}
		} else {
			credHelperArgsParts = append(credHelperArgsParts, fmt.Sprintf("args:%q", "--auth_source=automaticAuth"))
			credHelperArgsParts = append(credHelperArgsParts, fmt.Sprintf("args:%q", "--gcert_refresh_timeout=20"))
		}
		helperSpec := fmt.Sprintf("protocol:RECLIENT exec:'%s' %s", helperPath, strings.Join(credHelperArgsParts, " "))
		cipdArgs = append(cipdArgs, "-credential-helper", helperSpec)
	case "RBE_use_application_default_credentials", "RBE_use_gce_credentials":
		fallthrough
	default:
		cipdArgs = append(cipdArgs, "-application-default-credentials=always")
		adcFlagAdded = true
	}

	if !adcFlagAdded {
		// RBE instructions for non-corp machines set both RBE_credentials_helper and
		// RBE_use_application_default_credentials. Pass that along to CIPD as well.
		// Even if USE_RBE=false, CIPD can still use ADC.
		if useAdcStr, ok := config.environ.Get("RBE_use_application_default_credentials"); ok {
			parsedVal, err := strconv.ParseBool(useAdcStr)
			if err == nil && parsedVal {
				cipdArgs = append(cipdArgs, "-application-default-credentials=always")
			}
		}
	}

	cipdCmd := fmt.Sprintf("cipd %s", strings.Join(cipdArgs, " "))
	ctx.Verbosef("Starting CIPD proxy server with: %s", cipdCmd)

	cmd := Command(ctx, config, nil, "cipd", cipdPath(config), cipdArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	cp := cipdProxy{cmd: cmd, unixsock: unixsock}
	cp.wg.Add(1)
	go func() {
		// Log any error output from cipd until it exits.
		defer cp.wg.Done()

		bufReader := bufio.NewReader(stderr)
		for {
			l, err := bufReader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					if !cp.stopping.Load() {
						err := cmd.Wait()
						ctx.Printf("cipd: unexpected EOF, process exited with %v", err)
					}
					break
				}
				ctx.Fatalf("cipd: %v %v", l, err)
			}
			ctx.Printf("cipd: %v", l)
		}
	}()

	bufReader := bufio.NewReader(stdout)
	for {
		l, err := bufReader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			ctx.Printf("cipd: unexpected EOF: %v\n", l)
			// The stderr goroutine will handle the EOF
			cp.wg.Wait()
		}

		if err != nil {
			log.Fatalf("Got %v reading from cipd process", err)
		}
		if strings.HasPrefix(l, cipdProxyUrlKey) {
			proxyUrl := strings.TrimSpace(l[len(cipdProxyUrlKey)+1:])
			if proxyUrl != "unix://"+unixsock {
				log.Fatalf("unexpected unix socket returned by cipd proxy: %s, expected unix://%s", proxyUrl, unixsock)
			}
			config.environ.Set(cipdProxyUrlKey, proxyUrl)
			ctx.Verbosef("Started CIPD proxy listening on", proxyUrl)
			break
		}
	}
	return &cp
}

func (c *cipdProxy) Stop(ctx Context) {
	c.stopping.Store(true)
	c.cmd.Process.Kill()
	c.wg.Wait()
	if err := os.Remove(c.unixsock); err != nil {
		ctx.Printf("failed to clean up cipd proxy socket %s.\n", c.unixsock)
	}
}
