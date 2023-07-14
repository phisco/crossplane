/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	"github.com/crossplane/crossplane/test/e2e/funcs"
)

// LabelArea represents the 'area' of a feature. For example 'apiextensions',
// 'pkg', etc. Assessments roll up to features, which roll up to feature areas.
// Features within an area may be split across different test functions.
const LabelArea = "area"

// LabelModifyCrossplaneInstallation is used to mark tests that are going to
// modify Crossplane's installation, e.g. installing, uninstalling or upgrading
// it.
const LabelModifyCrossplaneInstallation = "modify-crossplane-installation"

// LabelModifyCrossplaneInstallationTrue is used to mark tests that are going to
// modify Crossplane's installation.
const LabelModifyCrossplaneInstallationTrue = "true"

// LabelStage represents the 'stage' of a feature - alpha, beta, etc. Generally
// available features have no stage label.
const LabelStage = "stage"

const (
	// LabelStageAlpha is used for tests of alpha features.
	LabelStageAlpha = "alpha"

	// LabelStageBeta is used for tests of beta features.
	LabelStageBeta = "beta"
)

// LabelSize represents the 'size' (i.e. duration) of a test.
const LabelSize = "size"

const (
	// LabelSizeSmall is used for tests that usually complete in a minute.
	LabelSizeSmall = "small"

	// LabelSizeLarge is used for test that usually complete in over a minute.
	LabelSizeLarge = "large"
)

const namespace = "crossplane-system"

const crdsDir = "cluster/crds"

// The caller (e.g. make e2e) must ensure these exists.
// Run `make build e2e-tag-images` to produce them
const (
	imgcore = "crossplane-e2e/crossplane:latest"
)

const (
	helmChartDir    = "cluster/charts/crossplane"
	helmReleaseName = "crossplane"
)

// FieldManager is the server-side apply field manager used when applying
// manifests.
const FieldManager = "crossplane-e2e-tests"

// The test environment, shared by all E2E test functions.
var environment env.Environment

var e2eConfig = NewE2EConfigFromFlags()

func NewCrossplaneInstallConfigPresets() CrossplaneInstallConfigPresets {
	return CrossplaneInstallConfigPresets{
		presets: make(map[string]crossplaneInstallConfigPreset),
	}
}

type CrossplaneInstallConfigPresets struct {
	presets map[string]crossplaneInstallConfigPreset
}

type crossplaneInstallConfigPreset struct {
	description     string
	installOpts     []helm.Option
	additionalSetup []env.Func
}

func (c *CrossplaneInstallConfigPresets) AddPreset(name, description string, opts ...CrossplaneInstallConfigOpt) {
	if _, exists := c.presets[name]; exists {
		panic(fmt.Sprintf("preset with name %s already exists", name))
	}

	p := crossplaneInstallConfigPreset{
		description: description,
	}
	for _, opt := range opts {
		opt(&p)
	}

	c.presets[name] = p
}

type CrossplaneInstallConfigOpt func(*crossplaneInstallConfigPreset)

func WithHelmOptions(condition func() bool, opts ...helm.Option) CrossplaneInstallConfigOpt {
	return func(p *crossplaneInstallConfigPreset) {
		if condition() {
			p.installOpts = append(p.installOpts, opts...)
		}
	}
}

func Always() bool {
	return true
}

func WithAdditionalSetup(condition func() bool, funcs ...env.Func) CrossplaneInstallConfigOpt {
	return func(p *crossplaneInstallConfigPreset) {
		if condition() {
			p.additionalSetup = append(p.additionalSetup, funcs...)
		}
	}
}

type E2EConfig struct {
	kindClusterName         *string
	createCluster           *bool
	destroyCluster          *bool
	installCrossplane       *bool
	installCrossplaneConfig *string
	loadImagesKindCluster   *bool

	presets CrossplaneInstallConfigPresets
	envConf *envconf.Config
}

func NewE2EConfigFromFlags() E2EConfig {
	e := E2EConfig{
		kindClusterName:         flag.String("kind-cluster-name", "", "name of the kind cluster to use"),
		createCluster:           flag.Bool("create-kind-cluster", true, "create a kind cluster (and deploy Crossplane) before running tests, if the cluster does not already exist with the same name"),
		destroyCluster:          flag.Bool("destroy-kind-cluster", true, "destroy the kind cluster when tests complete"),
		installCrossplane:       flag.Bool("install-crossplane", true, "install Crossplane before running tests"),
		installCrossplaneConfig: flag.String("install-crossplane-config", "", "the preset configuration to install Crossplane with if --install-crossplane is true"),
		loadImagesKindCluster:   flag.Bool("load-images-kind-cluster", true, "load Crossplane images into the kind cluster before running tests"),

		presets: NewCrossplaneInstallConfigPresets(),
	}

	return e
}

func (c *E2EConfig) AddPreset(name, description string, opts ...CrossplaneInstallConfigOpt) {
	c.presets.AddPreset(name, description, opts...)
}

