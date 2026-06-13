# Image URL to use for building/pushing
IMG ?= gpu-isolation-operator:latest

# Tool versions
ENVTEST_K8S_VERSION ?= 1.31.0
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen$(shell go env GOEXE)

LOCALBIN ?= $(abspath bin)
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: all
all: build

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: generate
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: controller-gen
controller-gen: $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) rbac:roleName=gpu-isolation-manager-role paths="./..." output:rbac:artifacts:config=config/rbac
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) webhook paths="./..." output:webhook:artifacts:config=config/webhook

.PHONY: build
build: fmt vet
	go build -o bin/manager main.go

.PHONY: run
run: fmt vet
	go run main.go

.PHONY: test
test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: envtest
envtest: $(ENVTEST)
	$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || true

$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .

.PHONY: install
install:
	kubectl apply -f config/crd/bases/

.PHONY: deploy
deploy: install
	kubectl apply -f config/rbac/service_account.yaml
	kubectl apply -f config/rbac/role.yaml
	kubectl apply -f config/rbac/role_binding.yaml
	kubectl apply -f config/certmanager/
	kubectl apply -f config/webhook/service.yaml
	kubectl apply -f config/manager/manager.yaml
	kubectl apply -f config/webhook/manifests.yaml

.PHONY: undeploy
undeploy:
	kubectl delete -f config/webhook/manifests.yaml --ignore-not-found
	kubectl delete -f config/manager/manager.yaml --ignore-not-found
	kubectl delete -f config/webhook/service.yaml --ignore-not-found
	kubectl delete -f config/rbac/role_binding.yaml --ignore-not-found
	kubectl delete -f config/rbac/role.yaml --ignore-not-found
	kubectl delete -f config/rbac/service_account.yaml --ignore-not-found
	kubectl delete -f config/certmanager/ --ignore-not-found
	kubectl delete -f config/crd/bases/ --ignore-not-found

.PHONY: samples
samples:
	kubectl apply -f config/samples/

HELM_CHART ?= helm/gpu-isolation-operator
HELM_RELEASE ?= gpu-isolation
HELM_NAMESPACE ?= gpu-isolation-system

.PHONY: helm-lint
helm-lint:
	helm lint $(HELM_CHART)

.PHONY: helm-template
helm-template:
	helm template $(HELM_RELEASE) $(HELM_CHART) --namespace $(HELM_NAMESPACE)

.PHONY: helm-install
helm-install:
	helm upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--namespace $(HELM_NAMESPACE) --create-namespace

.PHONY: helm-install-dev
helm-install-dev:
	helm upgrade --install $(HELM_RELEASE) $(HELM_CHART) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		-f $(HELM_CHART)/values-dev.yaml

.PHONY: helm-uninstall
helm-uninstall:
	helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)
