/*
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/apache/camel-k/pkg/apis/camel/v1"
	"github.com/apache/camel-k/pkg/platform"
	"github.com/apache/camel-k/pkg/trait"
	"github.com/apache/camel-k/pkg/util"
	"github.com/apache/camel-k/pkg/util/defaults"
	"github.com/apache/camel-k/pkg/util/log"
)

func lookupKitsForIntegration(ctx context.Context, c ctrl.Reader, integration *v1.Integration, options ...ctrl.ListOption) ([]v1.IntegrationKit, error) {
	pl, err := platform.GetForResource(ctx, c, integration)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	kitTypes, err := labels.NewRequirement(v1.IntegrationKitTypeLabel, selection.In, []string{
		v1.IntegrationKitTypePlatform,
		v1.IntegrationKitTypeExternal,
	})
	if err != nil {
		return nil, err
	}

	listOptions := []ctrl.ListOption{
		ctrl.InNamespace(integration.GetIntegrationKitNamespace(pl)),
		ctrl.MatchingLabels{
			"camel.apache.org/runtime.version":  integration.Status.RuntimeVersion,
			"camel.apache.org/runtime.provider": string(integration.Status.RuntimeProvider),
		},
		ctrl.MatchingLabelsSelector{
			Selector: labels.NewSelector().Add(*kitTypes),
		},
	}
	listOptions = append(listOptions, options...)

	list := v1.NewIntegrationKitList()
	if err := c.List(ctx, &list, listOptions...); err != nil {
		return nil, err
	}

	kits := make([]v1.IntegrationKit, 0)
	for i := range list.Items {
		kit := &list.Items[i]
		match, err := integrationMatches(integration, kit)
		if err != nil {
			return nil, err
		} else if !match {
			continue
		}
		kits = append(kits, *kit)
	}

	return kits, nil
}

// integrationMatches returns whether the v1.IntegrationKit meets the requirements of the v1.Integration.
func integrationMatches(integration *v1.Integration, kit *v1.IntegrationKit) (bool, error) {
	ilog := log.ForIntegration(integration)

	ilog.Debug("Matching integration", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
	if !statusMatches(integration, kit, &ilog) {
		return false, nil
	}

	// When a platform kit is created it inherits the traits from the integrations and as
	// some traits may influence the build thus the artifacts present on the container image,
	// we need to take traits into account when looking up for compatible kits.
	//
	// It could also happen that an integration is updated and a trait is modified, if we do
	// not include traits in the lookup, we may use a kit that does not have all the
	// characteristics required by the integration.
	//
	// A kit can be used only if it contains a subset of the traits and related configurations
	// declared on integration.
	if match, err := hasMatchingTraits(integration.Spec.Traits, kit.Spec.Traits); !match || err != nil {
		ilog.Debug("Integration and integration-kit traits do not match", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false, err
	}
	if !util.StringSliceContains(kit.Spec.Dependencies, integration.Status.Dependencies) {
		ilog.Debug("Integration and integration-kit dependencies do not match", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false, nil
	}

	ilog.Debug("Matched Integration and integration-kit", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
	return true, nil
}

func statusMatches(integration *v1.Integration, kit *v1.IntegrationKit, ilog *log.Logger) bool {
	if kit.Status.Phase == v1.IntegrationKitPhaseError {
		ilog.Debug("Integration kit has a phase of Error", "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false
	}
	if kit.Status.Version != integration.Status.Version {
		ilog.Debug("Integration and integration-kit versions do not match", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false
	}
	if kit.Status.RuntimeProvider != integration.Status.RuntimeProvider {
		ilog.Debug("Integration and integration-kit runtime providers do not match", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false
	}
	if kit.Status.RuntimeVersion != integration.Status.RuntimeVersion {
		ilog.Debug("Integration and integration-kit runtime versions do not match", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
		return false
	}
	if len(integration.Status.Dependencies) != len(kit.Spec.Dependencies) {
		ilog.Debug("Integration and integration-kit have different number of dependencies", "integration", integration.Name, "integration-kit", kit.Name, "namespace", integration.Namespace)
	}

	return true
}

// kitMatches returns whether the two v1.IntegrationKit match.
func kitMatches(kit1 *v1.IntegrationKit, kit2 *v1.IntegrationKit) (bool, error) {
	version := kit1.Status.Version
	if version == "" {
		// Defaults with the version that is going to be set during the kit initialization
		version = defaults.Version
	}
	if version != kit2.Status.Version {
		return false, nil
	}
	if len(kit1.Spec.Dependencies) != len(kit2.Spec.Dependencies) {
		return false, nil
	}
	if match, err := hasMatchingTraits(kit1.Spec.Traits, kit2.Spec.Traits); !match || err != nil {
		return false, err
	}
	if !util.StringSliceContains(kit1.Spec.Dependencies, kit2.Spec.Dependencies) {
		return false, nil
	}

	return true, nil
}

func hasMatchingTraits(traits interface{}, kitTraits interface{}) (bool, error) {
	traitMap, err := trait.ToTraitMap(traits)
	if err != nil {
		return false, err
	}
	kitTraitMap, err := trait.ToTraitMap(kitTraits)
	if err != nil {
		return false, err
	}
	catalog := trait.NewCatalog(nil)

	for _, t := range catalog.AllTraits() {
		if t == nil || !t.InfluencesKit() {
			// We don't store the trait configuration if the trait cannot influence the kit behavior
			continue
		}
		id := string(t.ID())
		it, ok1 := findTrait(traitMap, id)
		kt, ok2 := findTrait(kitTraitMap, id)

		if !ok1 && !ok2 {
			continue
		}
		if !ok1 || !ok2 {
			return false, nil
		}
		if ct, ok := t.(trait.ComparableTrait); ok {
			// if it's match trait use its matches method to determine the match
			if match, err := matchesComparableTrait(ct, it, kt); !match || err != nil {
				return false, err
			}
		} else {
			if !matchesTrait(it, kt) {
				return false, nil
			}
		}
	}

	return true, nil
}

func findTrait(traitsMap map[string]map[string]interface{}, id string) (map[string]interface{}, bool) {
	if trait, ok := traitsMap[id]; ok {
		return trait, true
	}

	if addons, ok := traitsMap["addons"]; ok {
		if addon, ok := addons[id]; ok {
			if trait, ok := addon.(map[string]interface{}); ok {
				return trait, true
			}
		}
	}

	return nil, false
}

func matchesComparableTrait(ct trait.ComparableTrait, it map[string]interface{}, kt map[string]interface{}) (bool, error) {
	t1 := reflect.New(reflect.TypeOf(ct).Elem()).Interface()
	if err := trait.ToTrait(it, &t1); err != nil {
		return false, err
	}

	t2 := reflect.New(reflect.TypeOf(ct).Elem()).Interface()
	if err := trait.ToTrait(kt, &t2); err != nil {
		return false, err
	}

	return t2.(trait.ComparableTrait).Matches(t1.(trait.Trait)), nil
}

func matchesTrait(it map[string]interface{}, kt map[string]interface{}) bool {
	// perform exact match on the two trait maps
	return reflect.DeepEqual(it, kt)
}
