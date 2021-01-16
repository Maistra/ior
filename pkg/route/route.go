// Copyright 2019 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package route

import (
	"fmt"
	"strings"

	"github.com/maistra/ior/pkg/bootstrap"
	"github.com/maistra/ior/pkg/util"
	v1 "github.com/openshift/api/route/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	maistraPrefix          = "maistra.io/"
	generatedByLabel       = maistraPrefix + "generated-by"
	generatedByValue       = "ior"
	originalHostAnnotation = maistraPrefix + "original-host"
	gatewayNameLabel       = maistraPrefix + "gateway-name"
	gatewayNamespaceLabel  = maistraPrefix + "gateway-namespace"
)

// GatewayInfo ...
type GatewayInfo struct {
	Metadata *mcp.Metadata
	Gateway  *networking.Gateway
}

type syncedRoute struct {
	route *v1.Route
	valid bool
}

// Route ...
type Route struct {
	args   *bootstrap.Args
	client *routev1.RouteV1Client
	ig     *IngressGateway
	routes map[string]*syncedRoute
}

// New ...
func New(args *bootstrap.Args) (*Route, error) {
	ig, err := NewIG()
	if err != nil {
		return nil, err
	}

	r := &Route{args: args, ig: ig}

	err = r.initClient()
	if err != nil {
		return nil, err
	}

	err = r.initRoutes()
	if err != nil {
		return nil, err
	}

	return r, nil
}

func getHost(route v1.Route) string {
	if host := route.ObjectMeta.Annotations[originalHostAnnotation]; host != "" {
		return host
	}
	return route.Spec.Host
}

func (r *Route) initRoutes() error {
	routes, err := r.client.Routes(r.args.Namespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", generatedByLabel, generatedByValue),
	})
	if err != nil {
		return fmt.Errorf("error getting routes: %v", err)
	}

	r.routes = make(map[string]*syncedRoute, len(routes.Items))
	for _, route := range routes.Items {
		localRoute := route
		r.routes[getHost(localRoute)] = &syncedRoute{
			route: &localRoute,
		}
	}
	return nil
}

// DumpRoutes ...
func (r *Route) DumpRoutes() {
	out := fmt.Sprintf("%d item(ns)\n", len(r.routes))
	for host, route := range r.routes {
		out += fmt.Sprintf("%s: %s/%s\n", host, route.route.ObjectMeta.Namespace, route.route.ObjectMeta.Name)
	}

	log.Debugf("Current state: %s\n", out)
}

// Sync ...
func (r *Route) Sync(gatewaysInfo []GatewayInfo) error {
	for _, sRoute := range r.routes {
		sRoute.valid = false
	}

	for _, gatewayInfo := range gatewaysInfo {
		for _, server := range gatewayInfo.Gateway.Servers {
			for _, host := range server.GetHosts() {
				_, ok := r.routes[host]
				if ok {
					r.editRoute(gatewayInfo.Metadata, host)
				} else {
					r.createRoute(gatewayInfo.Metadata, gatewayInfo.Gateway, host, server)
				}
			}
		}
	}

	for _, sRoute := range r.routes {
		if !sRoute.valid {
			r.deleteRoute(sRoute.route)
		}
	}

	return nil
}

func (r *Route) editRoute(metadata *mcp.Metadata, host string) {
	log.Debugf("Editing route for hostname %s", host)
	r.routes[host].valid = true
}

func (r *Route) deleteRoute(route *v1.Route) {
	var immediate int64
	host := getHost(*route)
	log.Debugf("Deleting route %s (hostname: %s)", route.ObjectMeta.Name, host)
	err := r.client.Routes(r.args.Namespace).Delete(route.ObjectMeta.Name, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	delete(r.routes, getHost(*route))
	if err == nil {
		log.Infof("Deleted route %s/%s (hostname: %s)", route.ObjectMeta.Namespace, route.ObjectMeta.Name, host)
	} else {
		log.Errorf("Error deleting route %s: %s", route.ObjectMeta.Name, err)
	}
}

func (r *Route) createRoute(metadata *mcp.Metadata, gateway *networking.Gateway, originalHost string, server *networking.Server) {
	var wildcard = v1.WildcardPolicyNone
	actualHost := originalHost

	log.Debugf("Creating route for hostname %s", originalHost)

	if originalHost == "*" {
		log.Infof("Gateway %s: Wildcard * is not supported at the moment. Letting OpenShift create the hostname instead.", metadata.Name)
		actualHost = ""
	} else if strings.HasPrefix(originalHost, "*.") {
		// Wildcards are not enabled by default in OCP 3.x.
		// See https://docs.openshift.com/container-platform/3.11/install_config/router/default_haproxy_router.html#using-wildcard-routes
		wildcard = v1.WildcardPolicySubdomain
		actualHost = "wildcard." + strings.TrimPrefix(originalHost, "*.")
	}

	var tlsConfig *v1.TLSConfig
	targetPort := "http2"
	if server.Tls != nil {
		tlsConfig = &v1.TLSConfig{Termination: v1.TLSTerminationPassthrough}
		if server.Tls.HttpsRedirect {
			tlsConfig.InsecureEdgeTerminationPolicy = v1.InsecureEdgeTerminationPolicyRedirect
		}
		targetPort = "https"
	}

	gatewayNamespace, gatewayName := util.ExtractNameNamespace(metadata.Name)

	// FIXME: Should we look for ingress gateway pod/service in all mesh members instead of just in the control plane namespace?
	serviceNamespace, serviceName, err := r.ig.FindService([]string{r.args.Namespace}, gateway)
	if err != nil {
		log.Errorf("Error creating a route for host %s (gateway %s): %s", originalHost, metadata.Name, err)
		return
	}

	annotations := map[string]string{
		originalHostAnnotation: originalHost,
	}
	for keyName, keyValue := range metadata.Annotations {
		if !strings.HasPrefix(keyName, "kubectl.kubernetes.io") {
			annotations[keyName] = keyValue
		}
	}

	nr, err := r.client.Routes(serviceNamespace).Create(&v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-", gatewayNamespace, gatewayName),
			Labels: map[string]string{
				generatedByLabel:      generatedByValue,
				gatewayNamespaceLabel: gatewayNamespace,
				gatewayNameLabel:      gatewayName,
			},
			Annotations: annotations,
		},
		Spec: v1.RouteSpec{
			Host: actualHost,
			Port: &v1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: targetPort,
				},
			},
			To: v1.RouteTargetReference{
				Name: serviceName,
			},
			TLS:            tlsConfig,
			WildcardPolicy: wildcard,
		},
	})

	if err != nil {
		log.Errorf("Error creating a route for host %s (gateway %s): %s", originalHost, metadata.Name, err)
		return
	}

	log.Infof("Created route %s/%s for hostname %s (gateway %s)", nr.ObjectMeta.Namespace, nr.ObjectMeta.Name, nr.Spec.Host, metadata.Name)

	r.routes[originalHost] = &syncedRoute{
		route: nr,
		valid: true,
	}
}

func (r *Route) initClient() error {
	config, err := kube.BuildClientConfig("", "")
	if err != nil {
		return fmt.Errorf("error creating a Kubernetes client: %v", err)
	}

	client, err := routev1.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error creating an OpenShift route client: %v", err)
	}

	r.client = client

	return nil
}
