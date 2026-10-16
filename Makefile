VERSION ?= $(shell git describe --tags --match 'v[0-9]*' 2>/dev/null | sed 's/^v//' || echo dev)
GO_IMAGE ?= golang:1.26.8
DOCKER_RUN = MSYS_NO_PATHCONV=1 docker run --rm -v "$(CURDIR):/src" -v rn-gomod:/go/pkg/mod -v rn-gocache:/root/.cache/go-build -w /src

.PHONY: test test-bpf test-api lint web bpf bpf-image dev dev-down images bundle iso minikube fmt

test:
	gofmt -s -l cmd internal tools
	go vet ./...
	go test ./...

fmt:
	gofmt -s -w cmd internal tools

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0 run --config build/golangci.yml

web:
	cd web && npm ci --no-audit --no-fund && npm run build

bpf-image:
	docker build -q --build-arg GO_IMAGE=$(GO_IMAGE) -f build/Dockerfile.bpf -t rendolnet-bpf build

# перегенерация обвязки после правки bpf/rendol.c
bpf: bpf-image
	$(DOCKER_RUN) rendolnet-bpf go generate ./internal/dataplane/ebpf/

# eBPF в настоящем ядре: нужен привилегированный контейнер
test-bpf: bpf-image
	$(DOCKER_RUN) --privileged -e RN_BPF_TEST=1 rendolnet-bpf go test -count=1 -v ./internal/dataplane/

test-api: bpf-image
	-docker network create rn-test >/dev/null 2>&1
	-docker rm -f rn-pg >/dev/null 2>&1
	docker run -d --name rn-pg --network rn-test -e POSTGRES_PASSWORD=pg -e POSTGRES_DB=rn postgres:17-alpine >/dev/null
	sleep 3
	$(DOCKER_RUN) --network rn-test -e RN_TEST_DATABASE_URL=postgres://postgres:pg@rn-pg:5432/rn rendolnet-bpf go test -count=1 ./internal/api/
	docker rm -f rn-pg >/dev/null

dev:
	docker compose -f deploy/dev/compose.yaml up --build -d
	@echo "https://localhost:8443  admin / admin"

dev-down:
	docker compose -f deploy/dev/compose.yaml down

images:
	for t in api agent zapret; do docker build -f deploy/Dockerfile --target $$t --build-arg VERSION=$(VERSION) -t rendolnet/$$t:$(VERSION) . || exit 1; done

bundle:
	bash scripts/build-bundle.sh $(VERSION)

iso:
	bash scripts/build-iso.sh $(VERSION)

# стенд в minikube: образы, dummy-интерфейсы на ноде, манифесты
minikube: images
	for t in api agent zapret; do minikube image load rendolnet/$$t:$(VERSION); done
	minikube ssh -- 'for s in "wan0 203.0.113.2/24" "lan0 192.168.1.1/24"; do set -- $$s; sudo ip link add $$1 type dummy 2>/dev/null; sudo ip addr replace $$2 dev $$1; sudo ip link set $$1 up; done'
	kubectl apply -f deploy/k8s/minikube.yaml
	bash scripts/render-manifests.sh $(VERSION) | kubectl apply -f -
