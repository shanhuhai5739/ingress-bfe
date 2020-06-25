package bfe

import (
	"flag"
)

import (
	coreV1 "k8s.io/api/core/v1"
)

import (
	"github.com/baidu/ingress-bfe/internal/config"
)

func parseFlags() config.Configuration  {
	namespace := flag.String("namespace", coreV1.NamespaceAll, "Namespace the controller watches for updates to Kubernetes objects. This includes Ingresses, Services and all configuration resources. All namespaces are watched if this parameter is left empty.")

	flag.Parse()

	return config.Configuration{
		Namespace:*namespace,
	}
}