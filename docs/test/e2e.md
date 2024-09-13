# End-to-End testing

All e2e tests can be found in the test/e2e folder. The tests use the testing framework [godogs](https://github.com/cucumber/godog).

A quick-start for running the tests can be found in the [README.md](../../test/e2e/README.md) in that folder.

# Full flow

A typical e2e test for this project has the following basic flow:

1. Install Kind cluster with ingress enabled and ports opened (see the sample [kind-config](../../hack/artifacts/kind-config.yaml))
2. Install workloads:
    - assisted-service (https://github.com/openshift/assisted-service/blob/master/docs/dev/operator-on-kind.md)
      1. Deploy CRDs
      2. Deploy cert-manager
      3. Deploy nginx ingress
      4. Deploy infrastructure operator
      5. Create agentserviceconfig 
    - capi and capm3
    - baremetal operator
    - ironic

3. Install our operator + capm3 + capi
    ````sh
    mkdir -p $XDG_CONFIG_HOME/cluster-api/
    cat <<EOF>$XDG_CONFIG_HOME/cluster-api/clusterctl.yaml
    providers:
    - name: "openshift-agent"
        url: "https://github.com/openshift-assisted/cluster-api-agent/releases/latest/download/bootstrap-components.yaml"
        type: "BootstrapProvider"
    - name: "openshift-agent"
        url: "https://github.com/openshift-assisted/cluster-api-agent/releases/latest/download/controlplane-components.yaml"
        type: "ControlPlaneProvider"
    EOF
    ````
    ````sh
    clusterctl init --bootstrap openshift-agent --control-plane openshift-agent -i  metal3:v1.7.0 --config $XDG_CONFIG_HOME/cluster-api/clusterctl.yaml
    ````
    
4. Create libvirt VMs and BMHs for creating a spoke cluster
5. Apply our CRs for testing the operators and installing a spoke cluster
    - See sample [here](../../hack/deploy/artifacts/)

