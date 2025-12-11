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

package main

import (
	"cmp"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	rc_lib "android/soong/cmd/release_config/release_config_lib"
	rc_proto "android/soong/cmd/release_config/release_config_proto"

	"google.golang.org/protobuf/proto"
)

type Flags struct {
	// The top of the workspace
	Top string

	// The source directories in which to permit writes
	RwDirs rc_lib.StringList

	// The number of concurrent runs of product config
	NumJob int

	// The products to use.
	Products []string

	// The products to skip, presumably because they are
	// temporarily allowed to be broken.
	SkipProducts rc_lib.StringList

	// Whether to copy artifacts to ${DIST_DIR}.
	Dist bool

	// Working directory to use for intermediates.
	WorkDir string

	// OUT_DIR (from environment)
	outDir string

	// DIST_DIR (from environment)
	distDir string

	// Formats for artifacts
	Textproto bool
	Json      bool
}

type ProductMaps struct {
	Product  string
	MakeVars map[string]string
}

type MapsData struct {
	Products    []string
	ArtifactTag string
}

// Map of PRODUCT_RELEASE_CONFIG_MAPS to Products and the TARGET_PRODUCT used
// to produce the release config.
type MapsInfo map[string]*MapsData

type RcGenReq struct {
	Product     string
	Maps        string
	ArtifactTag string
}

type RcGenResp struct {
	req                      *RcGenReq
	AllReleaseConfigsPathMap map[string]string
	InheritanceGraphPath     string
}

type ArtifactInfo struct {
	// The products represented.
	Products []string `json:"products"`
	// The value of ProductReleaseConfigMaps
	ProductReleaseConfigMaps string `json:"product_release_config_maps"`
	// All release configs for these products.
	// Key: variant Value: all_release_configs.pb
	AllReleaseConfigsMap map[string]string `json:"all_release_configs_map"`
	// Inheritance graph for these products.
	InheritanceGraphPath string `json:"inheritance_graph"`
}

type ReleaseConfigInfo struct {
	// Key: product name Value:key for artifact map
	ProductMap map[string]string `json:"product_map"`
	// Key: one product name  Value: Release Config artifacts.
	ArtifactMap map[string]ArtifactInfo `json:"artifact_map"`
}

