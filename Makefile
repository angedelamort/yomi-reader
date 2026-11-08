APP      = angedelamort/yomi-reader
ARTIFACT = yomi-reader
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
IMAGE    = $(APP):$(VERSION)
LATEST   = $(APP):latest
OUT_DIR  = dist
PLATFORM ?= linux/amd64

.PHONY: build run release clean tidy vet test docker-build dev

## Compile the binary (CGO-free)
build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(ARTIFACT) .

## Run with the local provider (admin/admin on an empty DB)
run:
	go run .

## Run with the mock provider (auto-login, no password)
dev:
	AUTH_PROVIDER=mock MOCK_USER=dev MOCK_ADMIN=true DATA_PATH=./testdata/data PORT=8098 go run .

tidy:
	go mod tidy

vet:
	go vet ./...

test:
	go test ./...

## Build the Docker image (local tags)
docker-build:
	docker build \
		--provenance=false \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE) \
		-t $(LATEST) \
		.
	@echo "Image: $(IMAGE)"

## Build + export TWO tarballs (version pin + latest override for Synology).
## Each file must contain ONLY its own RepoTag — Synology replaces the running
## image by matching the :latest tag, so bundling both tags in one archive
## (or saving the version tag into the "latest" file) will not update the host.
release: docker-build
	mkdir -p $(OUT_DIR)
	docker save $(IMAGE) | gzip > $(OUT_DIR)/$(ARTIFACT)-$(VERSION).tar.gz
	docker save $(LATEST) | gzip > $(OUT_DIR)/$(ARTIFACT)-latest.tar.gz
	@echo ""
	@echo "Artefacts prêts :"
	@echo "  $(OUT_DIR)/$(ARTIFACT)-$(VERSION).tar.gz  (tag $(VERSION))"
	@echo "  $(OUT_DIR)/$(ARTIFACT)-latest.tar.gz      (tag latest)"
	@echo "Charger avec : docker load < fichier.tar.gz"
	@echo "Sur NAS (ex. Synology) : importer le *-latest.tar.gz pour remplacer l'image en cours"

## Nettoie les images et le dossier dist
clean:
	rm -rf $(OUT_DIR) $(ARTIFACT) $(ARTIFACT).exe testdata
	docker rmi $(IMAGE) $(LATEST) 2>/dev/null || true
