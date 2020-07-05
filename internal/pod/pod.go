package pod

import (
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Info struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

func GetPodDetails(kubeClient *kubernetes.Clientset) (*Info, error) {
	podName := os.Getenv("POD_NAME")
	podNs := os.Getenv("POD_NAMESPACE")

	if podName == "" || podNs == "" {
		return nil, fmt.Errorf("unable to get POD information (missing POD_NAME or POD_NAMESPACE environment variable")
	}

	pod, _ := kubeClient.CoreV1().Pods(podNs).Get(context.Background(), podName, metav1.GetOptions{})
	if pod == nil {
		return nil, fmt.Errorf("unable to get POD information")
	}

	return &Info{
		Name:      podName,
		Namespace: podNs,
		Labels:    pod.GetLabels(),
	}, nil
}