func main() {
	start := time.Now()
	defer func() {
		fmt.Printf("Runtime: %v\n", time.Now().Sub(start))
	}()

	flags := Flags{
		Top:    ".",
		NumJob: max(2, runtime.NumCPU()-2),
	}
	if os.Getenv("TARGET_PRODUCT") == "" {
		os.Setenv("TARGET_PRODUCT", "aosp_cf_x86_64_only_phone")
	}
	if os.Getenv("TARGET_RELEASE") == "" {
		os.Setenv("TARGET_RELEASE", "trunk_staging")
	}
	if os.Getenv("TARGET_BUILD_VARIANT") == "" {
		os.Setenv("TARGET_BUILD_VARIANT", "eng")
	}

	var rwDirsFile string
	var skipProductsFile string
	flag.StringVar(&flags.Top, "top", flags.Top, "path to top of workspace")
	flag.IntVar(&flags.NumJob, "j", flags.NumJob, "number of concurrent threads to use")
	flag.Var(&flags.RwDirs, "rw-dir", "path to be read-write during the build")
	flag.StringVar(&rwDirsFile, "rw-dirs-file", "", "path to a file containing a list of rw-dirs")
	flag.BoolVar(&flags.Dist, "dist", false, "Whether to copy arifacts to ${DIST_DIR}")
	flag.Var(&flags.SkipProducts, "skip", "product to be ignored when creating product list")
	flag.StringVar(&skipProductsFile, "skip-file", "", "path to a file containing a list of products to skip")
	flag.StringVar(&flags.WorkDir, "intermediate-dir", "", "path to a directory for intermediates")
	flag.BoolVar(&flags.Textproto, "textproto", false, "Whether to also generate textproto artifacts")
	flag.BoolVar(&flags.Json, "json", false, "Whether to also generate json artifacts")

	flag.Parse()
	flags.Products = flag.Args()

	if skipProductsFile != "" {
		if len(flags.SkipProducts) > 0 {
			log.Fatal(fmt.Errorf("Cannot specify both --skip and --skip-file"))
		} else if err := flags.SkipProducts.ReadFromFile(skipProductsFile); err != nil {
			log.Fatal(err)
		}
	}

	if rwDirsFile != "" {
		if len(flags.RwDirs) > 0 {
			log.Fatal(fmt.Errorf("Cannot specify both --rw-dir and --rw-dirs-file"))
		} else if err := flags.RwDirs.ReadFromFile(rwDirsFile); err != nil {
			log.Fatal(err)
		}
	}

	if err := os.Chdir(flags.Top); err != nil {
		log.Fatal(fmt.Errorf("Failed to chdir to %s", flags.Top))
	}
	out, err := exec.Command("pwd", "-P").Output()
	if err != nil {
		log.Fatal(err)
	}
	flags.Top = strings.TrimSpace(string(out))

	// Get the abspath of all of the RwDirs.
	for idx, v := range flags.RwDirs {
		if v[0] != filepath.Separator {
			v = filepath.Join(flags.Top, v)
		}
		flags.RwDirs[idx] = filepath.Clean(v)
	}

	// make sure that OUT_DIR and DIST_DIR are expanded and set.
	flags.outDir = ExpandEnvWithDefault("OUT_DIR", "out")
	flags.distDir = ExpandEnvWithDefault("DIST_DIR", "${OUT_DIR}/dist")

	os.Setenv("SOONG_SRC_DIR_IS_READ_ONLY", "true")
	os.Setenv("ANDROID_QUIET_BUILD", "true")
	os.Setenv("BUILD_BROKEN_SRC_DIR_IS_WRITABLE", "false")
	os.Setenv("BUILD_BROKEN_SRC_DIR_RW_ALLOWLIST", strings.Join(flags.RwDirs, " "))

	if len(flags.Products) == 0 {
		data, err := CommandOutput("list_products", nil, nil)
		if err != nil {
			log.Fatal(err)
		}
		flags.Products = strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	var errSummary error
	var errWg sync.WaitGroup
	errCh := make(chan error, 40)
	errWg.Add(1)
	go func() {
		defer errWg.Done()
		var errs []error
		for e := range errCh {
			errs = append(errs, e)
		}
		if len(errs) > 1 {
			errs = append(errs, fmt.Errorf("Total errors: %d", len(errs)))
		}
		errSummary = errors.Join(errs...)
	}()

	mapsInfo := GenerateProductConfigs(flags, errCh)
	GenerateReleaseConfigs(flags, mapsInfo, errCh)

	close(errCh)
	errWg.Wait()
	if errSummary != nil {
		fmt.Printf("Runtime: %v\n", time.Now().Sub(start))
		log.Fatal(errSummary)
	}
}

// Clean all of the paths, and drop duplicates while preserving order.
func cleanMaps(s string) string {
	used := make(map[string]bool)
	maps := []string{}
	for v := range strings.SplitSeq(s, " ") {
		v = filepath.Clean(v)
		if v != "." && !used[v] {
			used[v] = true
			maps = append(maps, v)
		}
	}
	return strings.Join(maps, " ")
}

func ExpandEnvWithDefault(name, defVal string) string {
	orig := os.Getenv(name)
	if orig == "" && defVal != "" {
		os.Setenv(name, defVal)
	}
	s := fmt.Sprintf("${%s}", name)
	var prior string
	for prior != s {
		prior = s
		s = os.ExpandEnv(s)
	}
	if s != orig {
		os.Setenv(name, s)
	}
	return s
}

func CommandRun(bin string, args, env []string) (err error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(cmd.Environ(), env...)
	err = cmd.Run()
	if exitErr, _ := err.(*exec.ExitError); exitErr != nil {
		return fmt.Errorf("failed to run %s\n%s", cmd, string(exitErr.Stderr))
	}
	return
}

func CommandOutput(bin string, args, env []string) (ret []byte, err error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(cmd.Environ(), env...)
	ret, err = cmd.Output()
	if exitErr, _ := err.(*exec.ExitError); exitErr != nil {
		err = fmt.Errorf("failed to run %s\n%s", cmd.Args, string(exitErr.Stderr))
	}
	return
}

// Compute the hash of a string.
func hashString(s string) string {
	h := fnv.New128()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum([]byte{}))
}

