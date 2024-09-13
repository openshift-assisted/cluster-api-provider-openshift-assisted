/*
Copyright 2024.

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

package workloads

import (
	"github.com/openshift-assisted/cluster-api-agent/test/utils"
)

func Uninstall(kubeconfig string) error {
	verb := "delete"
	err := BootstrapProvider(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = ControlPlaneProvider(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = CAPM3(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = CAPI(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = UninstallBMOIronic(kubeconfig)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = UninstallAssistedService(kubeconfig)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = UninstallCertManager(kubeconfig)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = NginxIngress(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	err = AssistedServiceCRDs(kubeconfig, verb)
	if err != nil {
		utils.WarnError(err)
		return err
	}
	return nil
}
