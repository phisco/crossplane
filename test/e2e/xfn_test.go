/*
Copyright 2023 The Crossplane Authors.
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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/test/e2e/funcs"
	"github.com/crossplane/crossplane/test/e2e/utils"
)

// LabelAreaXFN is applied to all 'features' pertaining to managing Crossplane's
// Composition Functions (XFN).
const (
	LabelAreaXFN = "xfn"

	imgxfn                        = "crossplane-e2e/xfn:latest"
	CrossplaneConfigPresetWithXFN = "with-xfn-enabled"
)

func init() {
	e2eConfig.AddPreset(LabelAreaXFN, "",
		WithHelmOptions(
			e2eConfig.shouldInstallCrossplane, helm.WithArgs(
				"--set args={--debug,--enable-composition-functions}",
				"--set xfn.enabled=true",
				"--set xfn.args={--debug}",
				"--set xfn.image.repository="+strings.Split(imgxfn, ":")[0],
				"--set xfn.image.tag="+strings.Split(imgxfn, ":")[1],
			)),
		WithAdditionalSetup(e2eConfig.shouldLoadImagesToKindCluster, envfuncs.LoadDockerImageToCluster(e2eConfig.GetClusterName(), imgxfn)))
}

func TestXfnRunnerImagePull(t *testing.T) {
	if e2eConfig.GetInstallCrossplaneConfig() != CrossplaneConfigPresetWithCompositionSchemaValidation {
		t.Skip("Skipping test because composition schema validation is not enabled")
	}

	manifests := "test/e2e/manifests/xfnrunner/private-registry/pull"
	environment.Test(t,
		features.New("PullFnImageFromPrivateRegistryWithCustomCert").
			WithLabel(LabelArea, LabelAreaXFN).
			WithLabel(LabelSize, LabelSizeLarge).
			WithLabel(LabelModifyCrossplaneInstallation, LabelModifyCrossplaneInstallationTrue).
			WithSetup("InstallRegistryWithCustomTlsCertificate",
				funcs.AllOf(
					funcs.AsFeaturesFunc(envfuncs.CreateNamespace("reg")),
					func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
						dnsName := "private-docker-registry.reg.svc.cluster.local"
						ns := "reg"
						caPem, keyPem, err := utils.CreateCert(dnsName)
						if err != nil {
							t.Fatal(err)
						}

						secret := &corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "reg-cert",
								Namespace: ns,
							},
							Type: corev1.SecretTypeTLS,
							StringData: map[string]string{
								"tls.crt": caPem,
								"tls.key": keyPem,
							},
						}
						client := config.Client().Resources()
						if err := client.Create(ctx, secret); err != nil {
							t.Fatalf("Cannot create secret %s: %v", secret.Name, err)
						}
						configMap := &corev1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "reg-ca",
								Namespace: namespace,
							},
							Data: map[string]string{
								"domain.crt": caPem,
							},
						}
						if err := client.Create(ctx, configMap); err != nil {
							t.Fatalf("Cannot create config %s: %v", configMap.Name, err)
						}
						return ctx
					},

					funcs.AsFeaturesFunc(
						funcs.HelmRepo(
							helm.WithArgs("add"),
							helm.WithArgs("twuni"),
							helm.WithArgs("https://helm.twun.io"),
						)),
					funcs.AsFeaturesFunc(
						funcs.HelmInstall(
							helm.WithName("private"),
							helm.WithNamespace("reg"),
							helm.WithWait(),
							helm.WithChart("twuni/docker-registry"),
							helm.WithVersion("2.2.2"),
							helm.WithArgs(
								"--set service.type=NodePort",
								"--set service.nodePort=32000",
								"--set tlsSecretName=reg-cert",
							),
						))),
			).
			WithSetup("CopyFnImageToRegistry", func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
				nodes := &corev1.NodeList{}
				if err := config.Client().Resources().List(ctx, nodes); err != nil {
					t.Fatal("cannot list nodes", err)
				}
				if len(nodes.Items) == 0 {
					t.Fatalf("no nodes in the cluster")
				}
				var addr string
				for _, a := range nodes.Items[0].Status.Addresses {
					if a.Type == corev1.NodeInternalIP {
						addr = a.Address
						break
					}
				}
				if addr == "" {
					t.Fatalf("no nodes with private address")
				}

				srcRef, err := name.ParseReference("crossplane-e2e/fn-labelizer:latest")
				if err != nil {
					t.Fatal(err)
				}
				src, err := daemon.Image(srcRef)
				if err != nil {
					t.Fatal(err)
				}
				err = wait.For(func() (done bool, err error) {
					err = crane.Push(src, fmt.Sprintf("%s:32000/fn-labelizer:latest", addr), crane.Insecure)
					if err != nil {
						return false, nil //nolint:nilerr // we want to retry and to throw error
					}
					return true, nil
				}, wait.WithTimeout(1*time.Minute))
				if err != nil {
					t.Fatal("copying image to registry not successful", err)
				}
				return ctx
			}).
			WithSetup("CrossplaneDeployedWithRegistryEnabled", funcs.AllOf(
				funcs.AsFeaturesFunc(funcs.HelmUpgrade(
					e2eConfig.GetHelmInstallOpts(helm.WithArgs(
						"--set registryCaBundleConfig.name=reg-ca",
						"--set registryCaBundleConfig.key=domain.crt",
						"--set xfn.resources.requests.cpu=100m",
						"--set xfn.resources.limits.cpu=100m",
					))...,
				)),
				funcs.ReadyToTestWithin(1*time.Minute, namespace),
			)).
			WithSetup("ProviderNopDeployed", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "prerequisites/provider.yaml"),
				funcs.ApplyResources(FieldManager, manifests, "prerequisites/definition.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "prerequisites/provider.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "prerequisites/definition.yaml"),
				funcs.ResourcesHaveConditionWithin(1*time.Minute, manifests, "prerequisites/definition.yaml", v1.WatchingComposite()),
			)).
			Assess("CompositionWithFunctionIsCreated", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "composition.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "composition.yaml"),
			)).
			Assess("ClaimIsCreated", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "claim.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "claim.yaml"),
			)).
			Assess("ClaimBecomesAvailable", funcs.ResourcesHaveConditionWithin(5*time.Minute, manifests, "claim.yaml", xpv1.Available())).
			Assess("ManagedResourcesProcessedByFunction", func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
				labelName := "labelizer.xfn.crossplane.io/processed"
				rg := utils.NewResourceGetter(ctx, t, config)
				claim := rg.Get("fn-labelizer", "default", "nop.example.org/v1alpha1", "NopResource")
				r := utils.ResourceValue(t, claim, "spec", "resourceRef")

				xr := rg.Get(r["name"], "default", r["apiVersion"], r["kind"])
				mrefs := utils.ResourceSliceValue(t, xr, "spec", "resourceRefs")
				for _, mref := range mrefs {
					err := wait.For(func() (done bool, err error) {
						mr := rg.Get(mref["name"], "default", mref["apiVersion"], mref["kind"])
						l, found := mr.GetLabels()[labelName]
						if !found {
							return false, nil
						}
						if l != "true" {
							return false, nil
						}
						return true, nil
					}, wait.WithTimeout(5*time.Minute))
					if err != nil {
						t.Fatalf("Expected label %v value to be true", labelName)
					}

				}
				return ctx
			}).
			WithTeardown("DeleteClaim", funcs.AllOf(
				funcs.DeleteResources(manifests, "claim.yaml"),
				funcs.ResourcesDeletedWithin(30*time.Second, manifests, "claim.yaml"),
			)).
			WithTeardown("DeleteComposition", funcs.AllOf(
				funcs.DeleteResources(manifests, "composition.yaml"),
				funcs.ResourcesDeletedWithin(30*time.Second, manifests, "composition.yaml"),
			)).
			WithTeardown("ProviderNopRemoved", funcs.AllOf(
				funcs.DeleteResources(manifests, "prerequisites/provider.yaml"),
				funcs.DeleteResources(manifests, "prerequisites/definition.yaml"),
				funcs.ResourcesDeletedWithin(30*time.Second, manifests, "prerequisites/provider.yaml"),
				funcs.ResourcesDeletedWithin(30*time.Second, manifests, "prerequisites/definition.yaml"),
			)).
			WithTeardown("RemoveRegistry", funcs.AllOf(
				funcs.AsFeaturesFunc(envfuncs.DeleteNamespace("reg")),
				func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
					client := config.Client().Resources(namespace)
					configMap := &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "reg-ca",
							Namespace: namespace,
						},
					}
					err := client.Delete(ctx, configMap)
					if err != nil {
						t.Fatal(err)
					}
					return ctx
				},
			)).
			WithTeardown("CrossplaneDeployedWithoutFunctionsEnabled", funcs.AllOf(
				funcs.AsFeaturesFunc(funcs.HelmUpgrade(e2eConfig.GetHelmInstallOpts()...)),
				funcs.ReadyToTestWithin(1*time.Minute, namespace),
			)).
			Feature(),
	)
}
