package store

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/api/networking/v1beta1"
)

import (
	"github.com/baidu/ingress-bfe/internal/config"
)


type Store struct {
	Ingresses   []*v1beta1.Ingress
	Config *config.Config
}

func NewStore(kubeClient *kubernetes.Clientset, namespace string, cfgMapConfig *config.Config) (store *Store, err error) {
	ingresses, err := kubeClient.NetworkingV1beta1().Ingresses("").List(v1.ListOptions{})
	if err != nil {
		return
	}

	store = &Store{
		Ingresses: []*v1beta1.Ingress{},
	}

	// store all ingress obj
	for _, i := range ingresses.Items {
		store.Ingresses = append(store.Ingresses, &i)
	}

	// TODO Secret
	// TODO store anything

	store.Config = cfgMapConfig

	return
}

func (s *Store) Add(ing *v1beta1.Ingress) {
	isUniq := true

	for i := range s.Ingresses {
		in := s.Ingresses[i]
		if in.GetUID() == ing.GetUID() {
			isUniq = false
			s.Ingresses[i] = ing
		}
	}

	if isUniq {
		s.Ingresses = append(s.Ingresses, ing)
	}
}

func (s *Store) Pick(ing *v1beta1.Ingress) {
	id := ing.GetUID()

	var index int
	var hasMatch bool
	for i := range s.Ingresses {
		if s.Ingresses[i].GetUID() == id {
			index = i
			hasMatch = true
			break
		}
	}

	if hasMatch {
		s.Ingresses[len(s.Ingresses)-1], s.Ingresses[index] = s.Ingresses[index], s.Ingresses[len(s.Ingresses)-1]
		s.Ingresses = s.Ingresses[:len(s.Ingresses)-1]
	}
}