func DumpVars(varNames, env []string) (ret map[string]string, err error) {
	ret = make(map[string]string)
	args := []string{
		"--dumpvars-mode",
		fmt.Sprintf("--vars=%s", strings.Join(varNames, " ")),
	}
	var out []byte
	out, err = CommandOutput("build/soong/soong_ui.bash", args, env)
	if err != nil {
		return
	}
	for _, e := range strings.Split(string(out), "\n") {
		if len(e) == 0 {
			continue
		}
		if k, v, ok := strings.Cut(e, "="); ok {
			if strings.HasPrefix(v, "'") {
				v = v[1 : len(v)-1]
			}
			ret[k] = v
		} else {
			err = fmt.Errorf("Ignoring bad variable from dumpvars: %s", e)
		}
	}
	return
}

func artifactPath(base, product, suffix string, dirs ...string) string {
	ret := filepath.Join(dirs...)
	return filepath.Join(ret, fmt.Sprintf("%s%s%s", base, product, suffix))
}

func GenerateProductConfigs(flags Flags, errCh chan error) (mapsInfo MapsInfo) {
	mapsResults := make(map[string]*ProductMaps)
	mapsInfo = make(MapsInfo)
	mapsCh := make(chan *ProductMaps, flags.NumJob)
	var mapsWg sync.WaitGroup
	mapsWg.Add(1)
	// Gather the products by their value of PRODUCT_RELEASE_CONFIG_MAPS.
	go func() {
		defer mapsWg.Done()
		var count int
		for info := range mapsCh {
			count += 1
			mapsResults[info.Product] = info
			prodMaps := cleanMaps(info.MakeVars["PRODUCT_RELEASE_CONFIG_MAPS"])
			if data, ok := mapsInfo[prodMaps]; !ok {
				mapsInfo[prodMaps] = &MapsData{
					Products:    []string{info.Product},
					ArtifactTag: hashString(prodMaps),
				}
			} else {
				data.Products = append(data.Products, info.Product)
			}
		}
		log.Printf("Fetched PRODUCT_RELEASE_CONFIG_MAPS for %d products\n", count)
	}()

	// Run product config for every flags.Products.
	batchCh := make(chan string, min(len(flags.Products), flags.NumJob*2))
	var batchWg sync.WaitGroup
	batchHandler := func() {
		defer batchWg.Done()
		for product := range batchCh {
			tmpDir := filepath.Join(flags.WorkDir, ".temp", product)
			os.MkdirAll(tmpDir, 0755)
			if len(product) == 0 {
				log.Fatal("received empty product")
			}
			env := []string{
				fmt.Sprintf("TARGET_PRODUCT=%s", product),
				fmt.Sprintf("TMPDIR=%s", tmpDir),
				"WRITE_SOONG_VARIABLES=true",
				"_SOONG_INTERNAL_NO_FINDER=true",
				fmt.Sprintf("OUT_DIR=%s", flags.outDir),
				fmt.Sprintf("SOONG_METRICS_SUFFIX=-%s", product),
			}
			makeVarNames := []string{
				"PRODUCT_RELEASE_CONFIG_MAPS",
				"PLATFORM_RELEASE_VERSION",
			}
			if vars, err := DumpVars(makeVarNames, env); err != nil {
				errCh <- fmt.Errorf("%s: %v", product, err)
			} else {
				prodMaps := &ProductMaps{
					Product:  product,
					MakeVars: vars,
				}
				mapsCh <- prodMaps
			}
			if err := os.RemoveAll(tmpDir); err != nil {
				errCh <- fmt.Errorf("failed to remove %s: %v", tmpDir, err)
			}
		}
	}
	for i := 0; i < flags.NumJob; i++ {
		batchWg.Add(1)
		go batchHandler()
	}

	skipMap := make(map[string]bool)
	for _, p := range flags.SkipProducts {
		if len(p) > 0 {
			skipMap[p] = true
		}
	}

	log.Printf("Starting product config: %d products, %d in skip list\n", len(flags.Products), len(skipMap))
	var skipped, started int
	for _, p := range flags.Products {
		if skipMap[p] {
			skipped += 1
			log.Printf("Skipping %s\n", p)
		} else {
			started += 1
			batchCh <- p
		}
	}
	log.Printf("Started product config on %d products, skipped %d\n", started, skipped)
	close(batchCh)
	batchWg.Wait()
	close(mapsCh)
	mapsWg.Wait()
	log.Printf("%d distinct values for PRODUCT_RELEASE_CONFIG_MAPS\n", len(mapsInfo))
	return
}

