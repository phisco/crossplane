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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	apiextensionsv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/crossplane/crossplane/test/e2e/funcs"
)

// LabelAreaLifecycle is applied to all 'features' pertaining to managing
// Crossplane's lifecycle (installing, upgrading, etc).
const LabelAreaLifecycle = "lifecycle"

// TestCrossplaneLifecycle tests two features expecting them to be run in order:
//   - Uninstall: Test that it's possible to cleanly uninstall Crossplane, even
//     after having created and deleted a claim.
//   - Upgrade: Test that it's possible to upgrade Crossplane from the most recent
//     stable Helm chart to the one we're testing, even when a claim exists. This
//     expects the Uninstall feature to have been run first.
//
// Note: First time Installation is tested as part of the environment setup,
// if not disabled explicitly.
func TestCrossplaneLifecycle(t *testing.T) {

	manifests := "test/e2e/manifests/lifecycle/upgrade"
	environment.Test(t,
		// Test that it's possible to cleanly uninstall Crossplane, even after
		// having created and deleted a claim.
		features.New("Uninstall").
			WithLabel(LabelArea, LabelAreaLifecycle).
			WithLabel(LabelSize, LabelSizeSmall).
			WithSetup("CreatePrerequisites", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "prerequisites/*.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "prerequisites/*.yaml"),
			)).
			WithSetup("XRDBecomesEstablished", funcs.ResourcesHaveConditionWithin(1*time.Minute, manifests, "prerequisites/definition.yaml", apiextensionsv1.WatchingComposite())).
			WithSetup("CreateClaim", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "claim.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "claim.yaml"),
			)).
			WithSetup("ClaimBecomesAvailable", funcs.ResourcesHaveConditionWithin(2*time.Minute, manifests, "claim.yaml", xpv1.Available())).
			Assess("DeleteClaim", funcs.AllOf(
				funcs.DeleteResources(manifests, "claim.yaml"),
				funcs.ResourcesDeletedWithin(2*time.Minute, manifests, "claim.yaml"),
			)).
			Assess("DeletePrerequisites", funcs.AllOf(
				funcs.DeleteResources(manifests, "prerequisites/*.yaml"),
				funcs.ResourcesDeletedWithin(2*time.Minute, manifests, "prerequisites/*.yaml"),
			)).
			Assess("UninstallCrossplane", funcs.AllOf(
				funcs.AsFeaturesFunc(funcs.HelmUninstall(helm.WithName(helmReleaseName), helm.WithNamespace(namespace))),
			)).
			// Uninstalling the Crossplane Helm chart doesn't remove its CRDs. We
			// want to make sure they can be deleted cleanly. If they can't, it's a
			// sign something they define might have stuck around.
			WithTeardown("DeleteCrossplaneCRDs", funcs.AllOf(
				funcs.DeleteResources(crdsDir, "*.yaml"),
				funcs.ResourcesDeletedWithin(3*time.Minute, crdsDir, "*.yaml"),
			)).
			// Uninstalling the Crossplane Helm chart doesn't remove the namespace
			// it was installed to either. We want to make sure it can be deleted
			// cleanly.
			WithTeardown("DeleteCrossplaneNamespace", funcs.AllOf(
				funcs.AsFeaturesFunc(envfuncs.DeleteNamespace(namespace)),
				funcs.ResourceDeletedWithin(3*time.Minute, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}),
			)).
			Feature(),
		features.New("Upgrade").
			WithLabel(LabelArea, LabelAreaLifecycle).
			WithLabel(LabelSize, LabelSizeSmall).
			WithSetup("CrossplaneIsUninstalled", funcs.AllOf(
				// We expect Crossplane to have been uninstalled first
				funcs.ResourceDeletedWithin(1*time.Minute, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}),
			)).
			WithSetup("CreateCrossplane", funcs.AllOf(
				funcs.AsFeaturesFunc(envfuncs.CreateNamespace(namespace)),
				funcs.ResourceCreatedWithin(1*time.Minute, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}),
			)).
			WithSetup("InstallStableCrossplane", funcs.AllOf(
				funcs.AsFeaturesFunc(funcs.HelmRepo(
					helm.WithArgs("add"),
					helm.WithArgs("crossplane-stable"),
					helm.WithArgs("https://charts.crossplane.io/stable"),
				)),
				funcs.AsFeaturesFunc(funcs.HelmInstall(
					helm.WithNamespace(namespace),
					helm.WithName(helmReleaseName),
					helm.WithChart("crossplane-stable/crossplane"),
				)),
				funcs.ReadyToTestWithin(1*time.Minute, namespace))).
			WithSetup("CreateClaimPrerequisites", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "prerequisites/*.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "prerequisites/*.yaml"),
			)).
			WithSetup("XRDBecomesEstablished", funcs.ResourcesHaveConditionWithin(1*time.Minute, manifests, "prerequisites/definition.yaml", apiextensionsv1.WatchingComposite())).
			WithSetup("CreateClaim", funcs.AllOf(
				funcs.ApplyResources(FieldManager, manifests, "claim.yaml"),
				funcs.ResourcesCreatedWithin(30*time.Second, manifests, "claim.yaml"),
			)).
			WithSetup("ClaimBecomesAvailable", funcs.ResourcesHaveConditionWithin(2*time.Minute, manifests, "claim.yaml", xpv1.Available())).
			Assess("UpgradeCrossplane", funcs.AllOf(
				funcs.AsFeaturesFunc(funcs.HelmUpgrade(HelmOptions()...)),
				funcs.ReadyToTestWithin(1*time.Minute, namespace),
			)).
			Assess("CoreDeploymentBecomesAvailable", funcs.DeploymentBecomesAvailableWithin(1*time.Minute, namespace, "crossplane")).
			Assess("RBACManagerDeploymentBecomesAvailable", funcs.DeploymentBecomesAvailableWithin(1*time.Minute, namespace, "crossplane-rbac-manager")).
			Assess("CoreCRDsBecomeEstablished", funcs.ResourcesHaveConditionWithin(1*time.Minute, crdsDir, "*.yaml", funcs.CRDInitialNamesAccepted())).
			Assess("ClaimStillAvailable", funcs.ResourcesHaveConditionWithin(2*time.Minute, manifests, "claim.yaml", xpv1.Available())).
			Assess("ClaimIsStillAvailable", funcs.ResourcesHaveConditionWithin(2*time.Minute, manifests, "claim.yaml", xpv1.Available())).
			Assess("DeleteClaim", funcs.AllOf(
				funcs.DeleteResources(manifests, "claim.yaml"),
				funcs.ResourcesDeletedWithin(2*time.Minute, manifests, "claim.yaml"),
			)).
			WithTeardown("DeletePrerequisites", funcs.AllOf(
				funcs.DeleteResources(manifests, "prerequisites/*.yaml"),
				funcs.ResourcesDeletedWithin(2*time.Minute, manifests, "prerequisites/*.yaml"),
			)).
			Feature(),
	)
}