func (c *E2EConfig) GetHelmInstallOpts(additionalOpts ...helm.Option) []helm.Option {
	opts := []helm.Option{
		helm.WithName(helmReleaseName),
		helm.WithNamespace(namespace),
		helm.WithChart(helmChartDir),
		// wait for the deployment to be ready for up to 5 minutes before returning
		helm.WithWait(),
		helm.WithTimeout("5m"),
		helm.WithArgs(
			// Run with debug logging to ensure all log statements are run.
			"--set args={--debug}",
			"--set image.repository="+strings.Split(imgcore, ":")[0],
			"--set image.tag="+strings.Split(imgcore, ":")[1],
		),
	}
	if c.installCrossplaneConfigDefined() {
		if p, ok := c.getPreset(*c.installCrossplaneConfig); ok {
			opts = append(opts, p.installOpts...)
		}
	}

	return append(opts, additionalOpts...)
}
func (c *E2EConfig) getPreset(name string) (crossplaneInstallConfigPreset, bool) {
	ps, ok := c.presets.presets[name]
	return ps, ok
}

func (c *E2EConfig) validate() error {
	// TODO(phisco)

	return nil
}

func (c *E2EConfig) installCrossplaneConfigDefined() bool {
	return c.installCrossplaneConfig != nil && *c.installCrossplaneConfig != ""
}

func (c *E2EConfig) GetInstallCrossplaneConfig() string {
	if c.installCrossplaneConfigDefined() {
		return *c.installCrossplaneConfig
	}
	return ""
}

func (c *E2EConfig) IsLabelExplicitlySelected(k, v string) bool {
	ls, _ := c.envConf.Labels()[k]
	for _, l := range ls {
		if l == v {
			return true
		}
	}
	return false
}

func (c *E2EConfig) shouldCreateCluster() bool {
	return *c.createCluster
}

func (c *E2EConfig) isKindCluster() bool {
	return *c.createCluster || *c.kindClusterName != ""
}

func (c *E2EConfig) shouldDestroyCluster() bool {
	return c.shouldCreateCluster() && *c.destroyCluster
}

func (c *E2EConfig) shouldInstallCrossplane() bool {
	return *c.installCrossplane
}

func (c *E2EConfig) shouldLoadImagesToKindCluster() bool {
	return c.isKindCluster() && *c.loadImagesKindCluster
}

func (c *E2EConfig) GetClusterName() string {
	if c.kindClusterName == nil || *c.kindClusterName == "" {
		n := envconf.RandomName("crossplane-e2e", 32)
		c.kindClusterName = &n
	}

	return *c.kindClusterName
}

func TestMain(m *testing.M) {
	// TODO(negz): Global loggers are dumb and klog is dumb. Remove this when
	// e2e-framework is running controller-runtime v0.15.x per
	// https://github.com/kubernetes-sigs/e2e-framework/issues/270
	log.SetLogger(klog.NewKlogr())

	if err := e2eConfig.validate(); err != nil {
		panic(err)
	}

	clusterName := e2eConfig.GetClusterName()

	var setup []env.Func
	var finish []env.Func

	cfg, err := envconf.NewFromFlags()
	if err != nil {
		panic(err)
	}
	e2eConfig.envConf = cfg

	// we want to create the cluster if it doesn't exist, but only if we're
	if e2eConfig.isKindCluster() {
		setup = []env.Func{
			envfuncs.CreateKindCluster(clusterName),
		}
	} else {
		e2eConfig.envConf.WithKubeconfigFile(conf.ResolveKubeConfigFile())
	}

	if e2eConfig.shouldLoadImagesToKindCluster() {
		setup = append(setup,
			envfuncs.LoadDockerImageToCluster(clusterName, imgcore),
		)
	}
	if e2eConfig.shouldInstallCrossplane() {
		setup = append(setup,
			envfuncs.CreateNamespace(namespace),
			funcs.HelmInstall(e2eConfig.GetHelmInstallOpts()...),
		)
	}

	// We always want to add our types to the scheme.
	setup = append(setup, funcs.AddCrossplaneTypesToScheme())

	// We want to destroy the cluster if we created it, but only if we created it,
	// otherwise the random name will be meaningless.
	if e2eConfig.shouldDestroyCluster() {
		finish = []env.Func{envfuncs.DestroyKindCluster(clusterName)}
	}

	if e2eConfig.installCrossplaneConfigDefined() {
		p, ok := e2eConfig.GetPreset(*e2eConfig.installCrossplaneConfig)
		if !ok {
			panic(fmt.Sprintf("preset with name %s does not exist", *e2eConfig.installCrossplaneConfig))
		}
		setup = append(setup, p.additionalSetup...)
		setup = append(setup)
	}

	environment = env.NewWithConfig(e2eConfig.envConf)
	environment.Setup(setup...)
	environment.Finish(finish...)
	os.Exit(environment.Run(m))
}
