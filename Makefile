## Copyright 2019 Red Hat, Inc.
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

##@ Default target (all you need - just run "make")
default: build ## runs build

GOBINARY ?= go
EXE = ior
HUB ?= docker.io/maistra
TAG ?= latest
NAMESPACE ?= ior

LD_EXTRAFLAGS =

VERSION ?= development
LD_EXTRAFLAGS += -X github.com/maistra/ior/pkg/version.buildVersion=${VERSION}

GITREVISION ?= $(shell git rev-parse --verify HEAD 2> /dev/null)
ifeq ($(GITREVISION),)
  GITREVISION = unknown
endif
LD_EXTRAFLAGS += -X github.com/maistra/ior/pkg/version.buildGitRevision=${GITREVISION}

GITSTATUS ?= $(shell git diff-index --quiet HEAD --  2> /dev/null; echo $$?)
ifeq ($(GITSTATUS),0)
  GITSTATUS = Clean
else ifeq ($(GITSTATUS),1)
  GITSTATUS = Modified
else
  GITSTATUS = unknown
endif
LD_EXTRAFLAGS += -X github.com/maistra/ior/pkg/version.buildStatus=${GITSTATUS}

GITTAG ?= $(shell git describe 2> /dev/null)
ifeq ($(GITTAG),)
  GITTAG = unknown
endif
LD_EXTRAFLAGS += -X github.com/maistra/ior/pkg/version.buildTag=${GITTAG}

##@ Build targets

clean: ##
	rm -f ./cmd/${EXE} && rm -f container/${EXE}

LDFLAGS = '-extldflags -static ${LD_EXTRAFLAGS}'
build: ##
	CGO_ENABLED=0 ${GOBINARY} build -mod=vendor -o ./cmd/${EXE} -ldflags ${LDFLAGS} ./cmd/...

test: ##
	./tests/test.sh

##@ Image creation

image: build ##
	cp ./cmd/${EXE} container/ && \
	cd container && \
	docker build -t ${HUB}/ior:${TAG} .

push: image ##
	docker push ${HUB}/ior:${TAG}

##@ Helpers

deploy: ## Deploys operator into defined ${NAMESPACE}
	sed -e "s|\$${HUB}|${HUB}|g" -e "s|\$${TAG}|${TAG}|g" -e "s|\$${NAMESPACE}|${NAMESPACE}|g" container/pod.yaml | kubectl -n ${NAMESPACE} apply -f-

undeploy: ## Deletes operator from the ${NAMESPACE}
	sed -e "s|\$${HUB}|${HUB}|g" -e "s|\$${TAG}|${TAG}|g" -e "s|\$${NAMESPACE}|${NAMESPACE}|g" container/pod.yaml | kubectl -n ${NAMESPACE} delete -f-

.PHONY: help
help:  ## Displays this help \o/
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m\033[2m %s\033[0m\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@cat $(MAKEFILE_LIST) | grep "^[A-Za-z_]*.?=" | sort | awk 'BEGIN {FS="?="; printf "\n\n\033[1mEnvironment variables\033[0m\n"} {printf "  \033[36m%-25s\033[0m\033[2m %s\033[0m\n", $$1, $$2}'