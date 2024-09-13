Feature: Installing a SNO workload cluster

    Scenario: Kubernetes cluster exists and no workloads are installed
        Given a Kubernetes management cluster
        Then I want to install all services onto the cluster

    Scenario: Cleanup installed workloads on Kubernetes cluster
        Given a Kubernetes management cluster
        Then I want to uninstall all services onto the cluster
