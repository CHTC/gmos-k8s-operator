#!/bin/bash
kubectl -n dev delete -f kustomize/dev/pilot-set.yaml
kubectl delete -k kustomize/
