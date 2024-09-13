# E2E tests

These e2e tests are built using [godogs](https://github.com/cucumber/godog) as the framework.

# How to run

Ensure you're either connected to a kubernetes cluster or your `KUBECONFIG` is pointing to the cluster you want to run tests on.

If using a `kind` cluster
```sh
kind export kubeconfig --kubeconfig $(pwd)/kind-kubeconfig
export KUBECONFIG=$(pwd)/kind-kubeconfig
```

```sh
cd test/e2e/godogs
go test
```

# Quick start Kind Kube cluster

```sh
kind create cluster --config hack/deploy/artifacts/kind-config.yaml
```