func GenerateReleaseConfigs(flags Flags, mapsInfo MapsInfo, errCh chan error) {
	artifactsDir := filepath.Join(flags.WorkDir, "all-release-configs")
	releaseConfigInfo := ReleaseConfigInfo{
		ProductMap:  make(map[string]string),
		ArtifactMap: make(map[string]ArtifactInfo),
	}
	rcGenJobs := min(flags.NumJob, len(mapsInfo))
	log.Printf("Using %d threads for %d runs of release-config\n", rcGenJobs, len(mapsInfo))
	rcGenCh := make(chan *RcGenReq, rcGenJobs)
	var rcGenWg sync.WaitGroup
	rcRespCh := make(chan *RcGenResp, rcGenJobs)
	var rcRespWg sync.WaitGroup
	rcRespWg.Add(1)
	go func() {
		defer rcRespWg.Done()
		for resp := range rcRespCh {
			k := resp.req.Product
			tag := resp.req.ArtifactTag
			info := ArtifactInfo{
				Products:                 mapsInfo[resp.req.Maps].Products,
				ProductReleaseConfigMaps: resp.req.Maps,
				AllReleaseConfigsMap:     resp.AllReleaseConfigsPathMap,
				InheritanceGraphPath:     resp.InheritanceGraphPath,
			}
			if m, ok := releaseConfigInfo.ArtifactMap[tag]; !ok {
				releaseConfigInfo.ArtifactMap[tag] = info
			} else {
				log.Fatalf("Tag %s already exists: %s and %s\n", tag, m.ProductReleaseConfigMaps, info.ProductReleaseConfigMaps)
			}
			for _, p := range info.Products {
				if m, ok := releaseConfigInfo.ProductMap[p]; !ok {
					releaseConfigInfo.ProductMap[p] = tag
				} else {
					log.Fatalf("%s has two mappings: %s and %s\n", p, k, m)
				}
			}
		}
	}()

	rcGenHandler := func() {
		defer rcGenWg.Done()
		for req := range rcGenCh {
			var err error
			product := req.Product
			tag := req.ArtifactTag
			env := []string{
				fmt.Sprintf("PRODUCT_RELEASE_CONFIG_MAPS=%s", req.Maps),
			}
			resp := &RcGenResp{
				req:                      req,
				AllReleaseConfigsPathMap: make(map[string]string),
			}
			commonArgs := []string{
				"--product", product,
				//"--quiet",
				"--pb=true",
				fmt.Sprintf("--textproto=%v", flags.Textproto),
				fmt.Sprintf("--json=%v", flags.Json),
			}
			// Run it for each of the build variants, since flag values change for
			// any `workflow: MANUAL_BUILD_VARIANT` flags.
			for idx, variant := range []string{"user", "userdebug", "eng"} {
				variantDir := filepath.Join(artifactsDir, variant)
				os.MkdirAll(variantDir, 0755)
				args := append(commonArgs,
					"--variant", variant,
					"--out_dir", variantDir,
				)
				if idx == 0 {
					// Only generate the inheritance graph once.
					args = append(args, "--inheritance=true")
				}
				if err = CommandRun("out/release-config", args, env); err != nil {
					errCh <- fmt.Errorf("release-config %s failed with env=%s: %v", strings.Join(args, " "), strings.Join(env, " "), err)
					break
				}
				if flags.Dist {
					var dst string
					// Always do ".pb" last, since we put that in the path map.
					for _, suff := range []string{".json", ".textproto", ".pb"} {
						switch suff {
						case ".json":
							if !flags.Json {
								continue
							}
						case ".textproto":
							if !flags.Textproto {
								continue
							}
						default:
						}
						dst = artifactPath(variant+"-all_release_configs", "", suff, flags.distDir, "release_configs", tag)
						err = DistArtifact(
							artifactPath("all_release_configs-", product, suff, variantDir),
							dst)
						if err != nil {
							errCh <- err
							continue
						}
					}
					// The path is relative to the directory where we write release_config_info.
					resp.AllReleaseConfigsPathMap[variant] = artifactPath(variant+"-all_release_configs", "", ".pb", tag)

					if idx == 0 {
						// The inheritance graph does not change between variants.
						// Only dist it once.
						dst := artifactPath("inheritance_graph", "", ".dot", flags.distDir, "release_configs", tag)
						err = DistArtifact(
							artifactPath("inheritance_graph-", product, ".dot", variantDir),
							dst)
						if err != nil {
							errCh <- err
							continue
						}
						// The path is relative to the directory where we write release_config_info.
						resp.InheritanceGraphPath = artifactPath("inheritance_graph", "", ".dot", tag)
					}
				}
			}
			if err == nil {
				rcRespCh <- resp
			}
		}
	}
	for i := 0; i < rcGenJobs; i++ {
		rcGenWg.Add(1)
		go rcGenHandler()
	}

	// Run release-config for each set of product maps.
	for maps, data := range mapsInfo {
		rcGenCh <- &RcGenReq{
			Product:     data.Products[0],
			Maps:        maps,
			ArtifactTag: data.ArtifactTag,
		}
	}
	close(rcGenCh)
	rcGenWg.Wait()
	close(rcRespCh)
	rcRespWg.Wait()

	prci := &rc_proto.ProductReleaseConfigsInfo{
		ProductToTagMap:  make(map[string]string),
		PsMapsToTagMap:   make(map[string]string),
		TagToArtifactMap: make(map[string]*rc_proto.ProductReleaseConfig),
	}
	for k, v := range releaseConfigInfo.ProductMap {
		prci.ProductToTagMap[k] = v
	}
	for k, v := range releaseConfigInfo.ArtifactMap {
		prci.PsMapsToTagMap[v.ProductReleaseConfigMaps] = k
		prc := &rc_proto.ProductReleaseConfig{
			Products:                      v.Products,
			PsMapsValue:                   proto.String(v.ProductReleaseConfigMaps),
			ReleaseConfigsArtifactPathMap: make(map[string]string),
			InheritanceGraphPath:          proto.String(v.InheritanceGraphPath),
		}
		for variant, artifact_path := range v.AllReleaseConfigsMap {
			prc.ReleaseConfigsArtifactPathMap[variant] = artifact_path
		}
		prci.TagToArtifactMap[k] = prc
	}

	writePcri := func(ext string) {
		src := filepath.Join(artifactsDir, "release_config_info"+ext)
		if err := rc_lib.WriteMessage(src, prci); err != nil {
			errCh <- fmt.Errorf("Could not create %s: %v\n", src, err)
			return
		}
		if flags.Dist {
			dst := filepath.Join(flags.distDir, "release_configs", filepath.Base(src))
			if err := DistArtifact(src, dst); err != nil {
				errCh <- fmt.Errorf("Could not dist %s: %v\n", dst, err)
			}
		}
	}
	writePcri(".pb")
	if flags.Textproto {
		writePcri(".textproto")
	}
	if flags.Json {
		writePcri(".json")
	}
	return
}

// SortedKeys returns the keys of the given map in the ascending order.
func SortedKeys[T cmp.Ordered, V any](m map[T]V) []T {
	if len(m) == 0 {
		return nil
	}
	ret := make([]T, 0, len(m))
	for k := range m {
		ret = append(ret, k)
	}
	slices.Sort(ret)
	return ret
}

func DistArtifact(src, dst string) error {
	os.MkdirAll(filepath.Dir(dst), 0755)
	srcData, srcErr := os.ReadFile(src)
	if srcErr != nil {
		return fmt.Errorf("Unable to read %s: %v", src, srcErr)
	}
	return os.WriteFile(dst, srcData, 0644)
}
